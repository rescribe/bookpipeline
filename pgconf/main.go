package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"rescribe.xyz/go.git/lib/hocr"
	"rescribe.xyz/go.git/lib/line"
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

	var err error
	lines := make(line.Details, 0)

	for _, f := range flag.Args() {
		var newlines line.Details
		newlines, err = hocr.GetLineBasics(f)
		if err != nil {
			log.Fatal(err)
		}

		for _, l := range newlines {
			lines = append(lines, l)
		}
	}

	if len(lines) == 0 {
		fmt.Printf("No lines found\n")
		os.Exit(0)
	}

	var total float64
	for _, l := range lines {
		total += l.Avgconf
	}
	avg := total / float64(len(lines))

	fmt.Printf("%0.0f\n", avg * 100)
}
