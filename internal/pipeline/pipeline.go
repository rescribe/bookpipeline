// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// pipeline is a package used by the bookpipeline command, which
// handles the core functionality, using channels heavily to
// coordinate jobs. Note that it is considered an "internal" package,
// not intended for external use, and no guarantee is made of the
// stability of any interfaces provided.
package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"rescribe.xyz/bookpipeline"
	"rescribe.xyz/preproc"
	"rescribe.xyz/utils/pkg/hocr"
)

const HeartbeatSeconds = 60

type Lister interface {
	ListObjects(bucket string, prefix string) ([]string, error)
	Log(v ...interface{})
	WIPStorageId() string
}

type Downloader interface {
	Download(bucket string, key string, fn string) error
	Log(v ...interface{})
	WIPStorageId() string
}

type DownloadLister interface {
	Download(bucket string, key string, fn string) error
	ListObjects(bucket string, prefix string) ([]string, error)
	Log(v ...interface{})
	WIPStorageId() string
}

type Uploader interface {
	Log(v ...interface{})
	Upload(bucket string, key string, path string) error
	WIPStorageId() string
}

type Queuer interface {
	AddToQueue(url string, msg string) error
	AnalyseQueueId() string
	CheckQueue(url string, timeout int64) (bookpipeline.Qmsg, error)
	DelFromQueue(url string, handle string) error
	Log(v ...interface{})
	OCRPageQueueId() string
	PreNoWipeQueueId() string
	PreQueueId() string
	QueueHeartbeat(msg bookpipeline.Qmsg, qurl string, duration int64) (bookpipeline.Qmsg, error)
	WipeQueueId() string
}

