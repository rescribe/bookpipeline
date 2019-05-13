package main

// TODO: add minimum size variable (default ~30%?)
// TODO: make into a small library
// TODO: have the integral image specific stuff done by interface functions

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"log"
	"os"

	"rescribe.xyz/go.git/binarize"
	"rescribe.xyz/go.git/preproc"
)

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

	lowedge, highedge := preproc.Findedges(integral, *wsize, *thresh)

	clean := preproc.Wipesides(gray, lowedge, highedge)

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
