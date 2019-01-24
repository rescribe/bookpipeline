package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"git.rescribe.xyz/testingtools/parse"
	"git.rescribe.xyz/testingtools/parse/hocr"
	"git.rescribe.xyz/testingtools/parse/prob"
)

func main() {
	b := parse.BucketSpecs{
		// minimum confidence, name
		{ 0, "bad" },
		{ 0.95, "95to98" },
		{ 0.98, "98plus" },
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: bucket-lines [-d dir] [-s specs.json] [hocr1] [prob1] [hocr2] [...]\n")
		fmt.Fprintf(os.Stderr, "Copies image-text line pairs into different directories according\n")
		fmt.Fprintf(os.Stderr, "to the average character probability for the line.\n\n")
		fmt.Fprintf(os.Stderr, "Both .hocr and .prob files can be processed.\n\n")
		fmt.Fprintf(os.Stderr, "For .hocr files, the x_wconf data is used to calculate confidence.\n\n")
		fmt.Fprintf(os.Stderr, "The .prob files are generated using ocropy-rpred's --probabilities\n")
		fmt.Fprintf(os.Stderr, "option.\n\n")
		fmt.Fprintf(os.Stderr, "The .prob and .hocr files are assumed to be in the same directory\n")
		fmt.Fprintf(os.Stderr, "as the line's image and text files.\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nAn example specs.json file would be the following:\n")
		fmt.Fprintf(os.Stderr, "[{\"min\": 0, \"name\": \"terrible\"}, {\"min\": 0.80, \"name\": \"ok\"}, {\"min\": 0.98, \"name\": \"great\"}]\n")
	}
	dir := flag.String("d", "buckets", "Directory to store the buckets")
	specs := flag.String("s", "", "JSON file describing specs to bucket into")
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	if *specs != "" {
		js, err := ioutil.ReadFile(*specs)
		if err != nil {
			log.Fatal(err)
		}
		err = json.Unmarshal(js, &b)
		if err != nil {
			log.Fatal(err)
		}
	}

	var err error
	lines := make(parse.LineDetails, 0)

	for _, f := range flag.Args() {
		var newlines parse.LineDetails
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

	stats, err := parse.BucketUp(lines, b, *dir)
	if err != nil {
		log.Fatal(err)
	}

	parse.PrintBucketStats(os.Stdout, stats)
}
