// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// booktopipeline uploads a book to cloud storage and adds the name
// to a queue ready to be processed by the bookpipeline tool.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"rescribe.xyz/bookpipeline"

	"rescribe.xyz/bookpipeline/internal/pipeline"
)

const usage = `Usage: booktopipeline [-c conn] [-t training] [-prebinarised] [-notbinarised] [-v] bookdir [bookname]

Uploads the book in bookdir to the S3 'inprogress' bucket and adds it
to the 'preprocess' or 'wipeonly' SQS queue. The queue to send to is
autodetected based on the number of .jpg and .png files; more .jpg
than .png means it will be presumed to be not binarised, and it will
go to the 'preprocess' queue. The queue can be manually selected by
using the flags -prebinarised (for the wipeonly queue) or
-notbinarised (for the preprocess queue).

If bookname is omitted the last part of the bookdir is used.
`

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

var verboselog *log.Logger

func main() {
	verbose := flag.Bool("v", false, "Verbose")
	conntype := flag.String("c", "aws", "connection type ('aws' or 'local')")
	wipeonly := flag.Bool("prebinarised", false, "Prebinarised: only preprocessing will be to wipe")
	dobinarise := flag.Bool("notbinarised", false, "Not binarised: all preprocessing will be done including binarisation")
	training := flag.String("t", "", "Training to use (training filename without the .traineddata part)")

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

	var conn pipeline.Pipeliner
	switch *conntype {
	case "aws":
		conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}
	case "local":
		conn = &bookpipeline.LocalConn{Logger: verboselog}
	default:
		log.Fatalln("Unknown connection type")
	}
	err := conn.Init()
	if err != nil {
		log.Fatalln("Failed to set up cloud connection:", err)
	}

	qid := pipeline.DetectQueueType(bookdir, conn)

	// Flags set override the queue selection
	if *wipeonly {
		qid = conn.WipeQueueId()
	}
	if *dobinarise {
		qid = conn.PreQueueId()
	}

	verboselog.Println("Checking that all images are valid in", bookdir)
	err = pipeline.CheckImages(bookdir)
	if err != nil {
		log.Fatalln(err)
	}

	verboselog.Println("Checking that a book hasn't already been uploaded with that name")
	list, err := conn.ListObjects(conn.WIPStorageId(), bookname)
	if err != nil {
		log.Fatalln(err)
	}
	if len(list) > 0 {
		log.Fatalf("Error: There is already a book in S3 named %s", bookname)
	}

	verboselog.Println("Uploading all images are valid in", bookdir)
	err = pipeline.UploadImages(bookdir, bookname, conn)
	if err != nil {
		log.Fatalln(err)
	}

	if *training != "" {
		bookname = bookname + " " + *training
	}
	err = conn.AddToQueue(qid, bookname)
	if err != nil {
		log.Fatalln("Error adding book to queue:", err)
	}

	var qname string
	if qid == conn.PreQueueId() {
		qname = "preprocess"
	} else {
		qname = "wipeonly"
	}

	fmt.Println("Uploaded book to queue", qname)
}
