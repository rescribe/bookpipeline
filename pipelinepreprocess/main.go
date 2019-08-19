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
	"path/filepath"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"

	"rescribe.xyz/go.git/preproc"
)

const usage = "Usage: pipelinepreprocess [-v]\n\nContinuously checks the preprocess queue for books.\nWhen a book is found it's downloaded from the S3 inprogress bucket, preprocessed, and the results are uploaded to the S3 inprogress bucket. The book name is then added to the ocr queue, and removed from the preprocess queue.\n\n-v  verbose\n"

// null writer to enable non-verbose logging to be discarded
type NullWriter bool
func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

var alreadydone *regexp.Regexp

const HeartbeatTime = 60
const PauseBetweenChecks = 60 * time.Second
const PreprocPattern = `_bin[0-9].[0-9].png`

// TODO: could restructure like so:
//       have the goroutine functions run outside of the main loop in the program,
//       so use them for multiple books indefinitely. would require finding a way to
//       signal when the queues need to be updated (e.g. when a book is finished)
//
// MAYBE use a struct holding config info ala downloader in
// https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/sdk-utilities.html
//
// TODO: consider having the download etc functions return a channel like a generator, like in rob pike's talk

type Clouder interface {
	Init() error
	ListObjects(bucket string, prefix string, names chan string) error
	Download(bucket string, key string, fn string) error
	Upload(bucket string, key string, path string) error
	CheckQueue(url string) (qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	QueueHeartbeat(t *time.Ticker, msgHandle string, qurl string) error
}

type Pipeliner interface {
	Clouder
	ListInProgress(bookname string, names chan string) error
	DownloadFromInProgress(key string, fn string) error
	UploadToInProgress(key string, path string) error
	CheckPreQueue() (qmsg, error)
	AddToOCRQueue(msg string) error
	DelFromPreQueue(handle string) error
	PreQueueHeartbeat(t *time.Ticker, msgHandle string) error
}

type qmsg struct {
	Handle, Body string
}

type awsConn struct {
	// these need to be set before running Init()
	region string
	logger *log.Logger

	// these are used internally
	sess *session.Session
        s3svc *s3.S3
        sqssvc *sqs.SQS
        downloader *s3manager.Downloader
	uploader *s3manager.Uploader
	prequrl, ocrqurl string
}

func (a awsConn) Init() error {
	if a.region == "" {
		return errors.New("No region set")
	}
	if a.logger == nil {
		return errors.New("No logger set")
	}

	var err error
	a.sess, err = session.NewSession(&aws.Config{
		Region: aws.String(a.region),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to set up aws session: %s", err))
	}
	a.s3svc = s3.New(a.sess)
	a.sqssvc = sqs.New(a.sess)
	a.downloader = s3manager.NewDownloader(a.sess)
	a.uploader = s3manager.NewUploader(a.sess)

        a.logger.Println("Getting preprocess queue URL")
        result, err := a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
                QueueName: aws.String("rescribepreprocess"),
        })
        if err != nil {
                return errors.New(fmt.Sprintf("Error getting preprocess queue URL: %s", err))
        }
        a.prequrl = *result.QueueUrl
        a.logger.Println("preprocess queue URL", a.prequrl)

        a.logger.Println("Getting OCR queue URL")
        result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
                QueueName: aws.String("rescribeocr"),
        })
        if err != nil {
                return errors.New(fmt.Sprintf("Error getting OCR queue URL: %s", err))
        }
        a.ocrqurl = *result.QueueUrl
	return nil
}

func (a awsConn) CheckQueue(url string) (qmsg, error) {
	msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
		MaxNumberOfMessages: aws.Int64(1),
		VisibilityTimeout: aws.Int64(HeartbeatTime * 2),
		WaitTimeSeconds: aws.Int64(20),
		QueueUrl: &url,
	})
	if err != nil {
		return qmsg{}, err
	}

	if len(msgResult.Messages) > 0 {
		msg := qmsg{ Handle: *msgResult.Messages[0].ReceiptHandle, Body: *msgResult.Messages[0].Body }
		a.logger.Println("Message received:", msg.Body)
		return msg, nil
	} else {
		return qmsg{}, nil
	}
}

