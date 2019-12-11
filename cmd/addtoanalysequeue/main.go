package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: addtoanalysequeue [-v] bookname

addtoanalysequeue adds a book it to the Analyse queue.

This should be done automatically by the bookpipeline tool once
the OCR job has completed, but sometimes it isn't, because of a
bug where if a file that is named like a preprocessed image
doesn't have a hOCR component. Once that bug is squashed, this
tool can be deleted.
`

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type UnstickPipeliner interface {
	Init() error
	AddToQueue(url string, msg string) error
	AnalyseQueueId() string
}

func main() {
	verbose := flag.Bool("v", false, "verbose")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		return
	}

	var verboselog *log.Logger
	if *verbose {
		verboselog = log.New(os.Stdout, "", 0)
	} else {
		var n NullWriter
		verboselog = log.New(n, "", 0)
	}

	var conn UnstickPipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}

	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}

	book := flag.Arg(0)

	err = conn.AddToQueue(conn.AnalyseQueueId(), book)
	if err != nil {
		log.Fatalln("Error adding message to Analyse queue:", err)
	}
	fmt.Println("Added message from to the Analyse queue.")
}
