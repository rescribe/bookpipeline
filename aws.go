// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package bookpipeline

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const defaultAwsRegion = `eu-west-2`

type Qmsg struct {
	Id, Handle, Body string
}

type InstanceDetails struct {
	Id, Name, Ip, Spot, Type, State, LaunchTime string
}

type ObjMeta struct {
	Name string
	Date time.Time
}

// AwsConn contains the necessary things to interact with various AWS
// services in ways useful for the bookpipeline package. It is
// designed to be generic enough to swap in other backends easily.
type AwsConn struct {
	// these should be set before running Init(), or left to defaults
	Region string
	Logger *log.Logger

	sess                                      *session.Session
	ec2svc                                    *ec2.EC2
	s3svc                                     *s3.S3
	sqssvc                                    *sqs.SQS
	downloader                                *s3manager.Downloader
	uploader                                  *s3manager.Uploader
	wipequrl, prequrl, ocrpgqurl, analysequrl string
	wipstorageid                              string
}

// MinimalInit does the bare minimum to initialise aws services
func (a *AwsConn) MinimalInit() error {
	if a.Region == "" {
		a.Region = defaultAwsRegion
	}
	if a.Logger == nil {
		a.Logger = log.New(os.Stdout, "", 0)
	}

	var err error
	a.sess, err = session.NewSession(&aws.Config{
		Region: aws.String(a.Region),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to set up aws session: %s", err))
	}
	a.ec2svc = ec2.New(a.sess)
	a.s3svc = s3.New(a.sess)
	a.sqssvc = sqs.New(a.sess)
	a.downloader = s3manager.NewDownloader(a.sess)
	a.uploader = s3manager.NewUploader(a.sess)

	a.wipstorageid = storageWip

	return nil
}

// Init initialises aws services, also finding the urls needed to
// address SQS queues directly.
func (a *AwsConn) Init() error {
	err := a.MinimalInit()
	if err != nil {
		return err
	}

	a.Logger.Println("Getting preprocess queue URL")
	result, err := a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(queuePreProc),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting preprocess queue URL: %s", err))
	}
	a.prequrl = *result.QueueUrl

	a.Logger.Println("Getting wipeonly queue URL")
	result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(queueWipeOnly),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting wipeonly queue URL: %s", err))
	}
	a.wipequrl = *result.QueueUrl

	a.Logger.Println("Getting OCR Page queue URL")
	result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(queueOcrPage),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting OCR Page queue URL: %s", err))
	}
	a.ocrpgqurl = *result.QueueUrl

	a.Logger.Println("Getting analyse queue URL")
	result, err = a.sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(queueAnalyse),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting analyse queue URL: %s", err))
	}
	a.analysequrl = *result.QueueUrl

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

func (a *AwsConn) LogAndPurgeQueue(url string) error {
	for {
		msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
			MaxNumberOfMessages: aws.Int64(10),
			VisibilityTimeout:   aws.Int64(300),
			QueueUrl:            &url,
		})
		if err != nil {
			return err
		}

		if len(msgResult.Messages) > 0 {
			for _, m := range msgResult.Messages {
				a.Logger.Println(*m.Body)
				_, err = a.sqssvc.DeleteMessage(&sqs.DeleteMessageInput{
					QueueUrl:      &url,
					ReceiptHandle: m.ReceiptHandle,
				})
				if err != nil {
					return err
				}
			}
		} else {
			break
		}
	}
	return nil
}

// LogQueue prints the body of all messages in a queue to the log
func (a *AwsConn) LogQueue(url string) error {
	for {
		msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
			MaxNumberOfMessages: aws.Int64(10),
			VisibilityTimeout:   aws.Int64(300),
			QueueUrl:            &url,
		})
		if err != nil {
			return err
		}

		if len(msgResult.Messages) > 0 {
			for _, m := range msgResult.Messages {
				a.Logger.Println(*m.Body)
			}
		} else {
			break
		}
	}
	return nil
}

// RemovePrefixesFromQueue removes any messages in a queue whose
// body starts with the specified prefix.
func (a *AwsConn) RemovePrefixesFromQueue(url string, prefix string) error {
	for {
		msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
			MaxNumberOfMessages: aws.Int64(10),
			VisibilityTimeout:   aws.Int64(300),
			QueueUrl:            &url,
		})
		if err != nil {
			return err
		}

		if len(msgResult.Messages) > 0 {
			for _, m := range msgResult.Messages {
				if !strings.HasPrefix(*m.Body, prefix) {
					continue
				}
				a.Logger.Printf("Removing %s from queue\n", *m.Body)
				_, err = a.sqssvc.DeleteMessage(&sqs.DeleteMessageInput{
					QueueUrl:      &url,
					ReceiptHandle: m.ReceiptHandle,
				})
				if err != nil {
					return err
				}
			}
		} else {
			break
		}
	}
	return nil
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
			// First try to set the visibilitytimeout to zero to immediately
			// make the message available to receive
			_, _ = a.sqssvc.ChangeMessageVisibility(&sqs.ChangeMessageVisibilityInput{
				ReceiptHandle:     &msg.Handle,
				QueueUrl:          &qurl,
				VisibilityTimeout: aws.Int64(0),
			})

			for i := 0; i < int(duration)*5; i++ {
				msgResult, err := a.sqssvc.ReceiveMessage(&sqs.ReceiveMessageInput{
					MaxNumberOfMessages: aws.Int64(10),
					VisibilityTimeout:   &duration,
					WaitTimeSeconds:     aws.Int64(1),
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
				// Wait a second before trying again if the ReceiveMessage
				// call succeeded but didn't contain our message (otherwise
				// the WaitTimeSeconds will have applied and we will already
				// have waited a second)
				if len(msgResult.Messages) > 0 {
					time.Sleep(time.Second)
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

func (a *AwsConn) WipeQueueId() string {
	return a.wipequrl
}

func (a *AwsConn) OCRPageQueueId() string {
	return a.ocrpgqurl
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

func (a *AwsConn) ListObjectsWithMeta(bucket string, prefix string) ([]ObjMeta, error) {
	var objs []ObjMeta
	err := a.s3svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}, func(page *s3.ListObjectsV2Output, last bool) bool {
		for _, r := range page.Contents {
			objs = append(objs, ObjMeta{Name: *r.Key, Date: *r.LastModified})
		}
		return true
	})
	return objs, err
}

func (a *AwsConn) ListObjectPrefixes(bucket string) ([]string, error) {
	var prefixes []string
	err := a.s3svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Delimiter: aws.String("/"),
		MaxKeys:   aws.Int64(1),
	}, func(page *s3.ListObjectsV2Output, last bool) bool {
		for _, r := range page.CommonPrefixes {
			prefixes = append(prefixes, *r.Prefix)
		}
		return true
	})
	return prefixes, err
}

// Deletes a list of objects
func (a *AwsConn) DeleteObjects(bucket string, keys []string) error {
	objs := []*s3.ObjectIdentifier{}
	for _, v := range keys {
		o := s3.ObjectIdentifier{Key: aws.String(v)}
		objs = append(objs, &o)
	}
	_, err := a.s3svc.DeleteObjects(&s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &s3.Delete{
			Objects: objs,
			Quiet: aws.Bool(true),
		},
	})
	return err
}

