// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sort"
	"time"

	"rescribe.xyz/bookpipeline"
	"rescribe.xyz/preproc"
	"rescribe.xyz/utils/pkg/hocr"
)

// TODO: stop using filepath.Join for keys; just use '/' delimeter

const usage = `Usage: bookpipeline [-v] [-np] [-nw] [-no] [-nop] [-na] [-t training]

Watches the preprocess, ocr and analyse queues for book names. When
one is found this general process is followed:

- The book name is hidden from the queue, and a 'heartbeat' is
  started which keeps it hidden (this will time out after 2 minutes
  if the program is terminated)
- The necessary files from bookname/ are downloaded
- The files are processed
- The resulting files are uploaded to bookname/
- The heartbeat is stopped
- The book name is removed from the queue it was taken from, and
  added to the next queue for future processing

`

const PauseBetweenChecks = 3 * time.Minute
const TimeBeforeShutdown = 5 * time.Minute
const HeartbeatTime = 60

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type Clouder interface {
	Init() error
	ListObjects(bucket string, prefix string) ([]string, error)
	Download(bucket string, key string, fn string) error
	Upload(bucket string, key string, path string) error
	CheckQueue(url string, timeout int64) (bookpipeline.Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	QueueHeartbeat(msg bookpipeline.Qmsg, qurl string, duration int64) (bookpipeline.Qmsg, error)
}

type Pipeliner interface {
	Clouder
	PreQueueId() string
	WipeQueueId() string
	OCRQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
	WIPStorageId() string
	GetLogger() *log.Logger
	Log(v ...interface{})
}

func download(dl chan string, process chan string, conn Pipeliner, dir string, errc chan error, logger *log.Logger) {
	for key := range dl {
		fn := filepath.Join(dir, filepath.Base(key))
		logger.Println("Downloading", key)
		err := conn.Download(conn.WIPStorageId(), key, fn)
		if err != nil {
			for range dl {
			} // consume the rest of the receiving channel so it isn't blocked
			close(process)
			errc <- err
			return
		}
		process <- fn
	}
	close(process)
}

func up(c chan string, done chan bool, conn Pipeliner, bookname string, errc chan error, logger *log.Logger) {
	for path := range c {
		name := filepath.Base(path)
		key := filepath.Join(bookname, name)
		logger.Println("Uploading", key)
		err := conn.Upload(conn.WIPStorageId(), key, path)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
	}

	done <- true
}

func upAndQueue(c chan string, done chan bool, toQueue string, conn Pipeliner, bookname string, training string, errc chan error, logger *log.Logger) {
	for path := range c {
		name := filepath.Base(path)
		key := filepath.Join(bookname, name)
		logger.Println("Uploading", key)
		err := conn.Upload(conn.WIPStorageId(), key, path)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
		logger.Println("Adding", key, training, "to queue", toQueue)
		err = conn.AddToQueue(toQueue, key + " " + training)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
	}

	done <- true
}

func preprocess(pre chan string, up chan string, errc chan error, logger *log.Logger) {
	for path := range pre {
		logger.Println("Preprocessing", path)
		done, err := preproc.PreProcMulti(path, []float64{0.1, 0.2, 0.4, 0.5}, "binary", 0, true, 5, 30)
		if err != nil {
			for range pre {
			} // consume the rest of the receiving channel so it isn't blocked
			close(up)
			errc <- err
			return
		}
		for _, p := range done {
			up <- p
		}
	}
	close(up)
}

func wipe(towipe chan string, up chan string, errc chan error, logger *log.Logger) {
	for path := range towipe {
		logger.Println("Wiping", path)
		s := strings.Split(path, ".")
		base := strings.Join(s[:len(s)-1], "")
		outpath := base + "_bin0.0.png"
		err := preproc.WipeFile(path, outpath, 5, 0.03, 30)
		if err != nil {
			for range towipe {
			} // consume the rest of the receiving channel so it isn't blocked
			close(up)
			errc <- err
			return
		}
		up <- outpath
	}
	close(up)
}

func ocr(training string) func(chan string, chan string, chan error, *log.Logger) {
	return func(toocr chan string, up chan string, errc chan error, logger *log.Logger) {
		for path := range toocr {
			logger.Println("OCRing", path)
			name := strings.Replace(path, ".png", "", 1)
			cmd := exec.Command("tesseract", "-l", training, path, name, "hocr")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err != nil {
				for range toocr {
				} // consume the rest of the receiving channel so it isn't blocked
				close(up)
				errc <- fmt.Errorf("Error ocring %s with training %s: %s\nStdout: %s\nStderr: %s\n", path, training, err, stdout.String(), stderr.String())
				return
			}
			up <- name + ".hocr"
		}
		close(up)
	}
}

func analyse(conn Pipeliner) func(chan string, chan string, chan error, *log.Logger) {
	return func(toanalyse chan string, up chan string, errc chan error, logger *log.Logger) {
	confs := make(map[string][]*bookpipeline.Conf)
	bestconfs := make(map[string]*bookpipeline.Conf)
	savedir := ""

	for path := range toanalyse {
		if savedir == "" {
			savedir = filepath.Dir(path)
		}
		logger.Println("Calculating confidence for", path)
		avg, err := hocr.GetAvgConf(path)
		if err != nil && err.Error() == "No words found" {
			continue
		}
		if err != nil {
			for range toanalyse {
			} // consume the rest of the receiving channel so it isn't blocked
			close(up)
			errc <- fmt.Errorf("Error retreiving confidence for %s: %s", path, err)
			return
		}
		base := filepath.Base(path)
		codestart := strings.Index(base, "_bin")
		name := base[0:codestart]
		var c bookpipeline.Conf
		c.Path = path
		c.Code = base[codestart:]
		c.Conf = avg
		confs[name] = append(confs[name], &c)
	}

	fn := filepath.Join(savedir, "conf")
	logger.Println("Saving confidences in file", fn)
	f, err := os.Create(fn)
	if err != nil {
		close(up)
		errc <- fmt.Errorf("Error creating file %s: %s", fn, err)
		return
	}
	defer f.Close()

	logger.Println("Finding best confidence for each page, and saving all confidences")
	for base, conf := range confs {
		var best float64
		for _, c := range conf {
			if c.Conf > best {
				best = c.Conf
				bestconfs[base] = c
			}
			_, err = fmt.Fprintf(f, "%s\t%02.f\n", c.Path, c.Conf)
			if err != nil {
				close(up)
				errc <- fmt.Errorf("Error writing confidences file: %s", err)
				return
			}
		}
	}
	up <- fn

	logger.Println("Creating best file listing the best file for each page")
	fn = filepath.Join(savedir, "best")
	f, err = os.Create(fn)
	if err != nil {
		close(up)
		errc <- fmt.Errorf("Error creating file %s: %s", fn, err)
		return
	}
	defer f.Close()
	for _, conf := range bestconfs {
		_, err = fmt.Fprintf(f, "%s\n", filepath.Base(conf.Path))
	}
	up <- fn

	var pgs []string
	for _, conf := range bestconfs {
		pgs = append(pgs, conf.Path)
	}
	sort.Strings(pgs)

	logger.Println("Downloading binarised and original images to create PDFs")
	bookname, err := filepath.Rel(os.TempDir(), savedir)
	if err != nil {
		close(up)
		errc <- fmt.Errorf("Failed to do filepath.Rel of %s to %s: %s", os.TempDir(), savedir, err)
		return
	}
	colourpdf := new(bookpipeline.Fpdf)
	err = colourpdf.Setup()
	if err != nil {
		close(up)
		errc <- fmt.Errorf("Failed to set up PDF: %s", err)
		return
	}
	binarisedpdf := new(bookpipeline.Fpdf)
	err = binarisedpdf.Setup()
	if err != nil {
		close(up)
		errc <- fmt.Errorf("Failed to set up PDF: %s", err)
		return
	}
	binhascontent, colourhascontent := false, false
	for _, pg := range pgs {
		var colourfn, binfn string
		base := filepath.Base(pg)
		nosuffix := strings.TrimSuffix(base, ".hocr")
		p := strings.SplitN(base, "_bin", 2)

		binfn = nosuffix + ".png"
		if len(p) > 1 {
			colourfn = p[0] + ".jpg"
		} else {
			colourfn = nosuffix + ".jpg"
		}

		logger.Println("Downloading binarised page to add to PDF", binfn)
		err := conn.Download(conn.WIPStorageId(), filepath.Join(bookname, binfn), filepath.Join(savedir, binfn))
		if err != nil {
			logger.Println("Download failed; skipping page", binfn)
		} else {
			err = binarisedpdf.AddPage(filepath.Join(savedir, binfn), pg, true)
			if err != nil {
				close(up)
				errc <- fmt.Errorf("Failed to add page %s to PDF: %s", binfn, err)
				return
			}
			binhascontent = true
		}

		logger.Println("Downloading colour page to add to PDF", colourfn)
		err = conn.Download(conn.WIPStorageId(), filepath.Join(bookname, colourfn), filepath.Join(savedir, colourfn))
		if err != nil {
			colourfn = strings.Replace(colourfn, ".jpg", ".png", 1)
			logger.Println("Download failed; trying", colourfn)
			err = conn.Download(conn.WIPStorageId(), filepath.Join(bookname, colourfn), filepath.Join(savedir, colourfn))
			if err != nil {
				logger.Println("Download failed; skipping page", colourfn)
			}
		}
		if err == nil {
			err = colourpdf.AddPage(filepath.Join(savedir, colourfn), pg, true)
			if err != nil {
				close(up)
				errc <- fmt.Errorf("Failed to add page %s to PDF: %s", colourfn, err)
				return
			}
			colourhascontent = true
		}
	}
	if colourhascontent {
		fn = filepath.Join(savedir, bookname + ".colour.pdf")
		err = colourpdf.Save(fn)
		if err != nil {
			close(up)
			errc <- fmt.Errorf("Failed to save colour pdf: %s", err)
			return
		}
		up <- fn
	}
	if binhascontent {
		fn = filepath.Join(savedir, bookname + ".binarised.pdf")
		err = binarisedpdf.Save(fn)
		if err != nil {
			close(up)
			errc <- fmt.Errorf("Failed to save binarised pdf: %s", err)
			return
		}
		up <- fn
	}

	logger.Println("Creating graph")
	fn = filepath.Join(savedir, "graph.png")
	f, err = os.Create(fn)
	if err != nil {
		close(up)
		errc <- fmt.Errorf("Error creating file %s: %s", fn, err)
		return
	}
	defer f.Close()
	err = bookpipeline.Graph(bestconfs, filepath.Base(savedir), f)
	if err != nil && err.Error() != "Not enough valid confidences" {
		close(up)
		errc <- fmt.Errorf("Error rendering graph: %s", err)
		return
	}
	up <- fn

	close(up)
	}
}

func heartbeat(conn Pipeliner, t *time.Ticker, msg bookpipeline.Qmsg, queue string, msgc chan bookpipeline.Qmsg, errc chan error) {
	currentmsg := msg
	for range t.C {
		m, err := conn.QueueHeartbeat(currentmsg, queue, HeartbeatTime*2)
		if err != nil {
			// This is for better debugging of the heartbeat issue
			conn.Log("Error with heartbeat", err)
			os.Exit(1)
			// TODO: would be better to ensure this error stops any running
			//       processes, as they will ultimately fail in the case of
			//       it. could do this by setting a global variable that
			//       processes check each time they loop.
			errc <- err
			t.Stop()
			return
		}
		if m.Id != "" {
			conn.Log("Replaced message handle as visibilitytimeout limit was reached")
			currentmsg = m
			// TODO: maybe handle communicating new msg more gracefully than this
			for range msgc {
			} // throw away any old msgc
			msgc <- m
		}
	}
}

// allOCRed checks whether all pages of a book have been OCRed.
// This is determined by whether every _bin0.?.png file has a
// corresponding .hocr file.
func allOCRed(bookname string, conn Pipeliner) bool {
	objs, err := conn.ListObjects(conn.WIPStorageId(), bookname)
	if err != nil {
		return false
	}

	preprocessedPattern := regexp.MustCompile(`_bin[0-9].[0-9].png$`)

	atleastone := false
	for _, png := range objs {
		if preprocessedPattern.MatchString(png) {
			atleastone = true
			found := false
			b := strings.TrimSuffix(filepath.Base(png), ".png")
			hocrname := bookname + "/" + b + ".hocr"
			for _, hocr := range objs {
				if hocr == hocrname {
					found = true
					break
				}
			}
			if found == false {
				return false
			}
		}
	}
	if atleastone == false {
		return false
	}
	return true
}

// ocrPage OCRs a page based on a message. It may make sense to
// roll this back into processBook (on which it is based) once
// working well.
func ocrPage(msg bookpipeline.Qmsg, conn Pipeliner, process func(chan string, chan string, chan error, *log.Logger), fromQueue string, toQueue string) error {
	dl := make(chan string)
	msgc := make(chan bookpipeline.Qmsg)
	processc := make(chan string)
	upc := make(chan string)
	done := make(chan bool)
	errc := make(chan error)

	msgparts := strings.Split(msg.Body, " ")
	bookname := filepath.Dir(msgparts[0])
	if len(msgparts) > 1 && msgparts[1] != "" {
		process = ocr(msgparts[1])
	}

	d := filepath.Join(os.TempDir(), bookname)
	err := os.MkdirAll(d, 0755)
	if err != nil {
		return fmt.Errorf("Failed to create directory %s: %s", d, err)
	}

	t := time.NewTicker(HeartbeatTime * time.Second)
	go heartbeat(conn, t, msg, fromQueue, msgc, errc)

	// these functions will do their jobs when their channels have data
	go download(dl, processc, conn, d, errc, conn.GetLogger())
	go process(processc, upc, errc, conn.GetLogger())
	go up(upc, done, conn, bookname, errc, conn.GetLogger())

	dl <- msgparts[0]
	close(dl)

	// wait for either the done or errc channel to be sent to
	select {
	case err = <-errc:
		t.Stop()
		_ = os.RemoveAll(d)
		return err
	case <-done:
	}

	if allOCRed(bookname, conn) && toQueue != "" {
		conn.Log("Sending", bookname, "to queue", toQueue)
		err = conn.AddToQueue(toQueue, bookname)
		if err != nil {
			t.Stop()
			_ = os.RemoveAll(d)
			return fmt.Errorf("Error adding to queue %s: %s", bookname, err)
		}
	}

	t.Stop()

	// check whether we're using a newer msg handle
	select {
	case m, ok := <-msgc:
		if ok {
			msg = m
			conn.Log("Using new message handle to delete message from queue")
		}
	default:
		conn.Log("Using original message handle to delete message from queue")
	}

	conn.Log("Deleting original message from queue", fromQueue)
	err = conn.DelFromQueue(fromQueue, msg.Handle)
	if err != nil {
		_ = os.RemoveAll(d)
		return fmt.Errorf("Error deleting message from queue: %s", err)
	}

	err = os.RemoveAll(d)
	if err != nil {
		return fmt.Errorf("Failed to remove directory %s: %s", d, err)
	}

	return nil
}

func processBook(msg bookpipeline.Qmsg, conn Pipeliner, process func(chan string, chan string, chan error, *log.Logger), match *regexp.Regexp, fromQueue string, toQueue string) error {
	dl := make(chan string)
	msgc := make(chan bookpipeline.Qmsg)
	processc := make(chan string)
	upc := make(chan string)
	done := make(chan bool)
	errc := make(chan error)

	msgparts := strings.Split(msg.Body, " ")
	bookname := msgparts[0]

	var training string
	if len(msgparts) > 1 {
		training = msgparts[1]
	}

	d := filepath.Join(os.TempDir(), bookname)
	err := os.MkdirAll(d, 0755)
	if err != nil {
		return fmt.Errorf("Failed to create directory %s: %s", d, err)
	}

	t := time.NewTicker(HeartbeatTime * time.Second)
	go heartbeat(conn, t, msg, fromQueue, msgc, errc)

	// these functions will do their jobs when their channels have data
	go download(dl, processc, conn, d, errc, conn.GetLogger())
	go process(processc, upc, errc, conn.GetLogger())
	if toQueue == conn.OCRPageQueueId() {
		go upAndQueue(upc, done, toQueue, conn, bookname, training, errc, conn.GetLogger())
	} else {
		go up(upc, done, conn, bookname, errc, conn.GetLogger())
	}

	conn.Log("Getting list of objects to download")
	objs, err := conn.ListObjects(conn.WIPStorageId(), bookname)
	if err != nil {
		t.Stop()
		_ = os.RemoveAll(d)
		return fmt.Errorf("Failed to get list of files for book %s: %s", bookname, err)
	}
	var todl []string
	for _, n := range objs {
		if !match.MatchString(n) {
			conn.Log("Skipping item that doesn't match target", n)
			continue
		}
		todl = append(todl, n)
	}
	for _, a := range todl {
		dl <- a
	}
	close(dl)

	// wait for either the done or errc channel to be sent to
	select {
	case err = <-errc:
		t.Stop()
		_ = os.RemoveAll(d)
		return err
	case <-done:
	}

	if toQueue != "" && toQueue != conn.OCRPageQueueId() {
		conn.Log("Sending", bookname, "to queue", toQueue)
		err = conn.AddToQueue(toQueue, bookname)
		if err != nil {
			t.Stop()
			_ = os.RemoveAll(d)
			return fmt.Errorf("Error adding to queue %s: %s", bookname, err)
		}
	}

	t.Stop()

	// check whether we're using a newer msg handle
	select {
	case m, ok := <-msgc:
		if ok {
			msg = m
			conn.Log("Using new message handle to delete message from queue")
		}
	default:
		conn.Log("Using original message handle to delete message from queue")
	}

	conn.Log("Deleting original message from queue", fromQueue)
	err = conn.DelFromQueue(fromQueue, msg.Handle)
	if err != nil {
		_ = os.RemoveAll(d)
		return fmt.Errorf("Error deleting message from queue: %s", err)
	}

	err = os.RemoveAll(d)
	if err != nil {
		return fmt.Errorf("Failed to remove directory %s: %s", d, err)
	}

	return nil
}

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		<-t.C
	}
}

