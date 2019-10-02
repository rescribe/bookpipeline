package main

// TODO: use bookpipeline package to do aws stuff

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"
)

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

var verboselog *log.Logger

type fileWalk chan string

func (f fileWalk) Walk(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if !info.IsDir() {
		f <- path
	}
	return nil
}

func main() {
	verbose := flag.Bool("v", false, "Verbose")
	wipeonly := flag.Bool("prebinarised", false, "Prebinarised: only preprocessing will be to wipe")
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatal("Usage: booktopipeline [-v] bookdir [bookname]\n\nUploads the book in bookdir to the S3 'inprogress' bucket and adds it to the 'preprocess' SQS queue\nIf bookname is omitted the last part of the bookdir is used\n")
	}

	bookdir := flag.Arg(0)
	var bookname string
	if flag.NArg() > 2 {
		bookname = flag.Arg(1)
	} else {
		bookname = filepath.Base(bookdir)
	}

	if *verbose {
		verboselog = log.New(os.Stdout, "", log.LstdFlags)
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
	sqssvc := sqs.New(sess)
	uploader := s3manager.NewUploader(sess)

	var qname string
	if *wipeonly {
		qname = "rescribewipeonly"
	} else {
		qname = "rescribepreprocess"
	}
	verboselog.Println("Getting Queue URL for", qname)
	result, err := sqssvc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(qname),
	})
	if err != nil {
		log.Fatalln("Error getting queue URL for", qname, ":", err)
	}
	qurl := *result.QueueUrl

	// concurrent walking upload based on example at
	// https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/sdk-utilities.html
	verboselog.Println("Walking", bookdir)
	walker := make(fileWalk)
	go func() {
		err = filepath.Walk(bookdir, walker.Walk)
		if err != nil {
			log.Fatalln("Filesystem walk failed:", err)
		}
		close(walker)
	}()

	for path := range walker {
		verboselog.Println("Uploading", path)
		name := filepath.Base(path)
		file, err := os.Open(path)
		if err != nil {
			log.Fatalln("Open file", path, "failed:", err)
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

	verboselog.Println("Sending message", bookname, "to queue", qurl)
	_, err = sqssvc.SendMessage(&sqs.SendMessageInput{
		MessageBody: aws.String(bookname),
		QueueUrl:    &qurl,
	})
	if err != nil {
		log.Fatalln("Error adding book to queue:", err)
	}
}
