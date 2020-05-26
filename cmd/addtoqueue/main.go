// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// addtoqueue adds a message to a queue. This is handy to work
// around bugs in the book pipeline when things are misbehaving.
package main

import (
	"flag"
	"fmt"
	"log"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: addtoqueue [-c conn] qname msg

addtoqueue adds a message to a queue.

This is handy to work around bugs when things are misbehaving.

Valid queue names:
- preprocess
- wipeonly
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
	OCRPageQueueId() string
	AnalyseQueueId() string
}

func main() {
	conntype := flag.String("c", "aws", "connection type ('aws' or 'local')")
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

	switch *conntype {
	case "aws":
		conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: quietlog}
	case "local":
		conn = &bookpipeline.LocalConn{Logger: quietlog}
	default:
		log.Fatalln("Unknown connection type")
	}

	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}

	qdetails := []struct {
		id, name string
	}{
		{conn.PreQueueId(), "preprocess"},
		{conn.WipeQueueId(), "wipeonly"},
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
