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
var verboselog *log.Logger

const HeartbeatTime = 60
const PauseBetweenChecks = 60 * time.Second

// TODO: could restructure like so:
//       have the goroutine functions run outside of the main loop in the program,
//       so use them for multiple books indefinitely. would require finding a way to
//       signal when the queues need to be updated (e.g. when a book is finished)
//
// MAYBE use a struct holding config info ala downloader in
// https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/sdk-utilities.html
//
// TODO: consider having the download etc functions return a channel like a generator, like in rob pike's talk

func download(dl chan string, pre chan string, downloader *s3manager.Downloader, dir string) {
	for key := range dl {
		verboselog.Println("Downloading", key)
		fn := filepath.Join(dir, filepath.Base(key))
		f, err := os.Create(fn)
		if err != nil {
			log.Fatalln("Failed to create file", fn, err)
		}
		defer f.Close()

		_, err = downloader.Download(f,
			&s3.GetObjectInput{
				Bucket: aws.String("rescribeinprogress"),
				Key: &key })
		if err != nil {
			log.Fatalln("Failed to download", key, err)
		}
		pre <- fn
	}
	close(pre)
}

func preprocess(pre chan string, up chan string) {
	for path := range pre {
		verboselog.Println("Preprocessing", path)
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

func up(c chan string, done chan bool, uploader *s3manager.Uploader, bookname string) {
	for path := range c {
		verboselog.Println("Uploading", path)
		name := filepath.Base(path)
		file, err := os.Open(path)
		if err != nil {
			log.Fatalln("Failed to open file", path, err)
		}
		defer file.Close()

		_, err = uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String("rescribeinprogress"),
			Key:    aws.String(filepath.Join(bookname, name)),
			Body:   file,
		})
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

	verboselog.Println("Setting up AWS session")
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("eu-west-2"),
	})
	if err != nil {
		log.Fatalln("Error: failed to set up aws session:", err)
	}
	s3svc := s3.New(sess)
	sqssvc := sqs.New(sess)
	downloader := s3manager.NewDownloader(sess)
	uploader := s3manager.NewUploader(sess)

	preqname := "rescribepreprocess"
	verboselog.Println("Getting Queue URL for", preqname)
	result, err := sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(preqname),
	})
	if err != nil {
		log.Fatalln("Error getting queue URL for", preqname, ":", err)
	}
	prequrl := *result.QueueUrl

	ocrqname := "rescribeocr"
	verboselog.Println("Getting Queue URL for", ocrqname)
	result, err = sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(ocrqname),
	})
	if err != nil {
		log.Fatalln("Error getting queue URL for", ocrqname, ":", err)
	}
	ocrqurl := *result.QueueUrl

	for {
		verboselog.Println("Checking preprocessing queue for new messages")
		msgResult, err := sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
			MaxNumberOfMessages: aws.Int64(1),
			VisibilityTimeout: aws.Int64(HeartbeatTime * 2),
			WaitTimeSeconds: aws.Int64(20),
			QueueUrl: &prequrl,
		})
		if err != nil {
			log.Fatalln("Error checking queue", preqname, ":", err)
		}

		var bookname string
		if len(msgResult.Messages) > 0 {
			bookname = *msgResult.Messages[0].Body
			verboselog.Println("Message received:", bookname)
		} else {
			verboselog.Println("No message received, sleeping")
			time.Sleep(PauseBetweenChecks)
			continue
		}

		verboselog.Println("Starting heartbeat every", HeartbeatTime, "seconds")
		t := time.NewTicker(HeartbeatTime * time.Second)
		go heartbeat(t, *msgResult.Messages[0].ReceiptHandle, prequrl, sqssvc)


		d := filepath.Join(os.TempDir(), bookname)
		err = os.Mkdir(d, 0755)
		if err != nil {
			log.Fatalln("Failed to create directory", d, err)
		}

		dl := make(chan string)
		pre := make(chan string)
		upc := make(chan string) // TODO: rename
		done := make(chan bool) // this is just to communicate when up has finished, so the queues can be updated

		// these functions will do their jobs when their channels have data
		go download(dl, pre, downloader, d)
		go preprocess(pre, upc)
		go up(upc, done, uploader, bookname)


		verboselog.Println("Getting list of appropriate objects to download")
		err = s3svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{
			Bucket: aws.String("rescribeinprogress"),
			Prefix: aws.String(bookname),
		}, func(page *s3.ListObjectsV2Output, last bool) bool {
			for _, r := range page.Contents {
				dl <- *r.Key
			}
			return true
		})
		close(dl)


		// wait for the done channel to be posted to
		<-done

		verboselog.Println("Sending", bookname, "to queue", ocrqurl)
		_, err = sqssvc.SendMessage(&sqs.SendMessageInput{
			MessageBody: aws.String(bookname),
			QueueUrl: &ocrqurl,
		})
		if err != nil {
			log.Fatalln("Error sending message to queue", ocrqname, ":", err)
		}

		t.Stop()

		verboselog.Println("Deleting original message from queue", prequrl)
		_, err = sqssvc.DeleteMessage(&sqs.DeleteMessageInput{
			QueueUrl: &prequrl,
			ReceiptHandle: msgResult.Messages[0].ReceiptHandle,
		})
		if err != nil {
			log.Fatalln("Error deleting message from queue", preqname, ":", err)
		}
	}
}
