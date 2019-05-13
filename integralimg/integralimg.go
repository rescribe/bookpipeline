package integralimg

import (
	"image"
	"math"
)

// I is the Integral Image
type I [][]uint64

// Sq contains an Integral Image and its Square
type WithSq struct {
	Img I
	Sq I
}

// Window is a part of an Integral Image
type Window struct {
	topleft uint64
	topright uint64
	bottomleft uint64
	bottomright uint64
	width int
	height int
}

// ToIntegralImg creates an integral image
func ToIntegralImg(img *image.Gray) I {
	var integral I
	var oldy, oldx, oldxy uint64
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		newrow := []uint64{}
		for x := b.Min.X; x < b.Max.X; x++ {
			oldx, oldy, oldxy = 0, 0, 0
			if x > 0 {
				oldx = newrow[x-1]
			}
			if y > 0 {
				oldy = integral[y-1][x]
			}
			if x > 0 && y > 0 {
				oldxy = integral[y-1][x-1]
			}
			pixel := uint64(img.GrayAt(x, y).Y)
			i := pixel + oldx + oldy - oldxy
			newrow = append(newrow, i)
		}
		integral = append(integral, newrow)
	}
	return integral
}

// ToSqIntegralImg creates an integral image of the square of all
// pixel values
func ToSqIntegralImg(img *image.Gray) I {
	var integral I
	var oldy, oldx, oldxy uint64
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		newrow := []uint64{}
		for x := b.Min.X; x < b.Max.X; x++ {
			oldx, oldy, oldxy = 0, 0, 0
			if x > 0 {
				oldx = newrow[x-1]
			}
			if y > 0 {
				oldy = integral[y-1][x]
			}
			if x > 0 && y > 0 {
				oldxy = integral[y-1][x-1]
			}
			pixel := uint64(img.GrayAt(x, y).Y)
			i := pixel * pixel + oldx + oldy - oldxy
			newrow = append(newrow, i)
		}
		integral = append(integral, newrow)
	}
	return integral
}

// ToAllIntegralImg creates a WithSq containing a regular and
// squared Integral Image
func ToAllIntegralImg(img *image.Gray) WithSq {
	var s WithSq
	s.Img = ToIntegralImg(img)
	s.Sq = ToSqIntegralImg(img)
	return s
}


// GetWindow gets the values of the corners of a part of an
// Integral Image, plus the dimensions of the part, which can
// be used to quickly calculate the mean of the area
func (i I) GetWindow(x, y, size int) Window {
	step := size / 2

	minx, miny := 0, 0
	maxy := len(i)-1
	maxx := len(i[0])-1

	if y > (step+1) {
		miny = y - step - 1
	}
	if x > (step+1) {
		minx = x - step - 1
	}

	if maxy > (y + step) {
		maxy = y + step
	}
	if maxx > (x + step) {
		maxx = x + step
	}

	return Window { i[miny][minx], i[miny][maxx], i[maxy][minx], i[maxy][maxx], maxx-minx, maxy-miny}
}

// Sum returns the sum of all pixels in a Window
func (w Window) Sum() uint64 {
	return w.bottomright + w.topleft - w.topright - w.bottomleft
}

// Size returns the total size of a Window
func (w Window) Size() int {
	return w.width * w.height
}

// Mean returns the average value of pixels in a Window
func (w Window) Mean() float64 {
	return float64(w.Sum()) / float64(w.Size())
}

// MeanWindow calculates the mean value of a section of an Integral
// Image
func (i I) MeanWindow(x, y, size int) float64 {
	return i.GetWindow(x, y, size).Mean()
}

// MeanStdDevWindow calculates the mean and standard deviation of
// a section on an Integral Image
func (i WithSq) MeanStdDevWindow(x, y, size int) (float64, float64) {
	imean := i.Img.GetWindow(x, y, size).Mean()
	smean := i.Sq.GetWindow(x, y, size).Mean()

	variance := smean - (imean * imean)

	return imean, math.Sqrt(variance)
}
