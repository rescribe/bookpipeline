package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Ernyoke/Imger/imgio"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: binarize [-w num] [-k num] inimg outimg\n")
		flag.PrintDefaults()
	}
	wsize := flag.Int("w", 31, "Window size for sauvola algorithm")
        ksize := flag.Float64("k", 0.5, "K for sauvola algorithm")
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	img, err := imgio.ImreadGray(flag.Arg(0))
	if err != nil {
		log.Fatalf("Could not read image %s\n", flag.Arg(0))
	}

	// TODO: should be able to estimate an appropriate window size based on resolution
	thresh := Sauvola(img, *ksize, *wsize)
	if err != nil {
		log.Fatal("Error binarising image\n")
	}

	err = imgio.Imwrite(thresh, flag.Arg(1))
	if err != nil {
		log.Fatalf("Could not write image %s\n", flag.Arg(1))
	}
}
