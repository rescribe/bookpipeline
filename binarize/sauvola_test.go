package binarize

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

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
	if ! b.Eq(img2.Bounds())  {
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

func TestBinarization(t *testing.T) {
	// TODO: test non-integral sauvola method too
	cases := []struct {
		name string
		orig string
		done string
		ksize float64
		wsize int
	}{
		{"integralsauvola", "testdata/pg1.png", "testdata/pg1_integralsauvola_k0.5_w41.png", 0.5, 41},
		{"integralsauvola", "testdata/pg1.png", "testdata/pg1_integralsauvola_k0.5_w19.png", 0.5, 19},
		{"integralsauvola", "testdata/pg1.png", "testdata/pg1_integralsauvola_k0.3_w19.png", 0.3, 19},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_%0.1f_%d", c.name, c.ksize, c.wsize), func(t *testing.T) {
			orig, err := decode(c.orig)
			if err != nil {
				t.Errorf("Could not open file %s: %v\n", c.orig, err)
			}
			done, err := decode(c.done)
			if err != nil {
				t.Errorf("Could not open file %s: %v\n", c.done, err)
			}
			new := IntegralSauvola(orig, c.ksize, c.wsize)
			if ! imgsequal(done, new) {
				t.Errorf("Binarized %s differs to %s\n", c.orig, c.done)
			}
		})
	}
}

func TestIntegralImg(t *testing.T) {
	// TODO: compare mean and stddev between integral and basic methods
}
