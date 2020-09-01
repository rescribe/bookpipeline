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

	var conn Pipeliner
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

	qid := conn.PreQueueId()

	// Auto detect type of queue to send to based on file extension
	pngdirs, _ := filepath.Glob(bookdir + "/*.png")
	jpgdirs, _ := filepath.Glob(bookdir + "/*.jpg")
	pngcount := len(pngdirs)
	jpgcount := len(jpgdirs)
	if pngcount > jpgcount {
		qid = conn.WipeQueueId()
	} else {
		qid = conn.PreQueueId()
	}

	// Flags set override the queue selection
	if *wipeonly {
		qid = conn.WipeQueueId()
	}
	if *dobinarise {
		qid = conn.PreQueueId()
	}

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

	fmt.Println("Uploaded book to %s queue", qname)
}
