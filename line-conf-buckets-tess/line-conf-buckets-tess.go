package main

// TODO: see TODO in hocr package
//
// TODO: Simplify things into functions more; this works well, but is a bit of a rush job

import (
	"flag"
	"fmt"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"git.rescribe.xyz/testingtools/parse"
	"git.rescribe.xyz/testingtools/parse/hocr"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: line-conf-buckets hocr1 [hocr2] [...]\n")
		fmt.Fprintf(os.Stderr, "Copies image-text line pairs into different directories according\n")
		fmt.Fprintf(os.Stderr, "to the average character probability for the line.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	lines := make(parse.LineDetails, 0)

	for _, f := range flag.Args() {
		file, err := ioutil.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}

		h, err := hocr.Parse(file)
		if err != nil {
			log.Fatal(err)
		}

		pngfn := strings.Replace(f, ".hocr", ".png", 1)
		pngf, err := os.Open(pngfn)
		if err != nil {
			log.Fatal(err)
		}
		defer pngf.Close()
		img, err := png.Decode(pngf)
		if err != nil {
			log.Fatal(err)
		}

		n := strings.Replace(filepath.Base(f), ".hocr", "", 1)
		newlines, err := hocr.GetLineDetails(h, img, n)
		if err != nil {
			log.Fatal(err)
		}
		for _, l := range newlines {
			lines = append(lines, l)
		}
	}

	sort.Sort(lines)

	worstnum := 0
	mediumnum := 0
	bestnum := 0

	outdir := "buckets" // TODO: set this from cmdline
	todir := ""

	for _, l := range lines {
		switch {
		case l.Avgconf < 0.95:
			todir = "bad"
			worstnum++
		case l.Avgconf < 0.98:
			todir = "95to98"
			mediumnum++
		default:
			todir = "98plus"
			bestnum++
		}

		avgstr := strconv.FormatFloat(l.Avgconf, 'f', 5, 64)
		avgstr = avgstr[2:]
		outname := filepath.Join(outdir, todir, l.OcrName + "_" + l.Name + "_" + avgstr + ".png")

		err := os.MkdirAll(filepath.Join(outdir, todir), 0700)
		if err != nil {
			log.Fatal(err)
		}

		outfile, err := os.Create(outname)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s\n", outname)
			log.Fatal(err)
		}
		defer outfile.Close()

		err = l.Img.CopyLineTo(outfile)
		if err != nil {
			log.Fatal(err)
		}

		outname = filepath.Join(outdir, todir, l.OcrName + "_" + l.Name + "_" + avgstr + ".txt")
		outfile, err = os.Create(outname)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s\n", outname)
			log.Fatal(err)
		}
		defer outfile.Close()

		_, err = io.WriteString(outfile, l.Text)
		if err != nil {
			log.Fatal(err)
		}

		// TODO: test whether the line.img works properly with multiple hocrs, as it could be that as it's a pointer, it always points to the latest image (don't think so, but not sure)
	}

	total := worstnum + mediumnum + bestnum

	fmt.Printf("Copied lines to %s\n", outdir)
	fmt.Printf("---------------------------------\n")
	fmt.Printf("Lines 98%%+ quality:     %d%%\n", 100 * bestnum / total)
	fmt.Printf("Lines 95-98%% quality:   %d%%\n", 100 * mediumnum / total)
	fmt.Printf("Lines <95%% quality:     %d%%\n", 100 * worstnum / total)
}
