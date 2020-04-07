// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: getpipelinebook [-a] [-graph] [-pdf] [-png] [-v] bookname

Downloads the pipeline results for a book.

By default this downloads the best hOCR version for each page, the
binarised and (if available) colour PDF, and the best, conf and
graph.png analysis files.
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
	Upload(bucket string, key string, path string) error
	CheckQueue(url string, timeout int64) (bookpipeline.Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	WIPStorageId() string
}

func getpdfs(conn Pipeliner, l *log.Logger, bookname string) {
	for _, suffix := range []string{".colour.pdf", ".binarised.pdf"} {
		fn := filepath.Join(bookname, bookname+suffix)
		l.Println("Downloading PDF", fn)
		err := conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			log.Printf("Failed to download %s: %s\n", fn, err)
		}
	}
}

func main() {
	all := flag.Bool("a", false, "Get all files for book")
	graph := flag.Bool("graph", false, "Only download graphs (can be used alongside -pdf)")
	binarisedpdf := flag.Bool("binarisedpdf", false, "Only download binarised PDF (can be used alongside -graph)")
	colourpdf := flag.Bool("colourpdf", false, "Only download colour PDF (can be used alongside -graph)")
	pdf := flag.Bool("pdf", false, "Only download PDFs (can be used alongside -graph)")
	png := flag.Bool("png", false, "Only download best binarised png files")
	verbose := flag.Bool("v", false, "Verbose")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		return
	}

	var verboselog *log.Logger
	if *verbose {
		verboselog = log.New(os.Stdout, "", log.LstdFlags)
	} else {
		var n NullWriter
		verboselog = log.New(n, "", log.LstdFlags)
	}

	var conn Pipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}

	verboselog.Println("Setting up AWS session")
	err := conn.MinimalInit()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}
	verboselog.Println("Finished setting up AWS session")

	bookname := flag.Arg(0)

	err = os.MkdirAll(bookname, 0755)
	if err != nil {
		log.Fatalln("Failed to create directory", bookname, err)
	}

	if *all {
		verboselog.Println("Downloading all files for", bookname)
		objs, err := conn.ListObjects(conn.WIPStorageId(), bookname)
		if err != nil {
			log.Fatalln("Failed to get list of files for book", bookname, err)
		}
		for _, i := range objs {
			verboselog.Println("Downloading", i)
			err = conn.Download(conn.WIPStorageId(), i, i)
			if err != nil {
				log.Fatalln("Failed to download file", i, err)
			}
		}
		return
	}

	if *binarisedpdf {
		fn := filepath.Join(bookname, bookname+".binarised.pdf")
		verboselog.Println("Downloading file", fn)
		err = conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			log.Fatalln("Failed to download file", fn, err)
		}
	}

	if *colourpdf {
		fn := filepath.Join(bookname, bookname+".colour.pdf")
		verboselog.Println("Downloading file", fn)
		err = conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			log.Fatalln("Failed to download file", fn, err)
		}
	}

	if *graph {
		fn := filepath.Join(bookname, "graph.png")
		verboselog.Println("Downloading file", fn)
		err = conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			log.Fatalln("Failed to download file", fn, err)
		}
	}

	if *pdf {
		getpdfs(conn, verboselog, bookname)
	}

	if *binarisedpdf || *colourpdf || *graph || *pdf {
		return
	}

	verboselog.Println("Downloading best file")
	fn := filepath.Join(bookname, "best")
	err = conn.Download(conn.WIPStorageId(), fn, fn)
	if err != nil {
		log.Fatalln("Failed to download 'best' file", err)
	}
	f, err := os.Open(fn)
	if err != nil {
		log.Fatalln("Failed to open best file", err)
	}
	defer f.Close()

	if *png {
		verboselog.Println("Downloading png files")
		s := bufio.NewScanner(f)
		for s.Scan() {
			txtfn := filepath.Join(bookname, s.Text())
			fn = strings.Replace(txtfn, ".hocr", ".png", 1)
			verboselog.Println("Downloading file", fn)
			err = conn.Download(conn.WIPStorageId(), fn, fn)
			if err != nil {
				log.Fatalln("Failed to download file", fn, err)
			}
		}
		return
	}

	verboselog.Println("Downloading HOCR files")
	s := bufio.NewScanner(f)
	for s.Scan() {
		fn = filepath.Join(bookname, s.Text())
		verboselog.Println("Downloading file", fn)
		err = conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			log.Fatalln("Failed to download file", fn, err)
		}
	}

	verboselog.Println("Downloading PDF files")
	getpdfs(conn, verboselog, bookname)

	verboselog.Println("Downloading analysis files")
	for _, a := range []string{"conf", "graph.png"} {
		fn = filepath.Join(bookname, a)
		verboselog.Println("Downloading file", fn)
		err = conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			log.Fatalln("Failed to download file", fn, err)
		}
	}
}
