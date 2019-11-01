package main

import (
	"bufio"
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

type Pdfer interface {
	Setup() error
	AddPage(imgpath, hocrpath string) error
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
func addBest(dir string, pdf Pdfer, colour bool) error {
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
		err := pdf.AddPage(img, hocrpath)
		if err != nil {
			return err
		}
	}
	return nil
}

// walker walks each hocr file in a directory and adds a page to
// the pdf for each one.
func walker(pdf Pdfer, colour bool) filepath.WalkFunc {
	return func(fpath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if path.Ext(fpath) != ".hocr" {
			return nil
		}
		return pdf.AddPage(imgPath(fpath, colour), fpath)
	}
}

func main() {
	// TODO: probably take flags to resize / change quality in due course
	colour := flag.Bool("c", false, "colour")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: pdfbook [-c] hocrdir out.pdf")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		return
	}

	_, err := os.Stat(path.Join(flag.Arg(0), "best"))
	if err != nil && !os.IsNotExist(err) {
		log.Fatalln("Failed to stat best", err)
	}

	pdf := new(bookpipeline.Fpdf)
	err = pdf.Setup()
	if err != nil {
		log.Fatalln("Failed to set up PDF", err)
	}

	if os.IsNotExist(err) {
		err = filepath.Walk(flag.Arg(0), walker(pdf, *colour))
		if err != nil {
			log.Fatalln("Failed to walk", flag.Arg(0), err)
		}
	} else {
		err = addBest(flag.Arg(0), pdf, *colour)
		if err != nil {
			log.Fatalln("Failed to add best pages", err)
		}
	}

	err = pdf.Save(flag.Arg(1))
	if err != nil {
		log.Fatalln("Failed to save", flag.Arg(1), err)
	}
}
