package main

// TODO: check if images are prebinarised and if so skip multiple binarisation

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"rescribe.xyz/go.git/bookpipeline"
	"rescribe.xyz/go.git/lib/hocr"
	"rescribe.xyz/go.git/preproc"
)

const usage = `Usage: bookpipeline [-v] [-np] [-no] [-na] [-t training]

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
	OCRQueueId() string
	AnalyseQueueId() string
	WIPStorageId() string
	GetLogger() *log.Logger
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

func ocr(training string) func(chan string, chan string, chan error, *log.Logger) {
	return func(toocr chan string, up chan string, errc chan error, logger *log.Logger) {
		for path := range toocr {
			logger.Println("OCRing", path)
			name := strings.Replace(path, ".png", "", 1)
			cmd := exec.Command("tesseract", "-l", training, path, name, "hocr")
			err := cmd.Run()
			if err != nil {
				for range toocr {
				} // consume the rest of the receiving channel so it isn't blocked
				close(up)
				errc <- errors.New(fmt.Sprintf("Error ocring %s: %s", path, err))
				return
			}
			up <- name + ".hocr"
		}
		close(up)
	}
}

func analyse(toanalyse chan string, up chan string, errc chan error, logger *log.Logger) {
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
			errc <- errors.New(fmt.Sprintf("Error retreiving confidence for %s: %s", path, err))
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
		errc <- errors.New(fmt.Sprintf("Error creating file %s: %s", fn, err))
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
				errc <- errors.New(fmt.Sprintf("Error writing confidences file: %s", err))
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
		errc <- errors.New(fmt.Sprintf("Error creating file %s: %s", fn, err))
		return
	}
	defer f.Close()
	for _, conf := range bestconfs {
		_, err = fmt.Fprintf(f, "%s\n", filepath.Base(conf.Path))
	}
	up <- fn

	logger.Println("Creating graph")
	fn = filepath.Join(savedir, "graph.png")
	f, err = os.Create(fn)
	if err != nil {
		close(up)
		errc <- errors.New(fmt.Sprintf("Error creating file %s: %s", fn, err))
		return
	}
	defer f.Close()
	err = bookpipeline.Graph(bestconfs, filepath.Base(savedir), f)
	if err != nil {
		close(up)
		errc <- errors.New(fmt.Sprintf("Error rendering graph: %s", err))
		return
	}
	up <- fn

	close(up)
}

func heartbeat(conn Pipeliner, t *time.Ticker, msg bookpipeline.Qmsg, queue string, msgc chan bookpipeline.Qmsg, errc chan error) {
	currentmsg := msg
	for range t.C {
		m, err := conn.QueueHeartbeat(currentmsg, queue, HeartbeatTime*2)
		if err != nil {
			errc <- err
			t.Stop()
			return
		}
		if m.Id != "" {
			conn.GetLogger().Println("Replaced message handle as visibilitytimeout limit was reached")
			currentmsg = m
			// TODO: maybe handle communicating new msg more gracefully than this
			for range msgc {
			} // throw away any old msgc
			msgc <- m
		}
	}
}

func processBook(msg bookpipeline.Qmsg, conn Pipeliner, process func(chan string, chan string, chan error, *log.Logger), match *regexp.Regexp, fromQueue string, toQueue string) error {
	dl := make(chan string)
	msgc := make(chan bookpipeline.Qmsg)
	processc := make(chan string)
	upc := make(chan string)
	done := make(chan bool)
	errc := make(chan error)

	bookname := msg.Body

	d := filepath.Join(os.TempDir(), bookname)
	err := os.MkdirAll(d, 0755)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to create directory %s: %s", d, err))
	}

	t := time.NewTicker(HeartbeatTime * time.Second)
	go heartbeat(conn, t, msg, fromQueue, msgc, errc)

	// these functions will do their jobs when their channels have data
	go download(dl, processc, conn, d, errc, conn.GetLogger())
	go process(processc, upc, errc, conn.GetLogger())
	go up(upc, done, conn, bookname, errc, conn.GetLogger())

	conn.GetLogger().Println("Getting list of objects to download")
	objs, err := conn.ListObjects(conn.WIPStorageId(), bookname)
	if err != nil {
		t.Stop()
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Failed to get list of files for book %s: %s", bookname, err))
	}
	var todl []string
	for _, n := range objs {
		if !match.MatchString(n) {
			conn.GetLogger().Println("Skipping item that doesn't match target", n)
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

	if toQueue != "" {
		conn.GetLogger().Println("Sending", bookname, "to queue", toQueue)
		err = conn.AddToQueue(toQueue, bookname)
		if err != nil {
			t.Stop()
			_ = os.RemoveAll(d)
			return errors.New(fmt.Sprintf("Error adding to queue %s: %s", bookname, err))
		}
	}

	t.Stop()

	// check whether we're using a newer msg handle
	select {
	case m, ok := <-msgc:
		if ok {
			msg = m
			conn.GetLogger().Println("Using new message handle to delete message from old queue")
		}
	default:
		conn.GetLogger().Println("Using original message handle to delete message from old queue")
	}

	conn.GetLogger().Println("Deleting original message from queue", fromQueue)
	err = conn.DelFromQueue(fromQueue, msg.Handle)
	if err != nil {
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Error deleting message from queue: %s", err))
	}

	err = os.RemoveAll(d)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to remove directory %s: %s", d, err))
	}

	return nil
}

func main() {
	verbose := flag.Bool("v", false, "verbose")
	training := flag.String("t", "rescribealphav5", "tesseract training file to use")
	nopreproc := flag.Bool("np", false, "disable preprocessing")
	noocr := flag.Bool("no", false, "disable ocr")
	noanalyse := flag.Bool("na", false, "disable analysis")

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

	origPattern := regexp.MustCompile(`[0-9]{4}.jpg$`) // TODO: match alternative file naming
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
	var checkOCRQueue <-chan time.Time
	var checkAnalyseQueue <-chan time.Time
	if !*nopreproc {
		checkPreQueue = time.After(0)
	}
	if !*noocr {
		checkOCRQueue = time.After(0)
	}
	if !*noanalyse {
		checkAnalyseQueue = time.After(0)
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
			err = processBook(msg, conn, preprocess, origPattern, conn.PreQueueId(), conn.OCRQueueId())
			if err != nil {
				log.Println("Error during preprocess", err)
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
			verboselog.Println("Message received on OCR queue, processing", msg.Body)
			err = processBook(msg, conn, ocr(*training), preprocessedPattern, conn.OCRQueueId(), conn.AnalyseQueueId())
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
			verboselog.Println("Message received on analyse queue, processing", msg.Body)
			err = processBook(msg, conn, analyse, ocredPattern, conn.AnalyseQueueId(), "")
			if err != nil {
				log.Println("Error during analysis", err)
			}
		}
	}
}
