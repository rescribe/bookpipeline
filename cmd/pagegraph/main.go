// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// pagegraph creates a graph showing the average confidence of each
// word in a page of hOCR.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"rescribe.xyz/bookpipeline"
	"rescribe.xyz/utils/pkg/hocr"
)

const usage = `Usage: pagegraph [-l] file.hocr graph.png

pagegraph creates a graph showing average confidence of each
word in a page of hOCR.
`

func main() {
	lines := flag.Bool("l", false, "use line confidence instead of word confidence")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		return
	}

	var confs []*bookpipeline.Conf
	var xlabel string
	if *lines {
		linedetails, err := hocr.GetLineDetails(flag.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		for n, l := range linedetails {
			c := bookpipeline.Conf{
				Conf: l.Avgconf * 100,
				Path: fmt.Sprintf("%d_line", n),
			}
			confs = append(confs, &c)
		}
		xlabel = "Line number"
	} else {
		wordconfs, err := hocr.GetWordConfs(flag.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		for n, wc := range wordconfs {
			c := bookpipeline.Conf{
				Conf: wc,
				Path: fmt.Sprintf("%d_word", n),
			}
			confs = append(confs, &c)
		}
		xlabel = "Word number"
	}

	// Structure to fit what bookpipeline.Graph needs
	// TODO: probably reorganise bookpipeline to just need []*Conf
	cconfs := make(map[string]*bookpipeline.Conf)
	for _, c := range confs {
		cconfs[c.Path] = c
	}

	fn := flag.Arg(1)
	f, err := os.Create(fn)
	if err != nil {
		log.Fatalln("Error creating file", fn, err)
	}
	defer f.Close()
	err = bookpipeline.GraphOpts(cconfs, filepath.Base(flag.Arg(0)), xlabel, false, f)
	if err != nil {
		log.Fatalln("Error creating graph", err)
	}
}
