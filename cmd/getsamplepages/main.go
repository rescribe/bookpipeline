// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// getsamplepages downloads sample pages from each book in a
// set of OCRed books
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: getsamplepages [-prefix prefix]

Downloads a sample page hocr and image from each book in a set
of OCRed books. These can then be used for various testing,
statistics, and so on.
`

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
	prefix := flag.String("prefix", "", "Only select books with this prefix (e.g. '17' for 18th century books)")
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

	fmt.Println("Getting list of all books")
	prefixes, err := conn.ListObjectPrefixes(conn.WIPStorageId())
	if err != nil {
		log.Fatalln("Failed to get list of books", err)
	}

	for _, p := range prefixes {
		if *prefix != "" && !strings.HasPrefix(p, *prefix) {
			continue
		}

		name := strings.Split(p, "/")[0]

		err = conn.Download(conn.WIPStorageId(), p+"best", name+"best")
		if err != nil {
		}
		b, err := ioutil.ReadFile(name + "best")
		if err != nil {
			log.Fatalf("Failed to read file %s\n", name+"best")
		}
		lines := strings.SplitN(string(b), "\n", 2)
		if len(lines) == 1 {
			fmt.Printf("No pages found for %s, skipping\n", name)
			continue
		}
		pg := strings.TrimSuffix(lines[0], ".hocr")

		err = os.Remove(name + "best")
		if err != nil {
			log.Fatalf("Failed to remove temporary best file for %s", name)
		}

		fmt.Printf("Downloading page %s from %s\n", pg, name)

		for _, suffix := range []string{".png", ".hocr"} {
			fn := pg + suffix
			err = conn.Download(conn.WIPStorageId(), p+fn, name+fn)
			if err != nil {
				log.Fatalf("Download of %s%s failed: %v\n", p+fn, err)
			}
		}
	}
}
