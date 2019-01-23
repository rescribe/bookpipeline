package parse

// TODO: integrate in line-conf-buckets linedetail
// TODO: add BucketUp() function here that does what both line-conf-buckets-tess.go
//       and line-conf-buckets.go do
// TODO: consider naming this package line, and separating it from hocr and prob

import (
	"image"
	"image/png"
	"io"
)

type LineDetail struct {
	Name string
	Avgconf float64
	Img CopyableLine
	Text string
	Hocrname string
}

type CopyableLine interface {
	CopyLineTo(io.Writer) (error)
}

// This is an implementation of the CopyableLine interface that
// stores the image directly as an image.Image
type ImgDirect struct {
	Img image.Image
}

func (i ImgDirect) CopyLineTo(w io.Writer) (error) {
	err := png.Encode(w, i.Img)
	if err != nil {
		return err
	}
	return nil
}

type LineDetails []LineDetail

// Used by sort.Sort.
func (l LineDetails) Len() int { return len(l) }

// Used by sort.Sort.
func (l LineDetails) Less(i, j int) bool {
	return l[i].Avgconf < l[j].Avgconf
}

// Used by sort.Sort.
func (l LineDetails) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
