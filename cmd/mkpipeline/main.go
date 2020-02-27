// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

// TODO: set up iam role and policy needed for ec2 instances to access this stuff;
//       see arn:aws:iam::557852942063:policy/pipelinestorageandqueue
//       and arn:aws:iam::557852942063:role/pipeliner
// TODO: set up launch template for ec2 instances

import (
	"log"
	"os"

	"rescribe.xyz/bookpipeline"
)

type MkPipeliner interface {
	MinimalInit() error
	CreateBucket(string) error
	CreateQueue(string) error
}

func main() {
	if len(os.Args) != 1 {
		log.Fatal("Usage: mkpipeline\n\nSets up necessary buckets and queues for our cloud pipeline\n")
	}

	var conn MkPipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: log.New(os.Stdout, "", 0)}
	err := conn.MinimalInit()
	if err != nil {
		log.Fatalln("Failed to set up cloud connection:", err)
	}

	prefix := "rescribe"
	buckets := []string{"inprogress", "done"}
	queues := []string{"preprocess", "wipeonly", "ocr", "analyse", "ocrpage"}

	for _, bucket := range buckets {
		bname := prefix + bucket
		log.Printf("Creating bucket %s\n", bname)
		err = conn.CreateBucket(bname)
		if err != nil {
			log.Fatalln(err)
		}
	}

	for _, queue := range queues {
		qname := prefix + queue
		log.Printf("Creating queue %s\n", qname)
		err = conn.CreateQueue(qname)
		if err != nil {
			log.Fatalln(err)
		}
	}
}
