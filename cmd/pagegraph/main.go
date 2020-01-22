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

func main() {
	lines := flag.Bool("l", false, "use line confidence instead of word confidence")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: pagegraph [-l] file.hocr graph.png")
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
