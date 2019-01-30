package main

// TODO: could look into other algorithms, see for examples see
//       the README at https://github.com/brandonmpetty/Doxa

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"log"
	"os"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: binarize [-w num] [-k num] inimg outimg\n")
		flag.PrintDefaults()
	}
	wsize := flag.Int("w", 31, "Window size for sauvola algorithm (needs to be odd)")
        ksize := flag.Float64("k", 0.5, "K for sauvola algorithm")
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	if *wsize % 2 == 0 {
		*wsize++
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

	// TODO: estimate an appropriate window size based on resolution
	thresh := IntegralSauvola(gray, *ksize, *wsize)

	f, err = os.Create(flag.Arg(1))
        if err != nil {
		log.Fatalf("Could not create file %s: %v\n", flag.Arg(1), err)
        }
	defer f.Close()
	err = png.Encode(f, thresh)
        if err != nil {
		log.Fatalf("Could not encode image: %v\n", err)
        }
}
