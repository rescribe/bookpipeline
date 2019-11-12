package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: unstickocr [-v] bookname

unstickocr deletes a book from the OCR queue and adds it to the
Analyse queue.

This should be done automatically by the bookpipeline tool once
the OCR job has completed, but sometimes it isn't, because of a
nasty bug. Once that bug is squashed, this tool can be deleted.
`

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type UnstickPipeliner interface {
	Init() error
	CheckQueue(url string, timeout int64) (bookpipeline.Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	OCRQueueId() string
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
	done := false

	for a := 0; a < 5; a++ {
		for i := 0; i < 10; i++ {
			verboselog.Println("Checking OCR queue for", book)
			msg, err := conn.CheckQueue(conn.OCRQueueId(), 10)
			if err != nil {
				log.Fatalln("Error checking OCR queue:", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on OCR queue")
				continue
			}
			if msg.Body != book {
				verboselog.Println("Message received on OCR queue is not the one we're",
					"looking for, so will try again - found", msg.Body)
				continue
			}
			err = conn.DelFromQueue(conn.OCRQueueId(), msg.Handle)
			if err != nil {
				log.Fatalln("Error deleting message from OCR queue:", err)
			}
			err = conn.AddToQueue(conn.AnalyseQueueId(), book)
			if err != nil {
				log.Fatalln("Error adding message to Analyse queue:", err)
			}
			done = true
			break
		}
		if done == true {
			break
		}
		log.Println("No message found yet, sleeping for 30 seconds to try again")
		time.Sleep(30 * time.Minute)
	}

	if done == true {
		fmt.Println("Succeeded moving message from OCR queue to Analyse queue.")
	} else {
		log.Fatalln("Failed to find message", book, "on OCR queue; is it still being processed?",
			"It can only be discovered and processed by this tool when it is available.",
			"Try shutting down any instance that is using it, waiting a few minutes,",
			"and rerunning this tool.")
	}
}
