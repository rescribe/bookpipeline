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

func walker(pdf Pdfer, colour bool) filepath.WalkFunc {
	return func(fpath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(fpath, ".hocr") {
			return nil
		}
		imgpath := ""
		if colour {
			p := strings.SplitN(path.Base(fpath), "_bin", 2)
			if len(p) > 1 {
				imgpath = path.Join(path.Dir(fpath), p[0] + ".jpg")
			} else {
				imgpath = strings.TrimSuffix(fpath, ".hocr") + ".jpg"
			}
		} else {
			imgpath = strings.TrimSuffix(fpath, ".hocr") + ".png"
		}
		return pdf.AddPage(imgpath, fpath)
	}
}

func main() {
	// TODO: handle best
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

	pdf := new(bookpipeline.Fpdf)
	pdf.Setup()

	err := filepath.Walk(flag.Arg(0), walker(pdf, *colour))
	if err != nil {
		log.Fatalln("Failed to walk", flag.Arg(0), err)
	}

	err = pdf.Save(flag.Arg(1))
	if err != nil {
		log.Fatalln("Failed to save", flag.Arg(1), err)
	}
}
