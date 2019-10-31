package main

import (
	"errors"
	"flag"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"rescribe.xyz/gofpdf"
	"rescribe.xyz/utils/pkg/hocr"
)

const pageWidth = 5 // pageWidth in inches

// pxToPt converts a pixel value into a pt value (72 pts per inch)
// This uses pageWidth to determine the appropriate value
func pxToPt(i int) float64 {
	return float64(i) / pageWidth
}

// setupPdf creates a new PDF with appropriate settings and fonts
// TODO: this will go in pdf.go in due course
// TODO: find a font that's closer to the average dimensions of the
//       text we're dealing with, and put it somewhere sensible
func setupPdf() *gofpdf.Fpdf {
	pdf := gofpdf.New("P", "pt", "A4", "")
	// Even though it's invisible, we need to add a font which can do
	// UTF-8 so that text renders correctly.
	pdf.AddUTF8Font("dejavu", "", "DejaVuSansCondensed.ttf")
	pdf.SetFont("dejavu", "", 10)
	pdf.SetAutoPageBreak(false, float64(0))
	return pdf
}

// addPage adds a page to the pdf with an image and (invisible)
// text from an hocr file
func addPage(pdf *gofpdf.Fpdf, imgpath string, hocrpath string) error {
	file, err := ioutil.ReadFile(hocrpath)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not read file %s: %v", hocrpath, err))
	}
	// TODO: change hocr.Parse to take a Reader rather than []byte
	h, err := hocr.Parse(file)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not parse hocr in file %s: %v", hocrpath, err))
	}

	f, err := os.Open(imgpath)
	defer f.Close()
	if err != nil {
		return errors.New(fmt.Sprintf("Could not open file %s: %v", imgpath, err))
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not decode image: %v", err))
	}
	b := img.Bounds()
	pdf.AddPageFormat("P", gofpdf.SizeType{Wd: pxToPt(b.Dx()), Ht: pxToPt(b.Dy())})

	// TODO: check for errors in pdf as going through

	_ = pdf.RegisterImageOptions(imgpath, gofpdf.ImageOptions{})
	pdf.ImageOptions(imgpath, 0, 0, pxToPt(b.Dx()), pxToPt(b.Dy()), false, gofpdf.ImageOptions{}, 0, "")

	pdf.SetTextRenderingMode(gofpdf.TextRenderingModeInvisible)

	for _, l := range h.Lines {
		coords, err := hocr.BoxCoords(l.Title)
		if err != nil {
			continue
		}
		pdf.SetXY(pxToPt(coords[0]), pxToPt(coords[1]))
		pdf.CellFormat(pxToPt(coords[2]), pxToPt(coords[3]), html.UnescapeString(hocr.LineText(l)), "", 0, "T", false, 0, "")
	}
	return nil
}

func savePdf(pdf *gofpdf.Fpdf, p string) error {
	return pdf.OutputFileAndClose(p)
}

func walker(pdf *gofpdf.Fpdf) filepath.WalkFunc {
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
		return addPage(pdf, imgpath, fpath)
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

	pdf := setupPdf()

	err := filepath.Walk(flag.Arg(0), walker(pdf))
	if err != nil {
		log.Fatalln("Failed to walk", flag.Arg(0), err)
	}

	err = savePdf(pdf, flag.Arg(1))
	if err != nil {
		log.Fatalln("Failed to save", flag.Arg(1), err)
	}
}
