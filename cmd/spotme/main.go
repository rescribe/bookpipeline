// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// spotme creates new spot instances for the book pipeline.
package main

import (
	"flag"
	"fmt"
	"log"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: spotme [-n num]

Create new spot instances for the book pipeline.
`

type SpotPipeliner interface {
	MinimalInit() error
	StartInstances(n int) error
}

func main() {
	num := flag.Int("n", 1, "number of instances to start")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	var conn SpotPipeliner
	conn = &bookpipeline.AwsConn{}
	err := conn.MinimalInit()
	if err != nil {
		log.Fatalln("Failed to set up cloud connection:", err)
	}

	log.Println("Starting spot instances")
	err = conn.StartInstances(*num)
	if err != nil {
		log.Fatalln("Failed to start a spot instance:", err)
	}
	log.Println("Spot instance request sent successfully")
}
