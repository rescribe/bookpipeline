package preproc

// TODO: add different pages as test cases
// TODO: test non integral img version

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"
)

func TestWipeSides(t *testing.T) {
	cases := []struct {
		name   string
		orig   string
		golden string
		thresh float64
		wsize  int
	}{
		{"integralwipesides", "testdata/pg2.png", "testdata/pg2_integralwipesides_t0.02_w5.png", 0.02, 5},
		{"integralwipesides", "testdata/pg2.png", "testdata/pg2_integralwipesides_t0.05_w5.png", 0.05, 5},
		{"integralwipesides", "testdata/pg2.png", "testdata/pg2_integralwipesides_t0.05_w25.png", 0.05, 25},
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
