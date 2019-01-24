package main

import (
	"flag"
	"fmt"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"git.rescribe.xyz/testingtools/parse"
	"git.rescribe.xyz/testingtools/parse/hocr"
	"git.rescribe.xyz/testingtools/parse/prob"
)

// TODO: maybe move this into hocr.go
func detailsFromHocr(f string) (parse.LineDetails, error) {
	var newlines parse.LineDetails

	file, err := ioutil.ReadFile(f)
	if err != nil {
		return newlines, err
	}

	h, err := hocr.Parse(file)
	if err != nil {
		return newlines, err
	}

	pngfn := strings.Replace(f, ".hocr", ".png", 1)
	pngf, err := os.Open(pngfn)
	if err != nil {
		return newlines, err
	}
	defer pngf.Close()
	img, err := png.Decode(pngf)
	if err != nil {
		return newlines, err
	}

	n := strings.Replace(filepath.Base(f), ".hocr", "", 1)
	return hocr.GetLineDetails(h, img, n)
}

func main() {
	// TODO: Allow different specs to be used for .prob vs .hocr. Do this
	//       by adding a field to LineDetails that is linked to a named
	//       BucketSpecs.
	b := parse.BucketSpecs{
		// minimum confidence, name
		{ 0, "bad" },
		{ 0.95, "95to98" },
		{ 0.98, "98plus" },
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: bucket-lines [-d dir] [hocr1] [prob1] [hocr2] [...]\n")
		fmt.Fprintf(os.Stderr, "Copies image-text line pairs into different directories according\n")
		fmt.Fprintf(os.Stderr, "to the average character probability for the line.\n\n")
		fmt.Fprintf(os.Stderr, "Both .hocr and .prob files can be processed.\n\n")
		fmt.Fprintf(os.Stderr, "For .hocr files, the x_wconf data is used to calculate confidence.\n\n")
		fmt.Fprintf(os.Stderr, "The .prob files are generated using ocropy-rpred's --probabilities\n")
		fmt.Fprintf(os.Stderr, "option.\n\n")
		fmt.Fprintf(os.Stderr, "The .prob and .hocr files are assumed to be in the same directory\n")
		fmt.Fprintf(os.Stderr, "as the line's image and text files.\n\n")
		flag.PrintDefaults()
	}
	dir := flag.String("d", "buckets", "Directory to store the buckets")
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	var err error
	lines := make(parse.LineDetails, 0)

	for _, f := range flag.Args() {
		var newlines parse.LineDetails
		switch ext := filepath.Ext(f); ext {
			case ".prob":
				newlines, err = prob.GetLineDetails(f)
			case ".hocr":
				newlines, err = detailsFromHocr(f)
			default:
				log.Printf("Skipping file '%s' as it isn't a .prob or .hocr\n", f)
		}
		if err != nil {
			log.Fatal(err)
		}

		for _, l := range newlines {
			lines = append(lines, l)
		}
	}

	stats, err := parse.BucketUp(lines, b, *dir)
	if err != nil {
		log.Fatal(err)
	}

	parse.PrintBucketStats(os.Stdout, stats)
}
