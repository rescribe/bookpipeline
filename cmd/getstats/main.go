// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// getstats gets relevant files for creating statistics from a set
// of OCRed books stored on cloud infrastructure
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: getstats

Downloads every 'conf' and 'best' file, and one hocr file, from a
set of OCRed books. This is useful for statistics.
`

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type Pipeliner interface {
	MinimalInit() error
	ListObjects(bucket string, prefix string) ([]string, error)
	Download(bucket string, key string, fn string) error
	WIPStorageId() string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	var n NullWriter
	verboselog := log.New(n, "", log.LstdFlags)

	var conn Pipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}

	err := conn.MinimalInit()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}

	log.Println("Getting list of all available objects to filter through")
	objs, err := conn.ListObjects(conn.WIPStorageId(), "")
	if err != nil {
		log.Fatalln("Failed to get list of files", err)
	}

	log.Println("Downloading all best and conf files found")
	for _, i := range objs {
		parts := strings.Split(i, "/")
		if parts[len(parts)-1] == "best" {
			fmt.Printf("Downloading %s to %s\n", i, parts[0]+"-best")
			err = conn.Download(conn.WIPStorageId(), i, parts[0]+"-best")
			if err != nil {
				log.Fatalln("Failed to download file", i, err)
			}
		}
		if parts[len(parts)-1] == "conf" {
			fmt.Printf("Downloading %s to %s\n", i, parts[0]+"-conf")
			err = conn.Download(conn.WIPStorageId(), i, parts[0]+"-conf")
			if err != nil {
				log.Fatalln("Failed to download file", i, err)
			}
		}
	}

	var bookpagesgot []string
	var found bool
	for _, i := range objs {
		parts := strings.Split(i, "/")
		if len(parts) < 2 {
			continue
		}

		// check if we already have a hocr page for this book
		found = false
		for _, name := range bookpagesgot {
			if name == parts[0] {
				found = true
				continue
			}
		}
		if found {
			continue
		}

		if strings.HasSuffix(parts[1], ".hocr") {
			fmt.Printf("Downloading %s to %s\n", i, parts[0]+"-hocr")
			err = conn.Download(conn.WIPStorageId(), i, parts[0]+"-hocr")
			if err != nil {
				log.Fatalln("Failed to download file", i, err)
			}
			bookpagesgot = append(bookpagesgot, parts[0])
		}
	}
}