func (a awsConn) CheckPreQueue() (qmsg, error) {
	a.logger.Println("Checking preprocessing queue for new messages:", a.prequrl)
	return a.CheckQueue(a.prequrl)
}

func (a awsConn) QueueHeartbeat(t *time.Ticker, msgHandle string, qurl string) error {
	for _ = range t.C {
		duration := int64(HeartbeatTime * 2)
		_, err := a.sqssvc.ChangeMessageVisibility(&sqs.ChangeMessageVisibilityInput{
			ReceiptHandle: &msgHandle,
			QueueUrl: &qurl,
			VisibilityTimeout: &duration,
		})
		if err != nil {
			return errors.New(fmt.Sprintf("Heartbeat error updating queue duration: %s", err))
		}
	}
	return nil
}

func (a awsConn) PreQueueHeartbeat(t *time.Ticker, msgHandle string) error {
	a.logger.Println("Starting preprocess queue heartbeat for", msgHandle)
	return a.QueueHeartbeat(t, msgHandle, a.prequrl)
}

func (a awsConn) ListObjects(bucket string, prefix string, names chan string) error {
	err := a.s3svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}, func(page *s3.ListObjectsV2Output, last bool) bool {
		for _, r := range page.Contents {
			if alreadydone.MatchString(*r.Key) {
				a.logger.Println("Skipping item that looks like it has already been processed", *r.Key)
				continue
			}
			names <- *r.Key
		}
		return true
	})
	close(names)
	return err
}

func (a awsConn) ListInProgress(bookname string, names chan string) error {
	return a.ListObjects("rescribeinprogress", bookname, names)
}

func (a awsConn) AddToQueue(url string, msg string) error {
	_, err := a.sqssvc.SendMessage(&sqs.SendMessageInput{
		MessageBody: &msg,
		QueueUrl: &url,
	})
	return err
}

func (a awsConn) AddToOCRQueue(msg string) error {
	return a.AddToQueue(a.ocrqurl, msg)
}

func (a awsConn) DelFromQueue(url string, handle string) error {
	_, err := a.sqssvc.DeleteMessage(&sqs.DeleteMessageInput{
		QueueUrl: &url,
		ReceiptHandle: &handle,
	})
	return err
}

func (a awsConn) DelFromPreQueue(handle string) error {
	return a.DelFromQueue(a.prequrl, handle)
}

func (a awsConn) Download(bucket string, key string, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = a.downloader.Download(f,
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key: &key,
	})
	return err
}

func (a awsConn) DownloadFromInProgress(key string, path string) error {
	a.logger.Println("Downloading", key)
	return a.Download("rescribeinprogress", key, path)
}

func (a awsConn) Upload(bucket string, key string, path string) error {
	file, err := os.Open(path)
	if err != nil {
		log.Fatalln("Failed to open file", path, err)
	}
	defer file.Close()

	_, err = a.uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	return err
}

func (a awsConn) UploadToInProgress(key string, path string) error {
	a.logger.Println("Uploading", path)
	return a.Upload("rescribeinprogress", key, path)
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

func heartbeat(h *time.Ticker, msgHandle string, qurl string, sqssvc *sqs.SQS) {
	for _ = range h.C {
		duration := int64(HeartbeatTime * 2)
		_, err := sqssvc.ChangeMessageVisibility(&sqs.ChangeMessageVisibilityInput{
			ReceiptHandle: &msgHandle,
			QueueUrl: &qurl,
			VisibilityTimeout: &duration,
		})
		if err != nil {
			log.Fatalln("Error updating queue duration:", err)
		}
	}
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

	alreadydone = regexp.MustCompile(PreprocPattern)

	var conn Pipeliner
	conn = awsConn{ region: "eu-west-2", logger: verboselog }

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
