package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type LineDetail struct {
	Filename string
	Avgconf float64
	Filebase string
	Basename string
	Dirname string
	Fulltext string
}

type LineDetails []LineDetail

// Used by sort.Sort.
func (l LineDetails) Len() int { return len(l) }

// Used by sort.Sort.
func (l LineDetails) Less(i, j int) bool {
	return l[i].Avgconf < l[j].Avgconf
}

// Used by sort.Sort.
func (l LineDetails) Swap(i, j int) { l[i], l[j] = l[j], l[i] }

func copyline(filebase string, dirname string, basename string, avgconf string, outdir string, todir string) (err error) {
	outname := filepath.Join(outdir, todir, filepath.Base(dirname) + "_" + basename + "_" + avgconf)
	//log.Fatalf("I'd use '%s' as outname, and '%s' as filebase\n", outname, filebase)

	for _, extn := range []string{".bin.png", ".txt"} {
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

	lines := make(LineDetails, 0)

	for _, f := range flag.Args() {
		file, err := os.Open(f)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		reader := bufio.NewReader(file)

		totalconf := float64(0)
		num := 0

		err = nil
		for err == nil {
			var line string
                        line, err = reader.ReadString('\n')
			fields := strings.Fields(line)

			if len(fields) == 2 {
				conf, converr := strconv.ParseFloat(fields[1], 64)
				if converr != nil {
					fmt.Fprintf(os.Stderr, "Error: can't convert '%s' to float (full line: %s)\n", fields[1], line)
					continue
				}
				totalconf += conf
				num += 1
			}
		}
		avg := totalconf / float64(num)

		if num == 0 || avg == 0 {
			continue
		}

		var linedetail LineDetail
		linedetail.Filename = f
		linedetail.Avgconf = avg
		linedetail.Filebase = strings.Replace(f, ".prob", "", 1)
		linedetail.Basename = filepath.Base(linedetail.Filebase)
		linedetail.Dirname = filepath.Dir(linedetail.Filebase)
		ft, ferr := ioutil.ReadFile(linedetail.Filebase + ".txt")
		if ferr != nil {
			log.Fatal(err)
		}
		linedetail.Fulltext = string(ft)
		lines = append(lines, linedetail)
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
		avgstr = avgstr[2:]
		err := copyline(l.Filebase, l.Dirname, l.Basename, avgstr, outdir, todir)
		if err != nil {
			log.Fatal(err)
		}
	}

	total := worstnum + mediumnum + bestnum

	fmt.Printf("Copied lines to %s\n", outdir)
	fmt.Printf("---------------------------------\n")
	fmt.Printf("Lines 98%%+ quality:     %d%%\n", 100 * bestnum / total)
	fmt.Printf("Lines 95-98%% quality:   %d%%\n", 100 * mediumnum / total)
	fmt.Printf("Lines <95%% quality:     %d%%\n", 100 * worstnum / total)
}
