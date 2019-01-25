package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"rescribe.xyz/go.git/lib/hocr"
	"rescribe.xyz/go.git/lib/line"
	"rescribe.xyz/go.git/lib/prob"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: avg-lines [-html dir] [-nosort] [prob1] [hocr1] [prob2] [...]\n")
		fmt.Fprintf(os.Stderr, "Prints a report of the average confidence for each line, sorted\n")
		fmt.Fprintf(os.Stderr, "from worst to best.\n")
		fmt.Fprintf(os.Stderr, "Both .hocr and .prob files can be processed.\n")
		fmt.Fprintf(os.Stderr, "For .hocr files, the x_wconf data is used to calculate confidence.\n")
		fmt.Fprintf(os.Stderr, "The .prob files are generated using ocropy-rpred's --probabilities\n")
		fmt.Fprintf(os.Stderr, "option.\n\n")
		flag.PrintDefaults()
	}
	var html = flag.String("html", "", "Output in html format to the specified directory")
	var nosort = flag.Bool("nosort", false, "Don't sort lines by confidence")
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	var err error
	lines := make(line.Details, 0)

	for _, f := range flag.Args() {
		var newlines line.Details
		switch ext := filepath.Ext(f); ext {
		case ".prob":
			newlines, err = prob.GetLineDetails(f)
		case ".hocr":
			newlines, err = hocr.GetLineDetails(f)
		default:
			log.Printf("Skipping file '%s' as it isn't a .prob or .hocr\n", f)
			continue
		}
		if err != nil {
			log.Fatal(err)
		}

		for _, l := range newlines {
			lines = append(lines, l)
		}
	}

	if *nosort == false {
		sort.Sort(lines)
	}

	if *html == "" {
		for _, l := range lines {
			fmt.Printf("%s %s: %.2f%%\n", l.OcrName, l.Name, l.Avgconf)
		}
	} else {
		htmlout(*html, lines)
	}
}
