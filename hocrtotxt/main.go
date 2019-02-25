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
		fmt.Fprintf(os.Stderr, "Usage: hocrtotxt hocrfile\n")
		fmt.Fprintf(os.Stderr, "Prints the text from a hocr file.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	text, err := hocr.GetText(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s\n", text)
}
