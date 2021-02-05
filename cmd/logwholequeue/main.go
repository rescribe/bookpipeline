// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// logwholequeue gets all messages in a queue. This can be useful
// for debugging queue issues.
package main

import (
	"flag"
	"fmt"
	"log"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: logwholequeue qname

logwholequeue gets all messages in a queue.

This can be useful for debugging queue issues.

Valid queue names:
- preprocess
- wipeonly
- ocrpage
- analyse
`

type QueuePipeliner interface {
	Init() error
	LogQueue(url string) error
	PreQueueId() string
	WipeQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
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

	err = conn.LogQueue(qid)
	if err != nil {
		log.Fatalln("Error getting queue", qname, ":", err)
	}
}