func restartTimer(t *time.Timer) {
	//if !t.Stop() {
	//	<-t.C
	//}
	t.Reset(TimeBeforeShutdown)
}

func main() {
	verbose := flag.Bool("v", false, "verbose")
	training := flag.String("t", "rescribealphav5", "default tesseract training file to use (without the .traineddata part)")
	nopreproc := flag.Bool("np", false, "disable preprocessing")
	nowipe := flag.Bool("nw", false, "disable wipeonly")
	noocr := flag.Bool("no", false, "disable ocr")
	noocrpg := flag.Bool("nop", false, "disable ocr on individual pages")
	noanalyse := flag.Bool("na", false, "disable analysis")
	autoshutdown := flag.Bool("shutdown", true, "automatically shut down if no work has been available for 5 minutes")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	var verboselog *log.Logger
	if *verbose {
		verboselog = log.New(os.Stdout, "", 0)
	} else {
		var n NullWriter
		verboselog = log.New(n, "", 0)
	}

	origPattern := regexp.MustCompile(`[0-9]{4}.jpg$`)
	wipePattern := regexp.MustCompile(`[0-9]{4,6}(.bin)?.png$`)
	preprocessedPattern := regexp.MustCompile(`_bin[0-9].[0-9].png$`)
	ocredPattern := regexp.MustCompile(`.hocr$`)

	var conn Pipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}

	verboselog.Println("Setting up AWS session")
	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}
	verboselog.Println("Finished setting up AWS session")

	var checkPreQueue <-chan time.Time
	var checkWipeQueue <-chan time.Time
	var checkOCRQueue <-chan time.Time
	var checkOCRPageQueue <-chan time.Time
	var checkAnalyseQueue <-chan time.Time
	var shutdownIfQuiet *time.Timer
	if !*nopreproc {
		checkPreQueue = time.After(0)
	}
	if !*nowipe {
		checkWipeQueue = time.After(0)
	}
	if !*noocr {
		checkOCRQueue = time.After(0)
	}
	if !*noocrpg {
		checkOCRPageQueue = time.After(0)
	}
	if !*noanalyse {
		checkAnalyseQueue = time.After(0)
	}
	if *autoshutdown {
		shutdownIfQuiet = time.NewTimer(TimeBeforeShutdown)
	}

	for {
		select {
		case <-checkPreQueue:
			msg, err := conn.CheckQueue(conn.PreQueueId(), HeartbeatTime*2)
			checkPreQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking preprocess queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on preprocess queue, sleeping")
				continue
			}
			verboselog.Println("Message received on preprocess queue, processing", msg.Body)
			stopTimer(shutdownIfQuiet)
			err = processBook(msg, conn, preprocess, origPattern, conn.PreQueueId(), conn.OCRPageQueueId())
			restartTimer(shutdownIfQuiet)
			if err != nil {
				log.Println("Error during preprocess", err)
			}
		case <-checkWipeQueue:
			msg, err := conn.CheckQueue(conn.WipeQueueId(), HeartbeatTime*2)
			checkWipeQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking wipeonly queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on wipeonly queue, sleeping")
				continue
			}
			stopTimer(shutdownIfQuiet)
			verboselog.Println("Message received on wipeonly queue, processing", msg.Body)
			err = processBook(msg, conn, wipe, wipePattern, conn.WipeQueueId(), conn.OCRPageQueueId())
			restartTimer(shutdownIfQuiet)
			if err != nil {
				log.Println("Error during wipe", err)
			}
		case <-checkOCRPageQueue:
			msg, err := conn.CheckQueue(conn.OCRPageQueueId(), HeartbeatTime*2)
			checkOCRPageQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking OCR Page queue", err)
				continue
			}
			if msg.Handle == "" {
				continue
			}
			// Have OCRPageQueue checked immediately after completion, as chances are high that
			// there will be more pages that should be done without delay
			checkOCRPageQueue = time.After(0)
			stopTimer(shutdownIfQuiet)
			verboselog.Println("Message received on OCR Page queue, processing", msg.Body)
			err = ocrPage(msg, conn, ocr(*training), conn.OCRPageQueueId(), conn.AnalyseQueueId())
			restartTimer(shutdownIfQuiet)
			if err != nil {
				log.Println("Error during OCR Page process", err)
			}
		case <-checkOCRQueue:
			msg, err := conn.CheckQueue(conn.OCRQueueId(), HeartbeatTime*2)
			checkOCRQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking OCR queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on OCR queue, sleeping")
				continue
			}
			stopTimer(shutdownIfQuiet)
			verboselog.Println("Message received on OCR queue, processing", msg.Body)
			err = processBook(msg, conn, ocr(*training), preprocessedPattern, conn.OCRQueueId(), conn.AnalyseQueueId())
			restartTimer(shutdownIfQuiet)
			if err != nil {
				log.Println("Error during OCR process", err)
			}
		case <-checkAnalyseQueue:
			msg, err := conn.CheckQueue(conn.AnalyseQueueId(), HeartbeatTime*2)
			checkAnalyseQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking analyse queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on analyse queue, sleeping")
				continue
			}
			stopTimer(shutdownIfQuiet)
			verboselog.Println("Message received on analyse queue, processing", msg.Body)
			err = processBook(msg, conn, analyse(conn), ocredPattern, conn.AnalyseQueueId(), "")
			restartTimer(shutdownIfQuiet)
			if err != nil {
				log.Println("Error during analysis", err)
			}
		case <-shutdownIfQuiet.C:
			if *autoshutdown {
				log.Println("If I was sufficiently brave, now would be the time I would shut down")
			}
		}
	}
}