type UploadQueuer interface {
	Log(v ...interface{})
	Upload(bucket string, key string, path string) error
	WIPStorageId() string
	PreNoWipeQueueId() string
	PreQueueId() string
	WipeQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
	CheckQueue(url string, timeout int64) (bookpipeline.Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	QueueHeartbeat(msg bookpipeline.Qmsg, qurl string, duration int64) (bookpipeline.Qmsg, error)
}

type Pipeliner interface {
	AddToQueue(url string, msg string) error
	AnalyseQueueId() string
	CheckQueue(url string, timeout int64) (bookpipeline.Qmsg, error)
	DelFromQueue(url string, handle string) error
	Download(bucket string, key string, fn string) error
	GetLogger() *log.Logger
	Init() error
	ListObjects(bucket string, prefix string) ([]string, error)
	Log(v ...interface{})
	OCRPageQueueId() string
	PreNoWipeQueueId() string
	PreQueueId() string
	QueueHeartbeat(msg bookpipeline.Qmsg, qurl string, duration int64) (bookpipeline.Qmsg, error)
	Upload(bucket string, key string, path string) error
	WipeQueueId() string
	WIPStorageId() string
}

type MinPipeliner interface {
	Pipeliner
	MinimalInit() error
}

type pageimg struct {
	hocr, img string
}

type mailSettings struct {
	server, port, user, pass, from, to string
}

func GetMailSettings() (mailSettings, error) {
	p := filepath.Join(os.Getenv("HOME"), ".config", "bookpipeline", "mailsettings")
	b, err := ioutil.ReadFile(p)
	if err != nil {
		return mailSettings{}, fmt.Errorf("Error reading mailsettings from %s: %v", p, err)
	}
	f := strings.Fields(string(b))
	if len(f) != 6 {
		return mailSettings{}, fmt.Errorf("Error parsing mailsettings, need %d fields, got %d", 6, len(f))
	}
	return mailSettings{f[0], f[1], f[2], f[3], f[4], f[5]}, nil
}

// download reads file names from a channel and downloads them into
// dir, putting each successfully downloaded file name into the
// process channel. If an error occurs it is sent to the errc channel
// and the function returns early.
func download(ctx context.Context, dl chan string, process chan string, conn Downloader, dir string, errc chan error, logger *log.Logger) {
	for key := range dl {
		select {
		case <-ctx.Done():
			for range dl {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- ctx.Err()
			close(process)
			return
		default:
		}
		fn := filepath.Join(dir, filepath.Base(key))
		logger.Println("Downloading", key)
		err := conn.Download(conn.WIPStorageId(), key, fn)
		if err != nil {
			for range dl {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			close(process)
			return
		}
		process <- fn
	}
	close(process)
}

// up reads file names from a channel and uploads them with
// the bookname/ prefix, removing the local copy of each file
// once it has been successfully uploaded. The done channel is
// then written to to signal completion. If an error occurs it
// is sent to the errc channel and the function returns early.
func up(ctx context.Context, c chan string, done chan bool, conn Uploader, bookname string, errc chan error, logger *log.Logger) {
	for path := range c {
		select {
		case <-ctx.Done():
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- ctx.Err()
			return
		default:
		}
		name := filepath.Base(path)
		key := bookname + "/" + name
		logger.Println("Uploading", key)
		err := conn.Upload(conn.WIPStorageId(), key, path)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
		err = os.Remove(path)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
	}

	done <- true
}

// upAndQueue reads file names from a channel and uploads them with
// the bookname/ prefix, removing the local copy of each file
// once it has been successfully uploaded. Each done file name is
// added to the toQueue once it has been uploaded. The done channel
// is then written to to signal completion. If an error occurs it
// is sent to the errc channel and the function returns early.
func upAndQueue(ctx context.Context, c chan string, done chan bool, toQueue string, conn UploadQueuer, bookname string, training string, errc chan error, logger *log.Logger) {
	for path := range c {
		select {
		case <-ctx.Done():
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- ctx.Err()
			return
		default:
		}
		name := filepath.Base(path)
		key := bookname + "/" + name
		logger.Println("Uploading", key)
		err := conn.Upload(conn.WIPStorageId(), key, path)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
		err = os.Remove(path)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
		logger.Println("Adding", key, training, "to queue", toQueue)
		err = conn.AddToQueue(toQueue, key+" "+training)
		if err != nil {
			for range c {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
	}

	done <- true
}

func Preprocess(thresholds []float64, nowipe bool) func(context.Context, chan string, chan string, chan error, *log.Logger) {
	return func(ctx context.Context, pre chan string, up chan string, errc chan error, logger *log.Logger) {
		for path := range pre {
			select {
			case <-ctx.Done():
				for range pre {
				} // consume the rest of the receiving channel so it isn't blocked
				errc <- ctx.Err()
				return
			default:
			}
			logger.Println("Preprocessing", path)
			done, err := preproc.PreProcMulti(path, thresholds, "binary", 0, !nowipe, 5, 30, 120, 30)
			if err != nil {
				for range pre {
				} // consume the rest of the receiving channel so it isn't blocked
				errc <- err
				return
			}
			_ = os.Remove(path)
			for _, p := range done {
				up <- p
			}
		}
		close(up)
	}
}

func Wipe(ctx context.Context, towipe chan string, up chan string, errc chan error, logger *log.Logger) {
	for path := range towipe {
		select {
		case <-ctx.Done():
			for range towipe {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- ctx.Err()
			return
		default:
		}
		logger.Println("Wiping", path)
		s := strings.Split(path, ".")
		base := strings.Join(s[:len(s)-1], "")
		outpath := base + "_bin0.0.png"
		err := preproc.WipeFile(path, outpath, 5, 0.03, 30, 120, 0.005, 30)
		if err != nil {
			for range towipe {
			} // consume the rest of the receiving channel so it isn't blocked
			errc <- err
			return
		}
		up <- outpath
	}
	close(up)
}

func Ocr(training string, tesscmd string) func(context.Context, chan string, chan string, chan error, *log.Logger) {
	return func(ctx context.Context, toocr chan string, up chan string, errc chan error, logger *log.Logger) {
		if tesscmd == "" {
			tesscmd = "tesseract"
		}
		for path := range toocr {
			select {
			case <-ctx.Done():
				for range toocr {
				} // consume the rest of the receiving channel so it isn't blocked
				errc <- ctx.Err()
				return
			default:
			}
			logger.Println("OCRing", path)
			name := strings.Replace(path, ".png", "", 1)
			cmd := exec.Command(tesscmd, "-l", training, path, name, "-c", "tessedit_create_hocr=1", "-c", "hocr_font_info=0")
			HideCmd(cmd)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err != nil {
				for range toocr {
				} // consume the rest of the receiving channel so it isn't blocked
				errc <- fmt.Errorf("Error ocring %s with training %s: %s\nStdout: %s\nStderr: %s\n", path, training, err, stdout.String(), stderr.String())
				return
			}
			up <- name + ".hocr"
		}
		close(up)
	}
}

func Analyse(conn Downloader) func(context.Context, chan string, chan string, chan error, *log.Logger) {
	return func(ctx context.Context, toanalyse chan string, up chan string, errc chan error, logger *log.Logger) {
		confs := make(map[string][]*bookpipeline.Conf)
		bestconfs := make(map[string]*bookpipeline.Conf)
		savedir := ""

		for path := range toanalyse {
			select {
			case <-ctx.Done():
				for range toanalyse {
				} // consume the rest of the receiving channel so it isn't blocked
				errc <- ctx.Err()
				return
			default:
			}
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
			errc <- fmt.Errorf("Error creating file %s: %s", fn, err)
			return
		}
		defer f.Close()

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

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
					errc <- fmt.Errorf("Error writing confidences file: %s", err)
					return
				}
			}
		}
		f.Close()
		up <- fn

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		logger.Println("Creating best file listing the best file for each page")
		fn = filepath.Join(savedir, "best")
		f, err = os.Create(fn)
		if err != nil {
			errc <- fmt.Errorf("Error creating file %s: %s", fn, err)
			return
		}
		defer f.Close()
		for _, conf := range bestconfs {
			_, err = fmt.Fprintf(f, "%s\n", filepath.Base(conf.Path))
		}
		f.Close()
		up <- fn

		var pgs []string
		for _, conf := range bestconfs {
			pgs = append(pgs, conf.Path)
		}
		sort.Strings(pgs)

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		logger.Println("Downloading binarised and original images to create PDFs")
		bookname, err := filepath.Rel(os.TempDir(), savedir)
		if err != nil {
			errc <- fmt.Errorf("Failed to do filepath.Rel of %s to %s: %s", os.TempDir(), savedir, err)
			return
		}
		colourpdf := new(bookpipeline.Fpdf)
		err = colourpdf.Setup()
		if err != nil {
			errc <- fmt.Errorf("Failed to set up PDF: %s", err)
			return
		}
		binarisedpdf := new(bookpipeline.Fpdf)
		err = binarisedpdf.Setup()
		if err != nil {
			errc <- fmt.Errorf("Failed to set up PDF: %s", err)
			return
		}
		fullsizepdf := new(bookpipeline.Fpdf)
		err = fullsizepdf.Setup()
		if err != nil {
			errc <- fmt.Errorf("Failed to set up PDF: %s", err)
			return
		}
		binhascontent, colourhascontent := false, false

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		var colourimgs, binimgs []pageimg

		for _, pg := range pgs {
			base := filepath.Base(pg)
			nosuffix := strings.TrimSuffix(base, ".hocr")
			p := strings.SplitN(base, "_bin", 2)

			var fn string
			if len(p) > 1 {
				fn = p[0] + ".jpg"
			} else {
				fn = nosuffix + ".jpg"
			}

			binimgs = append(binimgs, pageimg{hocr: base, img: nosuffix + ".png"})
			colourimgs = append(colourimgs, pageimg{hocr: base, img: fn})
		}

		for _, pg := range binimgs {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			default:
			}

			logger.Println("Downloading binarised page to add to PDF", pg.img)
			err := conn.Download(conn.WIPStorageId(), bookname+"/"+pg.img, filepath.Join(savedir, pg.img))
			if err != nil {
				logger.Println("Download failed; skipping page", pg.img)
			} else {
				err = binarisedpdf.AddPage(filepath.Join(savedir, pg.img), filepath.Join(savedir, pg.hocr), true)
				if err != nil {
					errc <- fmt.Errorf("Failed to add page %s to PDF: %s", pg.img, err)
					return
				}
				binhascontent = true
				err = os.Remove(filepath.Join(savedir, pg.img))
				if err != nil {
					errc <- err
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		if binhascontent {
			fn = filepath.Join(savedir, bookname+".binarised.pdf")
			err = binarisedpdf.Save(fn)
			if err != nil {
				errc <- fmt.Errorf("Failed to save binarised pdf: %s", err)
				return
			}
			up <- fn
		}

		for _, pg := range colourimgs {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			default:
			}

			logger.Println("Downloading colour page to add to PDF", pg.img)
			colourfn := pg.img
			err = conn.Download(conn.WIPStorageId(), bookname+"/"+colourfn, filepath.Join(savedir, colourfn))
			if err != nil {
				colourfn = strings.Replace(pg.img, ".jpg", ".png", 1)
				logger.Println("Download failed; trying", colourfn)
				err = conn.Download(conn.WIPStorageId(), bookname+"/"+colourfn, filepath.Join(savedir, colourfn))
				if err != nil {
					logger.Println("Download failed; skipping page", pg.img)
				}
			}
			if err == nil {
				err = colourpdf.AddPage(filepath.Join(savedir, colourfn), filepath.Join(savedir, pg.hocr), true)
				if err != nil {
					errc <- fmt.Errorf("Failed to add page %s to PDF: %s", pg.img, err)
					return
				}
				colourhascontent = true
				err = os.Remove(filepath.Join(savedir, colourfn))
				if err != nil {
					errc <- err
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		if colourhascontent {
			fn = filepath.Join(savedir, bookname+".colour.pdf")
			err = colourpdf.Save(fn)
			if err != nil {
				errc <- fmt.Errorf("Failed to save colour pdf: %s", err)
				return
			}
			up <- fn
		}

		for _, pg := range colourimgs {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			default:
			}

			logger.Println("Downloading colour page to add to PDF", pg.img)
			colourfn := pg.img
			err = conn.Download(conn.WIPStorageId(), bookname+"/"+colourfn, filepath.Join(savedir, colourfn))
			if err != nil {
				colourfn = strings.Replace(pg.img, ".jpg", ".png", 1)
				logger.Println("Download failed; trying", colourfn)
				err = conn.Download(conn.WIPStorageId(), bookname+"/"+colourfn, filepath.Join(savedir, colourfn))
				if err != nil {
					logger.Println("Download failed; skipping page", pg.img)
				}
			}
			if err == nil {
				err = fullsizepdf.AddPage(filepath.Join(savedir, colourfn), filepath.Join(savedir, pg.hocr), false)
				if err != nil {
					errc <- fmt.Errorf("Failed to add page %s to PDF: %s", pg.img, err)
					return
				}
				err = os.Remove(filepath.Join(savedir, colourfn))
				if err != nil {
					errc <- err
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		if colourhascontent {
			fn = filepath.Join(savedir, bookname+".original.pdf")
			err = fullsizepdf.Save(fn)
			if err != nil {
				errc <- fmt.Errorf("Failed to save full size pdf: %s", err)
				return
			}
			up <- fn
		}

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		logger.Println("Creating graph")
		fn = filepath.Join(savedir, "graph.png")
		f, err = os.Create(fn)
		if err != nil {
			errc <- fmt.Errorf("Error creating file %s: %s", fn, err)
			return
		}
		defer f.Close()
		err = bookpipeline.Graph(bestconfs, filepath.Base(savedir), f)
		if err != nil {
			_ = os.Remove(fn)
		}
		if err != nil && err.Error() != "Not enough valid confidences" {
			errc <- fmt.Errorf("Error rendering graph: %s", err)
			return
		}

		select {
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		default:
		}

		if err == nil {
			up <- fn
		}

		close(up)
	}
}

func heartbeat(conn Queuer, t *time.Ticker, msg bookpipeline.Qmsg, queue string, msgc chan bookpipeline.Qmsg, errc chan error) {
	currentmsg := msg
	for range t.C {
		m, err := conn.QueueHeartbeat(currentmsg, queue, HeartbeatSeconds*2)
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
func allOCRed(bookname string, conn Lister) bool {
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
			hocrname := strings.TrimSuffix(png, ".png") + ".hocr"
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

// OcrPage OCRs a page based on a message. It may make sense to
// roll this back into processBook (on which it is based) once
// working well.
func OcrPage(ctx context.Context, msg bookpipeline.Qmsg, conn Pipeliner, process func(context.Context, chan string, chan string, chan error, *log.Logger), fromQueue string, toQueue string) error {
	dl := make(chan string)
	msgc := make(chan bookpipeline.Qmsg)
	processc := make(chan string)
	upc := make(chan string)
	done := make(chan bool)
	errc := make(chan error)

	msgparts := strings.Split(msg.Body, " ")
	bookname := filepath.Dir(msgparts[0])
	if len(msgparts) > 1 && msgparts[1] != "" {
		process = Ocr(msgparts[1], "")
	}

	d := filepath.Join(os.TempDir(), bookname)
	err := os.MkdirAll(d, 0755)
	if err != nil {
		return fmt.Errorf("Failed to create directory %s: %s", d, err)
	}

	t := time.NewTicker(HeartbeatSeconds * time.Second)
	go heartbeat(conn, t, msg, fromQueue, msgc, errc)

	// these functions will do their jobs when their channels have data
	go download(ctx, dl, processc, conn, d, errc, conn.GetLogger())
	go process(ctx, processc, upc, errc, conn.GetLogger())
	go up(ctx, upc, done, conn, bookname, errc, conn.GetLogger())

	dl <- msgparts[0]
	close(dl)

	// wait for either the done or errc channels to be sent to
	select {
	case err = <-errc:
		t.Stop()
		_ = os.RemoveAll(d)
		return err
	case <-ctx.Done():
		t.Stop()
		_ = os.RemoveAll(d)
		return ctx.Err()
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

func ProcessBook(ctx context.Context, msg bookpipeline.Qmsg, conn Pipeliner, process func(context.Context, chan string, chan string, chan error, *log.Logger), match *regexp.Regexp, fromQueue string, toQueue string) error {
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

	t := time.NewTicker(HeartbeatSeconds * time.Second)
	go heartbeat(conn, t, msg, fromQueue, msgc, errc)

	// these functions will do their jobs when their channels have data
	go download(ctx, dl, processc, conn, d, errc, conn.GetLogger())
	go process(ctx, processc, upc, errc, conn.GetLogger())
	if toQueue == conn.OCRPageQueueId() {
		go upAndQueue(ctx, upc, done, toQueue, conn, bookname, training, errc, conn.GetLogger())
	} else {
		go up(ctx, upc, done, conn, bookname, errc, conn.GetLogger())
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
		// if the error is in preprocessing / wipeonly, chances are that it will never
		// complete, and will fill the ocrpage queue with parts which succeeded
		// on each run, so in that case it's better to delete the message from
		// the queue and notify us.
		if fromQueue == conn.PreQueueId() || fromQueue == conn.WipeQueueId() || fromQueue == conn.PreNoWipeQueueId() {
			conn.Log("Deleting message from queue due to a bad error", fromQueue)
			err2 := conn.DelFromQueue(fromQueue, msg.Handle)
			if err2 != nil {
				conn.Log("Error deleting message from queue", err2)
			}
			ms, err2 := GetMailSettings()
			if err2 != nil {
				conn.Log("Failed to mail settings ", err2)
			}
			if err2 == nil && ms.server != "" {
				logs, err2 := getLogs()
				if err2 != nil {
					conn.Log("Failed to get logs ", err2)
					logs = ""
				}
				msg := fmt.Sprintf("To: %s\r\nFrom: %s\r\n"+
					"Subject: [bookpipeline] Error in wipeonly / preprocessing queue with %s\r\n\r\n"+
					" Fail message: %s\r\nFull log:\r\n%s\r\n",
					ms.to, ms.from, bookname, err, logs)
				host := fmt.Sprintf("%s:%s", ms.server, ms.port)
				auth := smtp.PlainAuth("", ms.user, ms.pass, ms.server)
				err2 = smtp.SendMail(host, auth, ms.from, []string{ms.to}, []byte(msg))
				if err2 != nil {
					conn.Log("Error sending email ", err2)
				}
			}
		}
		return err
	case <-ctx.Done():
		t.Stop()
		_ = os.RemoveAll(d)
		return ctx.Err()
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

// TODO: rather than relying on journald, would be nicer to save the logs
//       ourselves maybe, so that we weren't relying on a particular systemd
//       setup. this can be done by having the conn.Log also append line
//       to a file (though that would mean everything would have to go through
//       conn.Log, which we're not consistently doing yet). the correct thing
//       to do then would be to implement a new interface that covers the part
//       of log.Logger we use (e.g. Print and Printf), and then have an exported
//       conn struct that implements those, so that we could pass a log.Logger
//       or the new conn struct everywhere (we wouldn't be passing a log.Logger,
//       it's just good to be able to keep the compatibility)
func getLogs() (string, error) {
	cmd := exec.Command("journalctl", "-u", "bookpipeline", "-n", "all")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), err
}

func SaveLogs(conn Uploader, starttime int64, hostname string) error {
	logs, err := getLogs()
	if err != nil {
		return fmt.Errorf("Error getting logs, error: %v", err)
	}
	key := fmt.Sprintf("bookpipeline.log.%d.%s", starttime, hostname)
	path := filepath.Join(os.TempDir(), key)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Error creating log file: %v", err)
	}
	defer f.Close()
	_, err = f.WriteString(logs)
	if err != nil {
		return fmt.Errorf("Error saving log file: %v", err)
	}
	_ = f.Close()
	err = conn.Upload(conn.WIPStorageId(), key, path)
	if err != nil {
		return fmt.Errorf("Error uploading log: %v", err)
	}
	conn.Log("Log saved to", key)
	return nil
}
