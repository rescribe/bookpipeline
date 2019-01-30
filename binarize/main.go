package main

// TODO: could look into other algorithms, see for examples see
//       the README at https://github.com/brandonmpetty/Doxa

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Ernyoke/Imger/imgio" // TODO: get rid of this and do things myself
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

	img, err := imgio.ImreadGray(flag.Arg(0))
	if err != nil {
		log.Fatalf("Could not read image %s\n", flag.Arg(0))
	}

	// TODO: estimate an appropriate window size based on resolution
	thresh := IntegralSauvola(img, *ksize, *wsize)

	err = imgio.Imwrite(thresh, flag.Arg(1))
	if err != nil {
		log.Fatalf("Could not write image %s\n", flag.Arg(1))
	}
}
