// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// trimqueue deletes any messages in a queue that match a specified
// prefix.
package main

import (
	"flag"
	"fmt"
	"log"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: trimprefix qname prefix

trimqueue deletes any messages in a queue that match a specified
prefix.

Valid queue names:
- preprocess
- wipeonly
- ocrpage
- analyse
`

type QueuePipeliner interface {
	Init() error
	RemovePrefixesFromQueue(url string, prefix string) error
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

	if flag.NArg() != 2 {
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

	err = conn.RemovePrefixesFromQueue(qid, flag.Arg(1))
	if err != nil {
		log.Fatalln("Error removing prefixes from queue", qname, ":", err)
	}
}
