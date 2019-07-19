package main

import (
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"
)

func main() {
	if len(os.Args) != 1 {
		log.Fatal("Usage: mkpipeline\n\nSets up necessary S3 buckets and SQS queues for our AWS pipeline\n")
	}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("eu-west-2"),
	})
	if err != nil {
		log.Fatalf("Error: failed to set up aws session: %v\n", err)
	}
	s3svc := s3.New(sess)
	sqssvc := sqs.New(sess)

	prefix := "rescribe"
	buckets := []string{"inprogress", "done"}
	queues := []string{"preprocess", "ocr", "analyse"}

	for _, bucket := range buckets {
		bname := prefix + bucket
		log.Printf("Creating bucket %s\n", bname)
		_, err = s3svc.CreateBucket(&s3.CreateBucketInput{
			Bucket: aws.String(bname),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if ok && (aerr.Code() == s3.ErrCodeBucketAlreadyExists || aerr.Code() == s3.ErrCodeBucketAlreadyOwnedByYou) {
				log.Printf("Bucket %s already exists\n", bname)
			} else {
				log.Fatalf("Error creating bucket %s: %v\n", bname, err)
			}
		}
	}

	for _, queue := range queues {
		qname := prefix + queue
		log.Printf("Creating queue %s\n", qname)
		_, err = sqssvc.CreateQueue(&sqs.CreateQueueInput{
			QueueName: aws.String(qname),
			Attributes: map[string]*string{
				"VisibilityTimeout": aws.String("120"), // 2 minutes
				"MessageRetentionPeriod": aws.String("1209600"), // 14 days; max allowed by sqs
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
				log.Fatalf("Error: Queue %s already exists but has different attributes\n", qname)
			} else {
				log.Fatalf("Error creating queue %s: %v\n", qname, err)
			}
		}
	}
}
