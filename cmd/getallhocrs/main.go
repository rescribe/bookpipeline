// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// getallhocrs downloads every 'best' file from a set of OCRed books
// stored on cloud infrastructure
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: getallhocrs

Downloads every 'hocr' file.
`

type Pipeliner interface {
	Init() error
	Download(bucket string, key string, fn string) error
	ListObjects(bucket string, prefix string) ([]string, error)
	ListObjectPrefixes(bucket string) ([]string, error)
	Log(v ...interface{})
	WIPStorageId() string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	verboselog := log.New(os.Stdout, "", log.LstdFlags)

	var conn Pipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}

	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}

	conn.Log("Getting list of all books")
	prefixes, err := conn.ListObjectPrefixes(conn.WIPStorageId())
	if err != nil {
		log.Fatalln("Failed to get list of prefixes", err)
	}

	for _, p := range prefixes {
		conn.Log("Getting list of files for book", p)
		objs, err := conn.ListObjects(conn.WIPStorageId(), p)
		if err != nil {
			log.Fatalln("Failed to get list of files", err)
		}
		err = os.MkdirAll(p, 0755)
		if err != nil {
			log.Fatalln("Failed to make directory", err)
		}
		conn.Log("Downloading hocrs from book", p)
		for _, o := range objs {
			if !strings.HasSuffix(o, ".hocr") {
				continue
			}
			// skip already downloaded items
			_, err = os.Stat(o)
			if err == nil || os.IsExist(err) {
				log.Println("  Skipping already complete download of", o)
				continue
			}
			log.Println("  Downloading", o)
			err = conn.Download(conn.WIPStorageId(), o, o)
			if err != nil {
				log.Fatalln("Failed to download file", o, err)
			}
		}
	}
}
