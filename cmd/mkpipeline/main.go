// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// mkpipeline sets up the necessary buckets and queues for the book
// pipeline.
package main

import (
	"log"
	"os"

	"rescribe.xyz/bookpipeline"
)

type MkPipeliner interface {
	MinimalInit() error
	MkPipeline() error
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

	err = conn.MkPipeline()
	if err != nil {
		log.Fatalln("MkPipeline failed:", err)
	}
}
