package main

// TODO: come up with a way to set a good ksize automatically
// TODO: add minimum size variable (default ~30%?) for wipe

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

// TODO: do more testing to see how good this assumption is
func autowsize(bounds image.Rectangle) int {
	return bounds.Dx() / 60
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: preproc [-bt bintype] [-bw winsize] [-k num] [-t thresh] [-ws wipesize] inimg outimg\n")
		fmt.Fprintf(os.Stderr, "Binarize and preprocess an image\n")
		flag.PrintDefaults()
	}
	binwsize := flag.Int("bw", 0, "Window size for sauvola binarization algorithm. Set automatically based on resolution if not set.")
	ksize := flag.Float64("k", 0.5, "K for sauvola binarization algorithm. This controls the overall threshold level. Set it lower for very light text (try 0.1 or 0.2).")
	btype := flag.String("bt", "binary", "Type of binarization threshold. binary or zeroinv are currently implemented.")
	wipewsize := flag.Int("ws", 5, "Window size for wiping algorithm.")
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

	if *binwsize == 0 {
		*binwsize = autowsize(b)
	}

	if *binwsize % 2 == 0 {
		*binwsize++
	}

	log.Print("Binarising")
	var threshimg image.Image
	threshimg = preproc.IntegralSauvola(gray, *ksize, *binwsize)

	if *btype == "zeroinv" {
		threshimg, err = preproc.BinToZeroInv(threshimg.(*image.Gray), img.(*image.RGBA))
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Print("Wiping sides")
	clean := preproc.Wipe(threshimg.(*image.Gray), *wipewsize, *thresh)

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
