package main

// TODO: rename this emphasising that it's for .prob/ocropy
// TODO: set bucket dirname from cmdline

import (
	"flag"
	"fmt"
	"log"
	"os"

	"git.rescribe.xyz/testingtools/parse"
	"git.rescribe.xyz/testingtools/parse/prob"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: line-conf-buckets prob1 [prob2] [...]\n")
		fmt.Fprintf(os.Stderr, "Copies image-text line pairs into different directories according\n")
		fmt.Fprintf(os.Stderr, "to the average character probability for the line.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	lines := make(parse.LineDetails, 0)

	for _, f := range flag.Args() {
		newlines, err := prob.GetLineDetails(f)
		if err != nil {
			log.Fatal(err)
		}

                for _, l := range newlines {
                        lines = append(lines, l)
                }
	}

	b := parse.BucketSpecs{
		{ 0, "bad" },
		{ 0.95, "95to98" },
		{ 0.98, "98plus" },
	}

	stats, err := parse.BucketUp(lines, b, "newbuckets")
	if err != nil {
		log.Fatal(err)
	}

	parse.PrintBucketStats(os.Stdout, stats)
}
