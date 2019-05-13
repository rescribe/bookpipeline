package preproc

// TODO: add different pages as test cases
// TODO: test non integral img version

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

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

func TestWipeSides(t *testing.T) {
	cases := []struct {
		name   string
		orig   string
		golden string
		thresh float64
		wsize  int
	}{
		{"integralwipesides", "testdata/pg1.png", "testdata/pg1_integralwipesides_t0.02_w5.png", 0.02, 5},
		{"integralwipesides", "testdata/pg1.png", "testdata/pg1_integralwipesides_t0.05_w5.png", 0.05, 5},
		{"integralwipesides", "testdata/pg1.png", "testdata/pg1_integralwipesides_t0.05_w25.png", 0.05, 25},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_%0.2f_%d", c.name, c.thresh, c.wsize), func(t *testing.T) {
			var actual *image.Gray
			orig, err := decode(c.orig)
			if err != nil {
				t.Fatalf("Could not open file %s: %v\n", c.orig, err)
			}
			actual = Wipe(orig, c.wsize, c.thresh)
			if *update {
				f, err := os.Create(c.golden)
				defer f.Close()
				if err != nil {
					t.Fatalf("Could not open file %s to update: %v\n", c.golden, err)
				}
				err = png.Encode(f, actual)
				if err != nil {
					t.Fatalf("Could not encode update of %s: %v\n", c.golden, err)
				}
			}
			golden, err := decode(c.golden)
			if err != nil {
				t.Fatalf("Could not open file %s: %v\n", c.golden, err)
			}
			if !imgsequal(golden, actual) {
				t.Errorf("Processed %s differs to %s\n", c.orig, c.golden)
			}
		})
	}
}
