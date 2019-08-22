package main

// TODO: have logs go somewhere useful, like email
// TODO: check if images are prebinarised and if so skip multiple binarisation

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"rescribe.xyz/go.git/preproc"
)

const usage = `Usage: bookpipeline [-v]

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

-v  verbose
`

const training = "rescribealphav5" // TODO: allow to set on cmdline

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

const PauseBetweenChecks = 3 * time.Minute

type Clouder interface {
	Init() error
	ListObjects(bucket string, prefix string) ([]string, error)
	Download(bucket string, key string, fn string) error
	Upload(bucket string, key string, path string) error
	CheckQueue(url string) (Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	QueueHeartbeat(t *time.Ticker, msgHandle string, qurl string) error
}

type Pipeliner interface {
	Clouder
	ListToPreprocess(bookname string) ([]string, error)
	ListToOCR(bookname string) ([]string, error)
	DownloadFromInProgress(key string, fn string) error
	UploadToInProgress(key string, path string) error
	CheckPreQueue() (Qmsg, error)
	CheckOCRQueue() (Qmsg, error)
	CheckAnalyseQueue() (Qmsg, error)
	AddToOCRQueue(msg string) error
	AddToAnalyseQueue(msg string) error
	DelFromPreQueue(handle string) error
	DelFromOCRQueue(handle string) error
	PreQueueHeartbeat(t *time.Ticker, msgHandle string) error
	OCRQueueHeartbeat(t *time.Ticker, msgHandle string) error
	Logger() *log.Logger
}

type Qmsg struct {
	Handle, Body string
}

func download(dl chan string, process chan string, conn Pipeliner, dir string, errc chan error) {
	for key := range dl {
		fn := filepath.Join(dir, filepath.Base(key))
		err := conn.DownloadFromInProgress(key, fn)
		if err != nil {
			close(process)
			errc <- err
			return
		}
		process <- fn
	}
	close(process)
}

func up(c chan string, done chan bool, conn Pipeliner, bookname string, errc chan error) {
	for path := range c {
		name := filepath.Base(path)
		key := filepath.Join(bookname, name)
		err := conn.UploadToInProgress(key, path)
		if err != nil {
			errc <- err
			return
		}
	}

	done <- true
}

func preprocess(pre chan string, up chan string, logger *log.Logger, errc chan error) {
	for path := range pre {
		logger.Println("Preprocessing", path)
		done, err := preproc.PreProcMulti(path, []float64{0.1, 0.2, 0.4, 0.5}, "binary", 0, true, 5, 30)
		if err != nil {
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

// TODO: use Tesseract API rather than calling the executable
func ocr(toocr chan string, up chan string, logger *log.Logger, errc chan error) {
	for path := range toocr {
		logger.Println("OCRing", path)
		name := strings.Replace(path, ".png", "", 1) // TODO: handle any file extension
		cmd := exec.Command("tesseract", "-l", training, path, name, "hocr")
		err := cmd.Run()
		if err != nil {
			close(up)
			errc <- errors.New(fmt.Sprintf("Error ocring %s: %s", path, err))
			return
		}
		up <- name + ".hocr"
	}
	close(up)
}

func preprocBook(msg Qmsg, conn Pipeliner) error {
	bookname := msg.Body

	t := time.NewTicker(HeartbeatTime * time.Second)
	go conn.PreQueueHeartbeat(t, msg.Handle)

	d := filepath.Join(os.TempDir(), bookname)
	err := os.MkdirAll(d, 0755)
	if err != nil {
		t.Stop()
		return errors.New(fmt.Sprintf("Failed to create directory %s: %s", d, err))
	}

	dl := make(chan string)
	pre := make(chan string)
	upc := make(chan string)
	done := make(chan bool)
	errc := make(chan error)

	// these functions will do their jobs when their channels have data
	go download(dl, pre, conn, d, errc)
	go preprocess(pre, upc, conn.Logger(), errc)
	go up(upc, done, conn, bookname, errc)

	conn.Logger().Println("Getting list of objects to download")
	todl, err := conn.ListToPreprocess(bookname)
	if err != nil {
		t.Stop()
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Failed to get list of files for book %s: %s", bookname, err))
	}
	for _, d := range todl {
		dl <- d
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

	conn.Logger().Println("Sending", bookname, "to OCR queue")
	err = conn.AddToOCRQueue(bookname)
	if err != nil {
		t.Stop()
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Error adding to ocr queue %s: %s", bookname, err))
	}

	t.Stop()

	conn.Logger().Println("Deleting original message from preprocessing queue")
	err = conn.DelFromPreQueue(msg.Handle)
	if err != nil {
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Error deleting message from preprocessing queue: %s", err))
	}

	err = os.RemoveAll(d)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to remove directory %s: %s", d, err))
	}

	return nil
}

// TODO: this is very similar to preprocBook; try to at least mostly merge them
func ocrBook(msg Qmsg, conn Pipeliner) error {
	bookname := msg.Body

	t := time.NewTicker(HeartbeatTime * time.Second)
	go conn.OCRQueueHeartbeat(t, msg.Handle)

	d := filepath.Join(os.TempDir(), bookname)
	err := os.MkdirAll(d, 0755)
	if err != nil {
		t.Stop()
		return errors.New(fmt.Sprintf("Failed to create directory %s: %s", d, err))
	}

	dl := make(chan string)
	ocrc := make(chan string)
	upc := make(chan string)
	done := make(chan bool)
	errc := make(chan error)

	// these functions will do their jobs when their channels have data
	go download(dl, ocrc, conn, d, errc)
	go ocr(ocrc, upc, conn.Logger(), errc)
	go up(upc, done, conn, bookname, errc)

	conn.Logger().Println("Getting list of objects to download")
	todl, err := conn.ListToOCR(bookname)
	if err != nil {
		t.Stop()
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Failed to get list of files for book %s: %s", bookname, err))
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

	conn.Logger().Println("Sending", bookname, "to analyse queue")
	err = conn.AddToAnalyseQueue(bookname)
	if err != nil {
		t.Stop()
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Error adding to analyse queue %s: %s", bookname, err))
	}

	t.Stop()

	conn.Logger().Println("Deleting original message from OCR queue")
	err = conn.DelFromOCRQueue(msg.Handle)
	if err != nil {
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Error deleting message from OCR queue: %s", err))
	}

	err = os.RemoveAll(d)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to remove directory %s: %s", d, err))
	}

	return nil
}

func main() {
	var verboselog *log.Logger
	if len(os.Args) > 1 {
		if os.Args[1] == "-v" {
			verboselog = log.New(os.Stdout, "", log.LstdFlags)
		} else {
			log.Fatal(usage)
		}
	} else {
		var n NullWriter
		verboselog = log.New(n, "", log.LstdFlags)
	}

	var conn Pipeliner
	conn = &awsConn{region: "eu-west-2", logger: verboselog}

	verboselog.Println("Setting up AWS session")
	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}
	verboselog.Println("Finished setting up AWS session")

	var checkPreQueue <-chan time.Time
	var checkOCRQueue <-chan time.Time
	checkPreQueue = time.After(0)
	checkOCRQueue = time.After(0)

	for {
		select {
		case <-checkPreQueue:
			msg, err := conn.CheckPreQueue()
			checkPreQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking preprocess queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on preprocess queue, sleeping")
				continue
			}
			err = preprocBook(msg, conn)
			if err != nil {
				log.Println("Error during preprocess", err)
			}
		case <-checkOCRQueue:
			msg, err := conn.CheckOCRQueue()
			checkOCRQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking OCR queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on OCR queue, sleeping")
				continue
			}
			err = ocrBook(msg, conn)
			if err != nil {
				log.Println("Error during OCR process", err)
			}
		}
	}
}
