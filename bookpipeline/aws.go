package bookpipeline

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const PreprocPattern = `_bin[0-9].[0-9].png`
const heartbeatRetry = 10

type Qmsg struct {
	Id, Handle, Body string
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
		msg := Qmsg{Id: *msgResult.Messages[0].MessageId,
			Handle: *msgResult.Messages[0].ReceiptHandle,
			Body:   *msgResult.Messages[0].Body}
		a.Logger.Println("Message received:", msg.Body)
		return msg, nil
	} else {
		return Qmsg{}, nil
	}
}

// QueueHeartbeat updates the visibility timeout of a message. This
// ensures that the message remains "in flight", meaning that it
// cannot be seen by other processes, but if this process fails the
// timeout will expire and it will go back to being available for
// any other process to retrieve and process.
//
// SQS only allows messages to be "in flight" for up to 12 hours, so
// this will detect if the request for an update to visibility timeout
// fails, and if so will attempt to find the message on the queue, and
// return it, as the handle will have changed.
func (a *AwsConn) QueueHeartbeat(msg Qmsg, qurl string, duration int64) (Qmsg, error) {
	_, err := a.sqssvc.ChangeMessageVisibility(&sqs.ChangeMessageVisibilityInput{
		ReceiptHandle:     &msg.Handle,
		QueueUrl:          &qurl,
		VisibilityTimeout: &duration,
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)

		// Check if the visibility timeout has exceeded the maximum allowed,
		// and if so try to find the message again to get a new handle.
		if ok && aerr.Code() == "InvalidParameterValue" {
			// Try heartbeatRetry times to find the message
			for range [heartbeatRetry]bool{} {
				// Wait a little in case existing visibilitytimeout needs to expire
				time.Sleep((time.Duration(duration) / heartbeatRetry) * time.Second)

				msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
					MaxNumberOfMessages: aws.Int64(10),
					VisibilityTimeout:   &duration,
					WaitTimeSeconds:     aws.Int64(20),
					QueueUrl:            &qurl,
				})
				if err != nil {
					return Qmsg{}, errors.New(fmt.Sprintf("Heartbeat error looking for message to update heartbeat: %s", err))
				}
				for _, m := range msgResult.Messages {
					if *m.MessageId == msg.Id {
						return Qmsg{
							Id:     *m.MessageId,
							Handle: *m.ReceiptHandle,
							Body:   *m.Body,
						}, nil
					}
				}
			}
			return Qmsg{}, errors.New("Heartbeat error failed to find message to update heartbeat")
		} else {
			return Qmsg{}, errors.New(fmt.Sprintf("Heartbeat error updating queue duration: %s", err))
		}
	}
	return Qmsg{}, nil
}

// GetQueueDetails gets the number of in progress and available
// messages for a queue. These are returned as strings.
func (a *AwsConn) GetQueueDetails(url string) (string, string, error) {
	numAvailable := "ApproximateNumberOfMessages"
	numInProgress := "ApproximateNumberOfMessagesNotVisible"
	attrs, err := a.sqssvc.GetQueueAttributes(&sqs.GetQueueAttributesInput{
		AttributeNames: []*string{&numAvailable, &numInProgress},
		QueueUrl:       &url,
	})
	if err != nil {
		return "", "", errors.New(fmt.Sprintf("Failed to get queue attributes: %s", err))
	}
	return *attrs.Attributes[numAvailable], *attrs.Attributes[numInProgress], nil
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
