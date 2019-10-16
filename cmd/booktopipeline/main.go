package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: booktopipeline [-prebinarised] [-v] bookdir [bookname]

Uploads the book in bookdir to the S3 'inprogress' bucket and adds it
to the 'preprocess' SQS queue, or the 'wipeonly' queue if the
prebinarised flag is set.

If bookname is omitted the last part of the bookdir is used.
`

type Pipeliner interface {
	Init() error
	PreQueueId() string
	WipeQueueId() string
	WIPStorageId() string
	AddToQueue(url string, msg string) error
	Upload(bucket string, key string, path string) error
}

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

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 || flag.NArg() > 3 {
		flag.Usage()
		return
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

	var conn Pipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}
	err := conn.Init()
	if err != nil {
		log.Fatalln("Failed to set up cloud connection:", err)
	}

	var qid string
	if *wipeonly {
		qid = conn.WipeQueueId()
	} else {
		qid = conn.PreQueueId()
	}

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
		err = conn.Upload(conn.WIPStorageId(), filepath.Join(bookname, name), path)
		if err != nil {
			log.Fatalln("Failed to upload", path, err)
		}
	}

	err = conn.AddToQueue(qid, bookname)
	if err != nil {
		log.Fatalln("Error adding book to queue:", err)
	}
}
