package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Ernyoke/Imger/threshold"
	"github.com/Ernyoke/Imger/imgio"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: binarize inimg outimg\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	img, err := imgio.ImreadGray(flag.Arg(0))
	if err != nil {
		log.Fatalf("Could not read image %s\n", flag.Arg(0))
	}

	thresh, err := threshold.OtsuThreshold(img, threshold.ThreshBinary)
	if err != nil {
		log.Fatal("Error binarising image\n")
	}

	err = imgio.Imwrite(thresh, flag.Arg(1))
	if err != nil {
		log.Fatalf("Could not write image %s\n", flag.Arg(1))
	}
}
