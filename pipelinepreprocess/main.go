package main
// TODO: have logs go somewhere useful, like email
// TODO: handle errors more smartly than just always fatal erroring
//       - read the sdk guarantees on retrying and ensure we retry some times before giving up if necessary
//       - cancel the current book processing rather than killing the program in the case of a nonrecoverable error 
// TODO: check if images are prebinarised and if so skip multiple binarisation

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"rescribe.xyz/go.git/preproc"
)

const usage = "Usage: pipelinepreprocess [-v]\n\nContinuously checks the preprocess queue for books.\nWhen a book is found it's downloaded from the S3 inprogress bucket, preprocessed, and the results are uploaded to the S3 inprogress bucket. The book name is then added to the ocr queue, and removed from the preprocess queue.\n\n-v  verbose\n"

// null writer to enable non-verbose logging to be discarded
type NullWriter bool
func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

const PauseBetweenChecks = 60 * time.Second

// TODO: consider having the download etc functions return a channel like a generator, like in rob pike's talk

type Clouder interface {
	Init() error
	ListObjects(bucket string, prefix string, names chan string) error
	Download(bucket string, key string, fn string) error
	Upload(bucket string, key string, path string) error
	CheckQueue(url string) (Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	QueueHeartbeat(t *time.Ticker, msgHandle string, qurl string) error
}

type Pipeliner interface {
	Clouder
	ListInProgress(bookname string, names chan string) error
	DownloadFromInProgress(key string, fn string) error
	UploadToInProgress(key string, path string) error
	CheckPreQueue() (Qmsg, error)
	AddToOCRQueue(msg string) error
	DelFromPreQueue(handle string) error
	PreQueueHeartbeat(t *time.Ticker, msgHandle string) error
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

func preprocess(pre chan string, up chan string, logger *log.Logger) {
	for path := range pre {
		logger.Println("Preprocessing", path)
		done, err := preproc.PreProcMulti(path, []float64{0.1, 0.2, 0.4, 0.5}, "binary", 0, true, 5, 30)
		if err != nil {
			log.Fatalln("Error preprocessing", path, err)
		}
		for _, p := range done {
			up <- p
		}
	}
	close(up)
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

	for {
		msg, err := conn.CheckPreQueue()
		if err != nil {
			log.Fatalln("Error checking preprocess queue", err)
		}
		if msg.Handle == "" {
			verboselog.Println("No message received, sleeping")
			time.Sleep(PauseBetweenChecks)
			continue
		}
		bookname := msg.Body

		t := time.NewTicker(HeartbeatTime * time.Second)
		go conn.PreQueueHeartbeat(t, msg.Handle)


		d := filepath.Join(os.TempDir(), bookname)
		err = os.MkdirAll(d, 0755)
		if err != nil {
			log.Fatalln("Failed to create directory", d, err)
		}

		dl := make(chan string)
		pre := make(chan string)
		upc := make(chan string) // TODO: rename
		done := make(chan bool) // this is just to communicate when up has finished, so the queues can be updated

		// these functions will do their jobs when their channels have data
		go download(dl, pre, conn, d)
		go preprocess(pre, upc, verboselog)
		go up(upc, done, conn, bookname)


		verboselog.Println("Getting list of objects to download")
		err = conn.ListInProgress(bookname, dl)
		if err != nil {
			log.Fatalln("Failed to get list of files for book", bookname, err)
		}

		// wait for the done channel to be posted to
		<-done

		verboselog.Println("Sending", bookname, "to OCR queue")
		err = conn.AddToOCRQueue(bookname)
		if err != nil {
			log.Fatalln("Error adding to ocr queue", bookname, err)
		}

		t.Stop()

		verboselog.Println("Deleting original message from preprocessing queue")
		err = conn.DelFromPreQueue(msg.Handle)
		if err != nil {
			log.Fatalln("Error deleting message from preprocessing queue", err)
		}

		err = os.RemoveAll(d)
		if err != nil {
			log.Fatalln("Failed to remove directory", d, err)
		}
	}
}
