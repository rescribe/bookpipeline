package main
// TODO: have logs go somewhere useful, like email
// TODO: handle errors more smartly than just always fatal erroring
//       - read the sdk guarantees on retrying and ensure we retry some times before giving up if necessary
//       - cancel the current book processing rather than killing the program in the case of a nonrecoverable error 
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

const usage = "Usage: pipelinepreprocess [-v]\n\nContinuously checks the preprocess queue for books.\nWhen a book is found it's downloaded from the S3 inprogress bucket, preprocessed, and the results are uploaded to the S3 inprogress bucket. The book name is then added to the ocr queue, and removed from the preprocess queue.\n\n-v  verbose\n"

const training = "rescribealphav5" // TODO: allow to set on cmdline

// null writer to enable non-verbose logging to be discarded
type NullWriter bool
func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

const PauseBetweenChecks = 60 * time.Second

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

func download(dl chan string, pre chan string, conn Pipeliner, dir string) {
	for key := range dl {
		fn := filepath.Join(dir, filepath.Base(key))
		err := conn.DownloadFromInProgress(key, fn)
		if err != nil {
			log.Fatalln("Failed to download", key, err)
		}
		pre <- fn
	}
	close(pre)
}

func up(c chan string, done chan bool, conn Pipeliner, bookname string) {
	for path := range c {
		name := filepath.Base(path)
		key := filepath.Join(bookname, name)
		err := conn.UploadToInProgress(key, path)
		if err != nil {
			log.Fatalln("Failed to upload", path, err)
		}
	}

	done <- true
}

func preprocess(pre chan string, up chan string, logger *log.Logger) {
	for path := range pre {
		logger.Println("Preprocessing", path)
		done, err := preproc.PreProcMulti(path, []float64{0.1, 0.2, 0.4, 0.5}, "binary", 0, true, 5, 30)
		if err != nil {
			// TODO: have error channel to signal that things are screwy, which
			// can close channels and stop the heartbeat, rather than just kill
			// the whole program
			log.Fatalln("Error preprocessing", path, err)
		}
		for _, p := range done {
			up <- p
		}
	}
	close(up)
}

// TODO: use Tesseract API rather than calling the executable
func ocr(toocr chan string, up chan string, logger *log.Logger) {
	for path := range toocr {
		logger.Println("OCRing", path)
		name := strings.Replace(path, ".png", "", 1) // TODO: handle any file extension
		cmd := exec.Command("tesseract", "-l", training, path, name, "hocr")
		err := cmd.Run()
		if err != nil {
			// TODO: have error channel to signal that things are screwy, which
			// can close channels and stop the heartbeat, rather than just kill
			// the whole program
			log.Fatalln("Error ocring", path, err)
		}
		up <- name + ".hocr"
	}
	close(up)
}

func preProcBook(msg Qmsg, conn Pipeliner) error {
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
	upc := make(chan string) // TODO: rename
	done := make(chan bool) // this is just to communicate when up has finished, so the queues can be updated

	// these functions will do their jobs when their channels have data
	go download(dl, pre, conn, d)
	go preprocess(pre, upc, conn.Logger())
	go up(upc, done, conn, bookname)

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

	// wait for the done channel to be posted to
	<-done

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
	upc := make(chan string) // TODO: rename
	done := make(chan bool) // this is just to communicate when up has finished, so the queues can be updated

	// these functions will do their jobs when their channels have data
	go download(dl, ocrc, conn, d)
	go ocr(ocrc, upc, conn.Logger())
	go up(upc, done, conn, bookname)

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

	// wait for the done channel to be posted to
	<-done

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
	conn = &awsConn{ region: "eu-west-2", logger: verboselog }

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
		case <- checkPreQueue:
			msg, err := conn.CheckPreQueue()
			if err != nil {
				log.Println("Error checking preprocess queue", err)
				checkPreQueue = time.After(PauseBetweenChecks)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on preprocess queue, sleeping")
				checkPreQueue = time.After(PauseBetweenChecks)
				continue
			}
			err = preProcBook(msg, conn)
			if err != nil {
				log.Println("Error during preprocess", err)
			}
			checkPreQueue = time.After(0)
		case <- checkOCRQueue:
			msg, err := conn.CheckOCRQueue()
			if err != nil {
				log.Println("Error checking OCR queue", err)
				checkOCRQueue = time.After(PauseBetweenChecks)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on OCR queue, sleeping")
				checkOCRQueue = time.After(PauseBetweenChecks)
				continue
			}
			err = ocrBook(msg, conn)
			if err != nil {
				log.Println("Error during OCR process", err)
			}
			checkOCRQueue = time.After(0)
		}
	}
}
