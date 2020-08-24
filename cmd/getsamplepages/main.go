// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// getsamplepages downloads sample pages from each book in a
// set of OCRed books
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: getsamplepages

Downloads a sample page hocr and image from each book in a set
of OCRed books. These can then be used for various testing,
statistics, and so on.
`

const pgnum = "0100"

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type Pipeliner interface {
	Init() error
	ListObjectPrefixes(bucket string) ([]string, error)
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

	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}

	log.Println("Getting list of all books")
	prefixes, err := conn.ListObjectPrefixes(conn.WIPStorageId())
	if err != nil {
		log.Fatalln("Failed to get list of books", err)
	}

	for _, p := range prefixes {
		name := strings.Split(p, "/")[0]
		log.Printf("Downloading a page from %s\n", name)

		fn := pgnum + ".jpg"
		err = conn.Download(conn.WIPStorageId(), p+fn, name+fn)
		if err != nil && strings.HasPrefix(err.Error(), "NoSuchKey:") {
			log.Printf("Skipping %s as no page %s found\n", p, pgnum)
			continue
		} else if err != nil {
			log.Fatalf("Download of %s%s failed: %v\n", p+fn, err)
		}
	}
}
