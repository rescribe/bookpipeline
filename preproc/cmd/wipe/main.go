package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"log"
	"os"

	"rescribe.xyz/go.git/preproc"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wipe [-m minperc] [-t thresh] [-w winsize] inimg outimg\n")
		fmt.Fprintf(os.Stderr, "Wipes the sections of an image which are outside the content area.\n")
		flag.PrintDefaults()
	}
	min := flag.Int("m", 30, "Minimum percentage of the image width for the content width calculation to be considered valid.")
	thresh := flag.Float64("t", 0.05, "Threshold for the proportion of black pixels below which a window is determined to be the edge. Higher means more aggressive wiping.")
	wsize := flag.Int("w", 5, "Window size for mask finding algorithm.")
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

	clean := preproc.Wipe(gray, *wsize, *thresh, *min)

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
