package bookpipeline

import (
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"os"

	"github.com/jung-kurt/gofpdf"
	"rescribe.xyz/utils/pkg/hocr"
)

const pageWidth = 5 // pageWidth in inches

// pxToPt converts a pixel value into a pt value (72 pts per inch)
// This uses pageWidth to determine the appropriate value
func pxToPt(i int) float64 {
	return float64(i) / pageWidth
}

type Fpdf struct {
	fpdf *gofpdf.Fpdf
}

// Setup creates a new PDF with appropriate settings and fonts
// TODO: find a font that's closer to the average dimensions of the
//       text we're dealing with
// TODO: once we have a good font, embed it in the binary as bytes
func (p *Fpdf) Setup() error {
	p.fpdf = gofpdf.New("P", "pt", "A4", "")
	// Even though it's invisible, we need to add a font which can do
	// UTF-8 so that text renders correctly.
	p.fpdf.AddUTF8Font("dejavu", "", "DejaVuSansCondensed.ttf")
	p.fpdf.SetFont("dejavu", "", 10)
	p.fpdf.SetAutoPageBreak(false, float64(0))
	return p.fpdf.Error()
}

// AddPage adds a page to the pdf with an image and (invisible)
// text from an hocr file
func (p *Fpdf) AddPage(imgpath, hocrpath string) error {
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
	p.fpdf.AddPageFormat("P", gofpdf.SizeType{Wd: pxToPt(b.Dx()), Ht: pxToPt(b.Dy())})

	// TODO: check for errors in pdf as going through

	_ = p.fpdf.RegisterImageOptions(imgpath, gofpdf.ImageOptions{})
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
