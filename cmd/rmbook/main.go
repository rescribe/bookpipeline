// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// rmbook removes a book from cloud storage.
package main

import (
	"flag"
	"fmt"
	"log"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: rmbook bookname

Removes a book from cloud storage.
`

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type RmPipeliner interface {
        MinimalInit() error
        WIPStorageId() string
	DeleteObjects(bucket string, keys []string) error
	ListObjects(bucket string, prefix string) ([]string, error)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		return
	}

	var n NullWriter
	verboselog := log.New(n, "", log.LstdFlags)

	var conn RmPipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}

	fmt.Println("Setting up cloud connection")
	err := conn.MinimalInit()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}

	bookname := flag.Arg(0)

	fmt.Println("Getting list of files for book")
	objs, err := conn.ListObjects(conn.WIPStorageId(), bookname)
	if err != nil {
		log.Fatalln("Error in listing book items:", err)
	}

	if len(objs) == 0 {
		log.Fatalln("No files found for book:", bookname)
	}

	fmt.Println("Deleting all files for book")
	err = conn.DeleteObjects(conn.WIPStorageId(), objs)
	if err != nil {
		log.Fatalln("Error deleting book files:", err)
	}

	fmt.Println("Finished deleting files")
}
