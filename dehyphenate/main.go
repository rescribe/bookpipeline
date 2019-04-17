package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"rescribe.xyz/go.git/lib/hocr"
)

// BUGS:
// - loses all elements not captured in hocr structure such as html headings
//   might be best to copy the header and footer separately and put the hocr in between, but would still need to ensure all elements are captured
// - loses any formatting; doesn't need to be identical, but e.g. linebreaks after elements would be handy
// - need to handle OcrChar

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: dehyphenate hocrin hocrout\n")
		fmt.Fprintf(os.Stderr, "Dehyphenates a hocr file.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(1)
	}

	in, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalf("Error reading %s: %v", flag.Arg(1), err)
	}
	h, err := hocr.Parse(in)
	if err != nil {
		log.Fatal(err)
	}

	for i, l := range h.Lines {
		w := l.Words[len(l.Words)-1]
		if len(w.Chars) == 0 {
			if len(w.Text) > 0 && w.Text[len(w.Text) - 1] == '-' {
				h.Lines[i].Words[len(l.Words)-1].Text = w.Text[0:len(w.Text)-1] + h.Lines[i+1].Words[0].Text
				h.Lines[i+1].Words[0].Text = ""
			}
		} else {
			log.Printf("TODO: handle OcrChar")
		}
	}

	f, err := os.Create(flag.Arg(1))
	if err != nil {
		log.Fatalf("Error creating file %s: %v", flag.Arg(1), err)
	}
	defer f.Close()
	e := xml.NewEncoder(f)
	err = e.Encode(h)
	if err != nil {
		log.Fatalf("Error encoding XML: %v", err)
	}
}
