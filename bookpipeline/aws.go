package bookpipeline

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const PreprocPattern = `_bin[0-9].[0-9].png`

type Qmsg struct {
        Handle, Body string
}

type AwsConn struct {
	// these need to be set before running Init()
	Region string
	Logger *log.Logger

	// these are used internally
	sess                          *session.Session
	s3svc                         *s3.S3
	sqssvc                        *sqs.SQS
	downloader                    *s3manager.Downloader
	uploader                      *s3manager.Uploader
	prequrl, ocrqurl, analysequrl string
	wipstorageid                  string
}

func (a *AwsConn) Init() error {
	if a.Region == "" {
		return errors.New("No Region set")
	}
	if a.Logger == nil {
		return errors.New("No logger set")
	}

	var err error
	a.sess, err = session.NewSession(&aws.Config{
		Region: aws.String(a.Region),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to set up aws session: %s", err))
	}
	a.s3svc = s3.New(a.sess)
	a.sqssvc = sqs.New(a.sess)
	a.downloader = s3manager.NewDownloader(a.sess)
	a.uploader = s3manager.NewUploader(a.sess)

	a.Logger.Println("Getting preprocess queue URL")
	result, err := a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String("rescribepreprocess"),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting preprocess queue URL: %s", err))
	}
	a.prequrl = *result.QueueUrl

	a.Logger.Println("Getting OCR queue URL")
	result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String("rescribeocr"),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting OCR queue URL: %s", err))
	}
	a.ocrqurl = *result.QueueUrl

	a.Logger.Println("Getting analyse queue URL")
	result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String("rescribeanalyse"),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting analyse queue URL: %s", err))
	}
	a.analysequrl = *result.QueueUrl

	a.wipstorageid = "rescribeinprogress"

	return nil
}

func (a *AwsConn) CheckQueue(url string, timeout int64) (Qmsg, error) {
	msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
		MaxNumberOfMessages: aws.Int64(1),
		VisibilityTimeout:   &timeout,
		WaitTimeSeconds:     aws.Int64(20),
		QueueUrl:            &url,
	})
	if err != nil {
		return Qmsg{}, err
	}

	if len(msgResult.Messages) > 0 {
		msg := Qmsg{Handle: *msgResult.Messages[0].ReceiptHandle, Body: *msgResult.Messages[0].Body}
		a.Logger.Println("Message received:", msg.Body)
		return msg, nil
	} else {
		return Qmsg{}, nil
	}
}

func (a *AwsConn) QueueHeartbeat(msgHandle string, qurl string, duration int64) error {
	_, err := a.sqssvc.ChangeMessageVisibility(&sqs.ChangeMessageVisibilityInput{
		ReceiptHandle:     &msgHandle,
		QueueUrl:          &qurl,
		VisibilityTimeout: &duration,
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Heartbeat error updating queue duration: %s", err))
	}
	return nil
}

func (a *AwsConn) PreQueueId() string {
	return a.prequrl
}

func (a *AwsConn) OCRQueueId() string {
	return a.ocrqurl
}

func (a *AwsConn) AnalyseQueueId() string {
	return a.analysequrl
}

func (a *AwsConn) WIPStorageId() string {
	return a.wipstorageid
}

func (a *AwsConn) ListObjects(bucket string, prefix string) ([]string, error) {
	var names []string
	err := a.s3svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}, func(page *s3.ListObjectsV2Output, last bool) bool {
		for _, r := range page.Contents {
			names = append(names, *r.Key)
		}
		return true
	})
	return names, err
}

func (a *AwsConn) AddToQueue(url string, msg string) error {
	_, err := a.sqssvc.SendMessage(&sqs.SendMessageInput{
		MessageBody: &msg,
		QueueUrl:    &url,
	})
	return err
}

func (a *AwsConn) DelFromQueue(url string, handle string) error {
	_, err := a.sqssvc.DeleteMessage(&sqs.DeleteMessageInput{
		QueueUrl:      &url,
		ReceiptHandle: &handle,
	})
	return err
}

func (a *AwsConn) Download(bucket string, key string, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = a.downloader.Download(f,
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    &key,
		})
	return err
}

func (a *AwsConn) Upload(bucket string, key string, path string) error {
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

func (a *AwsConn) GetLogger() *log.Logger {
	return a.Logger
}
