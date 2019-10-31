package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"rescribe.xyz/gofpdf"
	"rescribe.xyz/utils/pkg/hocr"
)

// see notebook for rationale; experimental
func pxToPt(i int) float64 {
	return float64(i) / 5
}

func lineText(l hocr.OcrLine) string {
	// TODO: handle cases of OcrLine being where the text is, and OcrChar being where the text is
	var t string
	for _, w := range l.Words {
		if len(t) > 0 {
			t += " "
		}
		t += w.Text
	}
	return t
}

func walker(pdf *gofpdf.Fpdf) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".hocr") {
			return nil
		}
		// TODO: have errors returned include the file path of the error
		file, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		h, err := hocr.Parse(file)
		if err != nil {
			return err
		}
		// TODO: get page dimensions from image dimensions
		pdf.AddPageFormat("P", gofpdf.SizeType{Wd: pxToPt(1414), Ht: pxToPt(2500)})
		//pdf.SetTextRenderingMode(gofpdf.TextRenderingModeInvisible)
		// TODO: add page image
		for _, l := range h.Lines {
			coords, err := hocr.BoxCoords(l.Title)
			if err != nil {
				return err
			}
			pdf.SetXY(pxToPt(coords[0]), pxToPt(coords[1]))
			// TODO: html escape text
			pdf.CellFormat(pxToPt(coords[2]), pxToPt(coords[3]), hocr.LineText(l), "", 0, "T", false, 0, "")
		}
		return nil
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

	// TODO: this will go in pdf.go in due course, potentially with a
	//       type which covers gofpdf.Fpdf, and an interface, so that
	//       the backend can be switched out like aws.go
	pdf := gofpdf.New("P", "pt", "A4", "")
	// Even though it's invisible, we need to add a font which can do UTF-8 so text is correctly rendered
	// TODO: find a font that's closer to the average dimensions of the
	//       text we're dealing with, and put it somewhere sensible
	pdf.AddUTF8Font("dejavu", "", "DejaVuSansCondensed.ttf")
	pdf.SetFont("dejavu", "", 10)
	pdf.SetAutoPageBreak(false, float64(0))

	err := filepath.Walk(flag.Arg(0), walker(pdf))
	if err != nil {
		log.Fatalln("Failed to walk", flag.Arg(0), err)
        }

	err = pdf.OutputFileAndClose(flag.Arg(1))
	if err != nil {
		log.Fatalln("Failed to save", flag.Arg(1), err)
        }
}
