package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const PreprocPattern = `_bin[0-9].[0-9].png`
const HeartbeatTime = 60

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
	prequrl, ocrqurl, analysequrl string
}

func (a *awsConn) Init() error {
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

        a.logger.Println("Getting OCR queue URL")
        result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
                QueueName: aws.String("rescribeocr"),
        })
        if err != nil {
                return errors.New(fmt.Sprintf("Error getting OCR queue URL: %s", err))
        }
        a.ocrqurl = *result.QueueUrl

        a.logger.Println("Getting analyse queue URL")
        result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
                QueueName: aws.String("rescribeanalyse"),
        })
        if err != nil {
                return errors.New(fmt.Sprintf("Error getting analyse queue URL: %s", err))
        }
        a.analysequrl = *result.QueueUrl

	return nil
}

func (a *awsConn) CheckQueue(url string) (Qmsg, error) {
	msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
		MaxNumberOfMessages: aws.Int64(1),
		VisibilityTimeout: aws.Int64(HeartbeatTime * 2),
		WaitTimeSeconds: aws.Int64(20),
		QueueUrl: &url,
	})
	if err != nil {
		return Qmsg{}, err
	}

	if len(msgResult.Messages) > 0 {
		msg := Qmsg{ Handle: *msgResult.Messages[0].ReceiptHandle, Body: *msgResult.Messages[0].Body }
		a.logger.Println("Message received:", msg.Body)
		return msg, nil
	} else {
		return Qmsg{}, nil
	}
}

func (a *awsConn) CheckPreQueue() (Qmsg, error) {
	a.logger.Println("Checking preprocessing queue for new messages")
	return a.CheckQueue(a.prequrl)
}

func (a *awsConn) CheckOCRQueue() (Qmsg, error) {
	a.logger.Println("Checking OCR queue for new messages")
	return a.CheckQueue(a.ocrqurl)
}

func (a *awsConn) CheckAnalyseQueue() (Qmsg, error) {
	a.logger.Println("Checking analyse queue for new messages")
	return a.CheckQueue(a.ocrqurl)
}

func (a *awsConn) QueueHeartbeat(t *time.Ticker, msgHandle string, qurl string) error {
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

func (a *awsConn) PreQueueHeartbeat(t *time.Ticker, msgHandle string) error {
	a.logger.Println("Starting preprocess queue heartbeat")
	return a.QueueHeartbeat(t, msgHandle, a.prequrl)
}

func (a *awsConn) OCRQueueHeartbeat(t *time.Ticker, msgHandle string) error {
	a.logger.Println("Starting ocr queue heartbeat")
	return a.QueueHeartbeat(t, msgHandle, a.ocrqurl)
}

func (a *awsConn) ListObjects(bucket string, prefix string, names chan string) {
	err := a.s3svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}, func(page *s3.ListObjectsV2Output, last bool) bool {
		for _, r := range page.Contents {
			names <- *r.Key
		}
		return true
	})
	close(names)
	if err != nil {
		// TODO: handle error properly
		log.Println("Error getting objects")
	}
}

func (a *awsConn) ListToPreprocess(bookname string, names chan string) error {
	objs := make(chan string)
	preprocessed := regexp.MustCompile(PreprocPattern)
	go a.ListObjects("rescribeinprogress", bookname, objs)
	// Filter out any object that looks like it's already been preprocessed
	for n := range objs {
		if preprocessed.MatchString(n) {
			a.logger.Println("Skipping item that looks like it has already been processed", n)
			continue
		}
		names <- n
	}
	close(names)
	// TODO: handle errors from ListObjects
	return nil
}

func (a *awsConn) ListToOCR(bookname string, names chan string) error {
	objs := make(chan string)
	preprocessed := regexp.MustCompile(PreprocPattern)
	go a.ListObjects("rescribeinprogress", bookname, objs)
	a.logger.Println("Completed running listobjects")
	// Filter out any object that looks like it hasn't already been preprocessed
	for n := range objs {
		if ! preprocessed.MatchString(n) {
			a.logger.Println("Skipping item that looks like it is not preprocessed", n)
			continue
		}
		names <- n
	}
	close(names)
	// TODO: handle errors from ListObjects
	return nil
}

func (a *awsConn) AddToQueue(url string, msg string) error {
	_, err := a.sqssvc.SendMessage(&sqs.SendMessageInput{
		MessageBody: &msg,
		QueueUrl: &url,
	})
	return err
}

func (a *awsConn) AddToOCRQueue(msg string) error {
	return a.AddToQueue(a.ocrqurl, msg)
}

func (a *awsConn) AddToAnalyseQueue(msg string) error {
	return a.AddToQueue(a.analysequrl, msg)
}

func (a *awsConn) DelFromQueue(url string, handle string) error {
	_, err := a.sqssvc.DeleteMessage(&sqs.DeleteMessageInput{
		QueueUrl: &url,
		ReceiptHandle: &handle,
	})
	return err
}

func (a *awsConn) DelFromPreQueue(handle string) error {
	return a.DelFromQueue(a.prequrl, handle)
}

func (a *awsConn) DelFromOCRQueue(handle string) error {
	return a.DelFromQueue(a.ocrqurl, handle)
}

func (a *awsConn) Download(bucket string, key string, path string) error {
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

func (a *awsConn) DownloadFromInProgress(key string, path string) error {
	a.logger.Println("Downloading", key)
	return a.Download("rescribeinprogress", key, path)
}

func (a *awsConn) Upload(bucket string, key string, path string) error {
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

func (a *awsConn) UploadToInProgress(key string, path string) error {
	a.logger.Println("Uploading", path)
	return a.Upload("rescribeinprogress", key, path)
}

func (a *awsConn) Logger() *log.Logger {
	return a.logger
}
