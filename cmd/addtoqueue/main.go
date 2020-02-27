// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: addtoqueue qname msg

addtoqueue adds a message to a queue.

This is handy to work around bugs when things are misbehaving.

Valid queue names:
- preprocess
- wipeonly
- ocr
- ocrpage
- analyse
`

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type QueuePipeliner interface {
	Init() error
	AddToQueue(url string, msg string) error
	PreQueueId() string
	WipeQueueId() string
	OCRQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		return
	}

	var n NullWriter
	quietlog := log.New(n, "", 0)
	var conn QueuePipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: quietlog}

	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}

	qdetails := []struct {
		id, name string
	}{
		{conn.PreQueueId(), "preprocess"},
		{conn.WipeQueueId(), "wipeonly"},
		{conn.OCRQueueId(), "ocr"},
		{conn.OCRPageQueueId(), "ocrpage"},
		{conn.AnalyseQueueId(), "analyse"},
	}

	qname := flag.Arg(0)
	msg := flag.Arg(1)

	var qid string
	for i, n := range qdetails {
		if n.name == qname {
			qid = qdetails[i].id
			break
		}
	}
	if qid == "" {
		log.Fatalln("Error, no queue named", qname)
	}

	err = conn.AddToQueue(qid, msg)
	if err != nil {
		log.Fatalln("Error adding message to", qname, "queue:", err)
	}
	fmt.Println("Added message to the queue.")
}
