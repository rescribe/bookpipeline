package preproc

import (
	"errors"
	"image"
	"math"
)

// TODO: name better; maybe verb, x-er
// TODO: implement these for regular image, and use them to make
//       image functions generic for integral and non- images
type UsefulImg interface {
	MeanWindow()
	MeanStdDevWindow()
}

func mean(i []int) float64 {
	sum := 0
	for _, n := range i {
		sum += n
	}
	return float64(sum) / float64(len(i))
}

func stddev(i []int) float64 {
	m := mean(i)

	var sum float64
	for _, n := range i {
		sum += (float64(n) - m) * (float64(n) - m)
	}
	variance := sum / float64(len(i) - 1)
	return math.Sqrt(variance)
}

func meanstddev(i []int) (float64, float64) {
	m := mean(i)

	var sum float64
	for _, n := range i {
		sum += (float64(n) - m) * (float64(n) - m)
	}
	variance := float64(sum) / float64(len(i) - 1)
	return m, math.Sqrt(variance)
}

// gets the pixel values surrounding a point in the image
func surrounding(img *image.Gray, x int, y int, size int) []int {
	b := img.Bounds()
	step := size / 2

	miny := y - step
	if miny < b.Min.Y {
		miny = b.Min.Y
	}
	minx := x - step
	if minx < b.Min.X {
		minx = b.Min.X
	}
	maxy := y + step
	if maxy > b.Max.Y {
		maxy = b.Max.Y
	}
	maxx := x + step
	if maxx > b.Max.X {
		maxx = b.Max.X
	}

	var s []int
	for yi := miny; yi <= maxy; yi++ {
		for xi := minx; xi <= maxx; xi++ {
			s = append(s, int(img.GrayAt(xi, yi).Y))
		}
	}
	return s
}

func BinToZeroInv(bin *image.Gray, orig *image.RGBA) (*image.RGBA, error) {
	b := bin.Bounds()
	if ! b.Eq(orig.Bounds()) {
		return orig, errors.New("bin and orig images need to be the same dimensions")
	}
	newimg := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if bin.GrayAt(x, y).Y == 255 {
				newimg.Set(x, y, bin.GrayAt(x, y))
			} else {
				newimg.Set(x, y, orig.At(x, y))
			}
		}
	}

	return newimg, nil
}
