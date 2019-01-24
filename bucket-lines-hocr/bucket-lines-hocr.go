package main

// TODO: merge with -prob, using filename extension to determine what to do for each file

import (
	"flag"
	"fmt"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"git.rescribe.xyz/testingtools/parse"
	"git.rescribe.xyz/testingtools/parse/hocr"
)

func detailsFromFile(f string) (parse.LineDetails, error) {
	var newlines parse.LineDetails

	file, err := ioutil.ReadFile(f)
	if err != nil {
		return newlines, err
	}

	h, err := hocr.Parse(file)
	if err != nil {
		return newlines, err
	}

	pngfn := strings.Replace(f, ".hocr", ".png", 1)
	pngf, err := os.Open(pngfn)
	if err != nil {
		return newlines, err
	}
	defer pngf.Close()
	img, err := png.Decode(pngf)
	if err != nil {
		return newlines, err
	}

	n := strings.Replace(filepath.Base(f), ".hocr", "", 1)
	return hocr.GetLineDetails(h, img, n)
}

func main() {
	b := parse.BucketSpecs{
		// minimum confidence, name
		{ 0, "bad" },
		{ 0.95, "95to98" },
		{ 0.98, "98plus" },
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: bucket-lines-hocr [-d dir] hocr1 [hocr2] [...]\n")
		fmt.Fprintf(os.Stderr, "Copies image-text line pairs into different directories according\n")
		fmt.Fprintf(os.Stderr, "to the average character probability for the line.\n")
		fmt.Fprintf(os.Stderr, "This uses the x_wconf data in .hocr files, which it assumes will be.\n")
		fmt.Fprintf(os.Stderr, "in the same directory as the line's image and text files. It can\n")
		fmt.Fprintf(os.Stderr, "handle hocr where each character is tagged separately and hocr where\n")
		fmt.Fprintf(os.Stderr, "only whole words are tagged.\n")
		flag.PrintDefaults()
	}
	dir := flag.String("d", "buckets", "Directory to store the buckets")
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	lines := make(parse.LineDetails, 0)

	for _, f := range flag.Args() {
		newlines, err := detailsFromFile(f)
		if err != nil {
			log.Fatal(err)
		}

		for _, l := range newlines {
			lines = append(lines, l)
		}
	}

	stats, err := parse.BucketUp(lines, b, *dir)
	if err != nil {
		log.Fatal(err)
	}

	parse.PrintBucketStats(os.Stdout, stats)
}
