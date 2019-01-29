package main

import (
	"image"
	"image/color"
	"math"
)

func mean(i []int) float64 {
	sum := 0
	for _, n := range i {
		sum += n
	}
	return float64(sum) / float64(len(i))
}

// TODO: is there a prettier way of doing this than float64() all over the place?
func stddev(i []int) float64 {
	m := mean(i)

	var sum float64
	for _, n := range i {
		sum += (float64(n) - m) * (float64(n) - m)
	}
	variance := float64(sum) / float64(len(i) - 1)
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

	miny := y - size/2
	if miny < b.Min.Y {
		miny = b.Min.Y
	}
	minx := x - size/2
	if minx < b.Min.X {
		minx = b.Min.X
	}
	maxy := y + size/2
	if maxy > b.Max.Y {
		maxy = b.Max.Y
	}
	maxx := x + size/2
	if maxx > b.Max.X {
		maxx = b.Max.X
	}

	var s []int
	for yi := miny; yi < maxy; yi++ {
		for xi := minx; xi < maxx; xi++ {
			s = append(s, int(img.GrayAt(xi, yi).Y))
		}
	}
	return s
}

// TODO: parallelize
// TODO: switch to using integral images to make faster; see paper
//       "Efficient Implementation of Local Adaptive Thresholding Techniques Using Integral Images"
func Sauvola(img *image.Gray, ksize float64, windowsize int) *image.Gray {
	b := img.Bounds()
	new := image.NewGray(b)

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			window := surrounding(img, x, y, windowsize)
			m, dev := meanstddev(window)
			threshold := m * (1 + ksize * ((dev / 128) - 1))
			if img.GrayAt(x, y).Y < uint8(threshold) {
				new.SetGray(x, y, color.Gray{0})
			} else {
				new.SetGray(x, y, color.Gray{255})
			}
		}
	}

	return new
}
