// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package bookpipeline

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"html"
	"image"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"os"

	"github.com/jung-kurt/gofpdf"
	"golang.org/x/image/draw"
	"rescribe.xyz/utils/pkg/hocr"
)

// TODO: maybe set this in Fpdf struct
const pageWidth = 5 // pageWidth in inches
const scaleSmaller = 3 // amount the width and height are divided by

// pxToPt converts a pixel value into a pt value (72 pts per inch)
// This uses pageWidth to determine the appropriate value
func pxToPt(i int) float64 {
	return float64(i) / pageWidth
}

type Fpdf struct {
	fpdf *gofpdf.Fpdf
}

// Setup creates a new PDF with appropriate settings and fonts
func (p *Fpdf) Setup() error {
	p.fpdf = gofpdf.New("P", "pt", "A4", "")

	// Even though it's invisible, we need to add a font which can do
	// UTF-8 so that text renders correctly.
	// We embed the font directly in the binary, compressed with zlib
	c := bytes.NewBuffer(dejavucondensed)
	r, err := zlib.NewReader(c)
	defer r.Close()
	if err != nil {
		return errors.New(fmt.Sprintf("Could not open compressed font: %v", err))
	}
	var b bytes.Buffer
	_, err = b.ReadFrom(r)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not read compressed font: %v", err))
	}
	p.fpdf.AddUTF8FontFromBytes("dejavu", "", b.Bytes())

	p.fpdf.SetFont("dejavu", "", 10)
	p.fpdf.SetAutoPageBreak(false, float64(0))
	return p.fpdf.Error()
}

// AddPage adds a page to the pdf with an image and (invisible)
// text from an hocr file
func (p *Fpdf) AddPage(imgpath, hocrpath string, smaller bool) error {
	file, err := ioutil.ReadFile(hocrpath)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not read file %s: %v", hocrpath, err))
	}
	h, err := hocr.Parse(file)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not parse hocr in file %s: %v", hocrpath, err))
	}

	imgf, err := os.Open(imgpath)
	defer imgf.Close()
	if err != nil {
		return errors.New(fmt.Sprintf("Could not open file %s: %v", imgpath, err))
	}
	img, imgtype, err := image.Decode(imgf)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not decode image: %v", err))
	}
	b := img.Bounds()
	if smaller {
		r := image.Rect(0, 0, b.Max.X/scaleSmaller, b.Max.Y/scaleSmaller)
		smimg := image.NewRGBA(r)
		draw.ApproxBiLinear.Scale(smimg, r, img, b, draw.Over, nil)
		img = smimg
	}

	var buf bytes.Buffer
	if imgtype == "jpeg" {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpeg.DefaultQuality})
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		return err
	}

	p.fpdf.AddPageFormat("P", gofpdf.SizeType{Wd: pxToPt(b.Dx()), Ht: pxToPt(b.Dy())})

	_ = p.fpdf.RegisterImageOptionsReader(imgpath, gofpdf.ImageOptions{ImageType: imgtype}, &buf)
	p.fpdf.ImageOptions(imgpath, 0, 0, pxToPt(b.Dx()), pxToPt(b.Dy()), false, gofpdf.ImageOptions{}, 0, "")

	p.fpdf.SetTextRenderingMode(3)

	for _, l := range h.Lines {
		for _, w := range l.Words {
			coords, err := hocr.BoxCoords(w.Title)
			if err != nil {
				continue
			}
			p.fpdf.SetXY(pxToPt(coords[0]), pxToPt(coords[1]))
			p.fpdf.CellFormat(pxToPt(coords[2]), pxToPt(coords[3]), html.UnescapeString(w.Text) + " ", "", 0, "T", false, 0, "")
		}
	}
	return p.fpdf.Error()
}

// Save saves the PDF to the file at path
func (p *Fpdf) Save(path string) error {
	return p.fpdf.OutputFileAndClose(path)
}
