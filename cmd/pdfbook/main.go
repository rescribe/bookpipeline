package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
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

func walker(pdf Pdfer) filepath.WalkFunc {
	return func(fpath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(fpath, ".hocr") {
			return nil
		}
		// TODO: handle jpg or binarised versions according to a flag
		imgpath := ""
		p := strings.SplitN(path.Base(fpath), "_bin", 2)
		if len(p) > 1 {
			imgpath = path.Join(path.Dir(fpath), p[0] + ".jpg")
		} else {
			imgpath = strings.TrimSuffix(fpath, ".hocr") + ".png"
		}
		return pdf.AddPage(imgpath, fpath)
	}
}

func main() {
	// TODO: handle best
	// TODO: take flags to do colour or binarised
	// TODO: probably also take flags to resize / change quality in due course
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: pdfbook hocrdir out.pdf")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		return
	}

	pdf := new(bookpipeline.Fpdf)
	pdf.Setup()

	err := filepath.Walk(flag.Arg(0), walker(pdf))
	if err != nil {
		log.Fatalln("Failed to walk", flag.Arg(0), err)
	}

	err = pdf.Save(flag.Arg(1))
	if err != nil {
		log.Fatalln("Failed to save", flag.Arg(1), err)
	}
}
