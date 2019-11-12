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
	Init() error
	StartInstances(n int) error
}

// NullWriter is used so non-verbose logging may be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func main() {
	num := flag.Int("n", 1, "number of instances to start")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	var verboselog *log.Logger
	var n NullWriter
	verboselog = log.New(n, "", 0)

	var conn SpotPipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}
	err := conn.Init()
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
