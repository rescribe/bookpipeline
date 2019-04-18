package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"log"
	"os"

	"rescribe.xyz/go.git/binarize"
)

type windowslice struct {
	topleft     uint64
	topright    uint64
	bottomleft  uint64
	bottomright uint64
}

func getwindowslice(i [][]uint64, x int, size int) windowslice {
	maxy := len(i) - 1
	maxx := x + size
	if maxx > len(i[0])-1 {
		maxx = len(i[0]) - 1
	}

	return windowslice{i[0][x], i[0][maxx], i[maxy][x], i[maxy][maxx]}
}

// checkwindow checks the window from x to see whether more than
// thresh proportion of the pixels are white, if so it returns true.
func checkwindow(integral [][]uint64, x int, size int, thresh float64) bool {
	height := len(integral)
	window := getwindowslice(integral, x, size)
	// divide by 255 as each on pixel has the value of 255
	sum := (window.bottomright + window.topleft - window.topright - window.bottomleft) / 255
	area := size * height
	proportion := float64(area)/float64(sum) - 1
	return proportion <= thresh
}

// cleanimg fills the sections of image not within the boundaries
// of lowedge and highedge with white
func cleanimg(img *image.Gray, lowedge int, highedge int) *image.Gray {
	b := img.Bounds()
	new := image.NewGray(b)

	// set left edge white
	for x := b.Min.X; x < lowedge; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			new.SetGray(x, y, color.Gray{255})
		}
	}
	// copy middle
	for x := lowedge; x < highedge; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			new.SetGray(x, y, img.GrayAt(x, y))
		}
	}
	// set right edge white
	for x := highedge; x < b.Max.X; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			new.SetGray(x, y, color.Gray{255})
		}
	}

	return new
}

// findedges finds the edges of the main content, by moving a window of wsize
// from the middle of the image to the left and right, stopping when it reaches
// a point at which there is a lower proportion of black pixels than thresh.
func findedges(integral [][]uint64, wsize int, thresh float64) (int, int) {
	maxx := len(integral[0]) - 1
	var lowedge, highedge int = 0, maxx

	for x := maxx / 2; x < maxx-wsize; x++ {
		if checkwindow(integral, x, wsize, thresh) {
			highedge = x + (wsize / 2)
			break
		}
	}

	for x := maxx / 2; x > 0; x-- {
		if checkwindow(integral, x, wsize, thresh) {
			lowedge = x - (wsize / 2)
			break
		}
	}

	return lowedge, highedge
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: cleanup [-t thresh] [-w winsize] inimg outimg\n")
		flag.PrintDefaults()
	}
	wsize := flag.Int("w", 5, "Window size for mask finding algorithm.")
	thresh := flag.Float64("t", 0.05, "Threshold for the proportion of black pixels below which a window is determined to be the edge.")
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	f, err := os.Open(flag.Arg(0))
	defer f.Close()
	if err != nil {
		log.Fatalf("Could not open file %s: %v\n", flag.Arg(0), err)
	}
	img, _, err := image.Decode(f)
	if err != nil {
		log.Fatalf("Could not decode image: %v\n", err)
	}
	b := img.Bounds()
	gray := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(gray, b, img, b.Min, draw.Src)

	integral := binarize.Integralimg(gray)

	lowedge, highedge := findedges(integral, *wsize, *thresh)

	clean := cleanimg(gray, lowedge, highedge)

	f, err = os.Create(flag.Arg(1))
	if err != nil {
		log.Fatalf("Could not create file %s: %v\n", flag.Arg(1), err)
	}
	defer f.Close()
	err = png.Encode(f, clean)
	if err != nil {
		log.Fatalf("Could not encode image: %v\n", err)
	}
}