// CreateBucket creates a new S3 bucket
func (a *AwsConn) CreateBucket(name string) error {
	_, err := a.s3svc.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(name),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if ok && (aerr.Code() == s3.ErrCodeBucketAlreadyExists || aerr.Code() == s3.ErrCodeBucketAlreadyOwnedByYou) {
			a.Logger.Println("Bucket already exists:", name)
		} else {
			return errors.New(fmt.Sprintf("Error creating bucket %s: %v", name, err))
		}
	}
	return nil
}

// CreateQueue creates a new SQS queue
// Note the queue attributes are currently hardcoded; it may make sense
// to specify them as arguments in the future.
func (a *AwsConn) CreateQueue(name string) error {
	_, err := a.sqssvc.CreateQueue(&sqs.CreateQueueInput{
		QueueName: aws.String(name),
		Attributes: map[string]*string{
			"VisibilityTimeout":             aws.String("120"),     // 2 minutes
			"MessageRetentionPeriod":        aws.String("1209600"), // 14 days; max allowed by sqs
			"ReceiveMessageWaitTimeSeconds": aws.String("20"),
		},
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		// Note the QueueAlreadyExists code is only emitted if an existing queue
		// has different attributes than the one that was being created. SQS just
		// quietly ignores the CreateQueue request if it is identical to an
		// existing queue.
		if ok && aerr.Code() == sqs.ErrCodeQueueNameExists {
			return errors.New("Error: Queue already exists but has different attributes:" + name)
		} else {
			return errors.New(fmt.Sprintf("Error creating queue %s: %v", name, err))
		}
	}
	return nil
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
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func (a *AwsConn) Upload(bucket string, key string, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
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

func instanceDetailsFromPage(page *ec2.DescribeInstancesOutput) []InstanceDetails {
	var details []InstanceDetails
	for _, r := range page.Reservations {
		for _, i := range r.Instances {
			var d InstanceDetails

			for _, t := range i.Tags {
				if *t.Key == "Name" {
					d.Name = *t.Value
				}
			}
			if i.PublicIpAddress != nil {
				d.Ip = *i.PublicIpAddress
			}
			if i.SpotInstanceRequestId != nil {
				d.Spot = *i.SpotInstanceRequestId
			}
			d.Type = *i.InstanceType
			d.Id = *i.InstanceId
			d.LaunchTime = i.LaunchTime.String()
			d.State = *i.State.Name

			details = append(details, d)
		}
	}

	return details
}

func (a *AwsConn) GetInstanceDetails() ([]InstanceDetails, error) {
	var details []InstanceDetails
	err := a.ec2svc.DescribeInstancesPages(&ec2.DescribeInstancesInput{}, func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
		for _, d := range instanceDetailsFromPage(page) {
			details = append(details, d)
		}
		return !lastPage
	})
	return details, err
}

func (a *AwsConn) StartInstances(n int) error {
	_, err := a.ec2svc.RequestSpotInstances(&ec2.RequestSpotInstancesInput{
		InstanceCount: aws.Int64(int64(n)),
		LaunchSpecification: &ec2.RequestSpotLaunchSpecification{
			IamInstanceProfile: &ec2.IamInstanceProfileSpecification{
				Arn: aws.String(spotProfile),
			},
			ImageId:      aws.String(spotImage),
			InstanceType: aws.String(spotType),
			SecurityGroupIds: []*string{
				aws.String(spotSg),
			},
		},
		Type: aws.String("one-time"),
	})
	return err
}

// Log records an item in the with the Logger. Arguments are handled
// as with fmt.Println.
func (a *AwsConn) Log(v ...interface{}) {
	a.Logger.Println(v...)
}

// mkpipeline sets up necessary buckets and queues for the pipeline
// TODO: also set up the necessary security group and iam stuff
func (a *AwsConn) MkPipeline() error {
	buckets := []string{storageWip}
	queues := []string{queuePreProc, queueWipeOnly, queueAnalyse, queueOcrPage}

	for _, bucket := range buckets {
		err := a.CreateBucket(bucket)
		if err != nil {
			return err
		}
	}

	for _, queue := range queues {
		err := a.CreateQueue(queue)
		if err != nil {
			return err
		}
	}

	return nil
}
