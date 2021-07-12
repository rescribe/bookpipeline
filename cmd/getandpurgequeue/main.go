// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// getandpurgequeue gets and deletes all messages from a queue. This can
// be useful for debugging queue issues.
package main

import (
	"flag"
	"fmt"
	"log"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: getandpurgequeue qname

getandpurgequeue gets and deletes all messages from a queue.

This can be useful for debugging queue issues.

Valid queue names:
- preprocess
- wipeonly
- ocrpage
- analyse
- test
`

type QueuePipeliner interface {
	Init() error
	LogAndPurgeQueue(url string) error
	PreQueueId() string
	WipeQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
	TestQueueId() string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		return
	}

	var conn QueuePipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2"}

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
		{conn.TestQueueId(), "test"},
	}

	qname := flag.Arg(0)

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

	err = conn.LogAndPurgeQueue(qid)
	if err != nil {
		log.Fatalln("Error getting and purging queue", qname, ":", err)
	}
}
