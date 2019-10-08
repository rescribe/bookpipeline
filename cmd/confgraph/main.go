package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"rescribe.xyz/bookpipeline"
	"rescribe.xyz/go.git/lib/hocr"
)

func walker(confs *[]*bookpipeline.Conf) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".hocr") {
			return nil
		}
		avg, err := hocr.GetAvgConf(path)
		if err != nil {
			return err
		}
		c := bookpipeline.Conf{
			Conf: avg,
			Path: path,
		}
		*confs = append(*confs, &c)
		return nil
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: bookpipeline hocrdir graph.png")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		return
	}

	var confs []*bookpipeline.Conf
	err := filepath.Walk(flag.Arg(0), walker(&confs))
	if err != nil {
		log.Fatalln("Failed to walk", flag.Arg(0), err)
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
	err = bookpipeline.Graph(cconfs, filepath.Base(flag.Arg(0)), f)
	if err != nil {
		log.Fatalln("Error creating graph", err)
	}
}
