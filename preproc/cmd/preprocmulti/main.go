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

	"rescribe.xyz/go.git/integralimg"
	"rescribe.xyz/go.git/preproc"
)

// TODO: do more testing to see how good this assumption is
func autowsize(bounds image.Rectangle) int {
	return bounds.Dx() / 60
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: preproc [-bt bintype] [-bw winsize] [-t thresh] [-ws wipesize] inimg outbase\n")
		fmt.Fprintf(os.Stderr, "Binarize and preprocess an image, with multiple binarisation levels,\n")
		fmt.Fprintf(os.Stderr, "saving images to outbase_knum.png.\n")
		flag.PrintDefaults()
	}
	binwsize := flag.Int("bw", 0, "Window size for sauvola binarization algorithm. Set automatically based on resolution if not set.")
	btype := flag.String("bt", "binary", "Type of binarization threshold. binary or zeroinv are currently implemented.")
	wipewsize := flag.Int("ws", 5, "Window size for wiping algorithm.")
	thresh := flag.Float64("t", 0.05, "Threshold for the proportion of black pixels below which a window is determined to be the edge.")
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	log.Printf("Opening %s\n", flag.Arg(0))
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

	if *binwsize%2 == 0 {
		*binwsize++
	}

	ksizes := []float64{0.2, 0.3, 0.4, 0.5, 0.6}

	var threshimg image.Image
	log.Print("Precalculating integral images")
	integrals := integralimg.ToAllIntegralImg(gray)

	for _, k := range ksizes {
		log.Print("Binarising")
		threshimg = preproc.PreCalcedSauvola(integrals, gray, k, *binwsize)

		if *btype == "zeroinv" {
			threshimg, err = preproc.BinToZeroInv(threshimg.(*image.Gray), img.(*image.RGBA))
			if err != nil {
				log.Fatal(err)
			}
		}

		log.Print("Wiping sides")
		clean := preproc.Wipe(threshimg.(*image.Gray), *wipewsize, *thresh)

		savefn := fmt.Sprintf("%s_%0.1f.png", flag.Arg(1), k)
		log.Printf("Saving %s\n", savefn)
		f, err = os.Create(savefn)
		if err != nil {
			log.Fatalf("Could not create file %s: %v\n", savefn, err)
		}
		defer f.Close()
		err = png.Encode(f, clean)
		if err != nil {
			log.Fatalf("Could not encode image: %v\n", err)
		}
	}
}
