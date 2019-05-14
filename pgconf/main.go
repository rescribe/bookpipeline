package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"rescribe.xyz/go.git/lib/hocr"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: pgconf hocr\n")
		fmt.Fprintf(os.Stderr, "Prints the total confidence for a page, as an average of the confidence of each word.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	avg, err := hocr.GetAvgConf(flag.Arg(0))
	if err != nil {
		log.Fatalf("Error retreiving confidence for %s: %v\n", flag.Arg(0), err)
	}

	fmt.Printf("%0.0f\n", avg)
}
