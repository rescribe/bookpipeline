package preproc

// TODO: add different pages as test cases
// TODO: test non integral img version

import (
	"flag"
	"image"
	"image/draw"
	"image/png"
	"os"
)

var slow = flag.Bool("slow", false, "include slow tests")
var update = flag.Bool("update", false, "update golden files")

func decode(s string) (*image.Gray, error) {
	f, err := os.Open(s)
	defer f.Close()
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	gray := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(gray, b, img, b.Min, draw.Src)
	return gray, nil
}

func imgsequal(img1 *image.Gray, img2 *image.Gray) bool {
	b := img1.Bounds()
	if !b.Eq(img2.Bounds()) {
		return false
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r0, g0, b0, a0 := img1.At(x, y).RGBA()
			r1, g1, b1, a1 := img2.At(x, y).RGBA()
			if r0 != r1 {
				return false
			}
			if g0 != g1 {
				return false
			}
			if b0 != b1 {
				return false
			}
			if a0 != a1 {
				return false
			}
		}
	}
	return true
}
