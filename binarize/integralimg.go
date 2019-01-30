package main

import (
	"image"
	"math"
)

type integralwindow struct {
	topleft uint64
	topright uint64
	bottomleft uint64
	bottomright uint64
	width int
	height int
}

func integralimg(img *image.Gray) [][]uint64 {
	b := img.Bounds()
	var oldy, oldx, oldxy uint64
	var integral [][]uint64
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

func integralimgsq(img *image.Gray) [][]uint64 {
	b := img.Bounds()
	var oldy, oldx, oldxy uint64
	var integral [][]uint64
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

// this gets the values of the four corners of a window, which can
// be used to quickly calculate the mean of the area
func getintegralwindow(integral [][]uint64, x int, y int, size int) integralwindow {
	step := size / 2

	minx, miny := 0, 0
	maxy := len(integral)-1
	maxx := len(integral[0])-1

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

	return integralwindow { integral[miny][minx], integral[miny][maxx], integral[maxy][minx], integral[maxy][maxx], maxx-minx, maxy-miny}
}

func integralmean(integral [][]uint64, x int, y int, size int) float64 {
	i := getintegralwindow(integral, x, y, size)
	total := float64(i.bottomright + i.topleft - i.topright - i.bottomleft)
	sqsize := float64(i.width) * float64(i.height)
	return total / sqsize
}

func integralmeanstddev(integral [][]uint64, integralsq [][]uint64, x int, y int, size int) (float64, float64) {
	i := getintegralwindow(integral, x, y, size)
	isq := getintegralwindow(integralsq, x, y, size)

	var total, sqtotal, sqsize float64

	sqsize = float64(i.width) * float64(i.height)

	total = float64(i.bottomright + i.topleft - i.topright - i.bottomleft)
	sqtotal = float64(isq.bottomright + isq.topleft - isq.topright - isq.bottomleft)

	mean := total / sqsize
	variance := (sqtotal / sqsize) - (mean * mean)
	return mean, math.Sqrt(variance)
}
