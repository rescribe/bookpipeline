// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// getpipelinebook downloads the pipeline results for a book.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"rescribe.xyz/bookpipeline"

	"rescribe.xyz/bookpipeline/internal/pipeline"
)

const usage = `Usage: getpipelinebook [-c conn] [-a] [-graph] [-pdf] [-png] [-v] bookname

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

func main() {
	all := flag.Bool("a", false, "Get all files for book")
	conntype := flag.String("c", "aws", "connection type ('aws' or 'local')")
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

	var conn pipeline.MinPipeliner
	switch *conntype {
	case "aws":
		conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}
	case "local":
		conn = &bookpipeline.LocalConn{Logger: verboselog}
	default:
		log.Fatalln("Unknown connection type")
	}

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
		err = pipeline.DownloadAll(bookname, bookname, conn)
		if err != nil {
			log.Fatalln(err)
		}
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
		verboselog.Println("Downloading PDFs")
		pipeline.DownloadPdfs(bookname, bookname, conn)
	}

	if *binarisedpdf || *colourpdf || *graph || *pdf {
		return
	}

	verboselog.Println("Downloading best pages")
	err = pipeline.DownloadBestPages(bookname, bookname, conn, *png)
	if err != nil {
		log.Fatalln(err)
	}

	verboselog.Println("Downloading PDFs")
	pipeline.DownloadPdfs(bookname, bookname, conn)
	if err != nil {
		log.Fatalln(err)
	}

	verboselog.Println("Downloading analyses")
	err = pipeline.DownloadAnalyses(bookname, bookname, conn)
	if err != nil {
		log.Fatalln(err)
	}
}
