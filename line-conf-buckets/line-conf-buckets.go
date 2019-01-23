package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"git.rescribe.xyz/testingtools/parse"
	"git.rescribe.xyz/testingtools/parse/prob"
)

// TODO: this is just a placeholder, do this more sensibly, as -tess does (hint: full txt should already be in the LineDetail)
func copyline(filebase string, dirname string, basename string, avgconf string, outdir string, todir string, l parse.LineDetail) (err error) {
	outname := filepath.Join(outdir, todir, filepath.Base(dirname) + "_" + basename + "_" + avgconf)
	//log.Fatalf("I'd use '%s' as outname, and '%s' as filebase\n", outname, filebase)

	for _, extn := range []string{".txt"} {
		infile, err := os.Open(filebase + extn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open %s\n", filebase + extn)
			return err
		}
		defer infile.Close()

		err = os.MkdirAll(filepath.Join(outdir, todir), 0700)
		if err != nil {
			return err
		}
	
		outfile, err := os.Create(outname + extn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s\n", outname + extn)
			return err
		}
		defer outfile.Close()
	
		_, err = io.Copy(outfile, infile)
		if err != nil {
			return err
		}
	}

	f, err := os.Create(outname + ".bin.png")
	if err != nil {
		return err
	}
	defer f.Close()
	err = l.Img.CopyLineTo(f)
	if err != nil {
		return err
	}

	return err
}

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
		file, err := os.Open(f)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		reader := bufio.NewReader(file)

		newlines, err := prob.GetLineDetails(f, reader)
		if err != nil {
			log.Fatal(err)
		}

                for _, l := range newlines {
                        lines = append(lines, l)
                }
		// explicitly close the file, so we can be sure we won't run out of
		// handles before defer runs
		file.Close()
	}

	sort.Sort(lines)

	worstnum := 0
	mediumnum := 0
	bestnum := 0

	outdir := "buckets" // TODO: set this from cmdline
	todir := ""

	for _, l := range lines {
		switch {
		case l.Avgconf < 0.95: 
			todir = "bad"
			worstnum++
		case l.Avgconf < 0.98:
			todir = "95to98"
			mediumnum++
		default:
			todir = "98plus"
			bestnum++
		}

		avgstr := strconv.FormatFloat(l.Avgconf, 'G', -1, 64)
		if len(avgstr) > 2 {
			avgstr = avgstr[2:]
		}
		filebase := strings.Replace(l.Name, ".prob", "", 1)
		basename := filepath.Base(filebase)
		err := copyline(filebase, l.OcrName, basename, avgstr, outdir, todir, l)
		if err != nil {
			log.Fatal(err)
		}
	}

	total := worstnum + mediumnum + bestnum

	if total == 0 {
		log.Fatal("No lines copied")
	}

	fmt.Printf("Copied lines to %s\n", outdir)
	fmt.Printf("---------------------------------\n")
	fmt.Printf("Lines 98%%+ quality:     %d%%\n", 100 * bestnum / total)
	fmt.Printf("Lines 95-98%% quality:   %d%%\n", 100 * mediumnum / total)
	fmt.Printf("Lines <95%% quality:     %d%%\n", 100 * worstnum / total)
}
