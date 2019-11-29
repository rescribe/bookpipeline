package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: pdfbook [-c] [-s] dir out.pdf

Creates a searchable PDF from a directory of hOCR and image files.

If a 'best' file exists in the directory, each hOCR listed in it is
used to provide the searchable text for each page. Otherwise pdfbook
just looks for a .hocr with the same file base as the image for the
searchable text.
`

type Pdfer interface {
	Setup() error
	AddPage(imgpath, hocrpath string, smaller bool) error
	Save(path string) error
}

const pageWidth = 5 // pageWidth in inches

// pxToPt converts a pixel value into a pt value (72 pts per inch)
// This uses pageWidth to determine the appropriate value
func pxToPt(i int) float64 {
	return float64(i) / pageWidth
}

// imgPath returns an appropriate path for the image that
// corresponds with the hocrpath
func imgPath(hocrpath string, colour bool) string {
	d := path.Dir(hocrpath)
	name := path.Base(hocrpath)
	nosuffix := strings.TrimSuffix(name, ".hocr")
	imgname := ""
	if colour {
		p := strings.SplitN(name, "_bin", 2)
		if len(p) > 1 {
			imgname = p[0] + ".jpg"
		} else {
			imgname = nosuffix + ".jpg"
		}
	} else {
		imgname = nosuffix + ".png"
	}
	return path.Join(d, imgname)
}

// addBest adds the pages in dir/best to a PDF
func addBest(dir string, pdf Pdfer, colour, smaller bool) error {
	f, err := os.Open(path.Join(dir, "best"))
	if err != nil {
		log.Fatalln("Failed to open best file", err)
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	var files []string
	for s.Scan() {
		fn := s.Text()
		if path.Ext(fn) != ".hocr" {
			continue
		}
		files = append(files, fn)
	}
	sort.Strings(files)

	for _, f := range files {
		hocrpath := path.Join(dir, f)
		img := imgPath(hocrpath, colour)
		err := pdf.AddPage(img, hocrpath, smaller)
		if err != nil {
			return errors.New(fmt.Sprintf("Failed to add page %s: %v", f, err))
		}
	}
	return nil
}

// walker walks each hocr file in a directory and adds a page to
// the pdf for each one.
func walker(pdf Pdfer, colour, smaller bool) filepath.WalkFunc {
	return func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if path.Ext(fpath) != ".hocr" {
			return nil
		}
		return pdf.AddPage(imgPath(fpath, colour), fpath, smaller)
	}
}

func main() {
	colour := flag.Bool("c", false, "colour")
	smaller := flag.Bool("s", false, "smaller")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		return
	}

	pdf := new(bookpipeline.Fpdf)
	err := pdf.Setup()
	if err != nil {
		log.Fatalln("Failed to set up PDF", err)
	}

	_, err = os.Stat(path.Join(flag.Arg(0), "best"))
	if err != nil && !os.IsNotExist(err) {
		log.Fatalln("Failed to stat best", err)
	}

	if os.IsNotExist(err) {
		err = filepath.Walk(flag.Arg(0), walker(pdf, *colour, *smaller))
		if err != nil {
			log.Fatalln("Failed to walk", flag.Arg(0), err)
		}
	} else {
		err = addBest(flag.Arg(0), pdf, *colour, *smaller)
		if err != nil {
			log.Fatalln("Failed to add best pages", err)
		}
	}

	err = pdf.Save(flag.Arg(1))
	if err != nil {
		log.Fatalln("Failed to save", flag.Arg(1), err)
	}
}
