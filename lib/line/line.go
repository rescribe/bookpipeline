package line

import (
	"image"
	"image/png"
	"io"
	"os"
)

type Detail struct {
	Name    string
	Avgconf float64
	Img     CopyableImg
	Text    string
	OcrName string
}

type CopyableImg interface {
	CopyLineTo(io.Writer) error
}

type Details []Detail

func (l Details) Len() int           { return len(l) }
func (l Details) Less(i, j int) bool { return l[i].Avgconf < l[j].Avgconf }
func (l Details) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

// This is an implementation of the CopyableImg interface that
// stores the image directly as an image.Image
type ImgDirect struct {
	Img image.Image
}

func (i ImgDirect) CopyLineTo(w io.Writer) error {
	err := png.Encode(w, i.Img)
	if err != nil {
		return err
	}
	return nil
}

// This is an implementation of the CopyableImg interface that
// stores the path of an image
type ImgPath struct {
	Path string
}

func (i ImgPath) CopyLineTo(w io.Writer) error {
	f, err := os.Open(i.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}
