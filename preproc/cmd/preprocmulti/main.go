package main

// TODO: come up with a way to set a good ksize automatically

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
	ksizes := []float64{0.1, 0.2, 0.4, 0.5}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: preprocmulti [-bt bintype] [-bw winsize] [-m minperc] [-nowipe] [-ws wipesize] inimg outbase\n")
		fmt.Fprintf(os.Stderr, "Binarize and preprocess an image, with multiple binarisation levels,\n")
		fmt.Fprintf(os.Stderr, "saving images to outbase_bin{k}.png.\n")
		fmt.Fprintf(os.Stderr, "Binarises with these levels for k: %v.\n", ksizes)
		flag.PrintDefaults()
	}
	binwsize := flag.Int("bw", 0, "Window size for sauvola binarization algorithm. Set automatically based on resolution if not set.")
	btype := flag.String("bt", "binary", "Type of binarization threshold. binary or zeroinv are currently implemented.")
	min := flag.Int("m", 30, "Minimum percentage of the image width for the content width calculation to be considered valid.")
	nowipe := flag.Bool("nowipe", false, "Disable wiping completely.")
	wipewsize := flag.Int("ws", 5, "Window size for wiping algorithm.")
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

	var clean, threshimg image.Image
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

		if !*nowipe {
			log.Print("Wiping sides")
			clean = preproc.Wipe(threshimg.(*image.Gray), *wipewsize, k*0.02, *min)
		} else {
			clean = threshimg
		}

		savefn := fmt.Sprintf("%s_bin%0.1f.png", flag.Arg(1), k)
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
