package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"rescribe.xyz/utils/pkg/hocr"
)

//TO DO: make writetofile return an error and handle that accordingly
// potential TO DO: add text versions where footer is cropped on odd/even pages only


// the trimblanks function trims the blank lines from a text input
func trimblanks(hocrfile string) string {

	filein := bytes.NewBufferString(hocrfile)

	var noblanks string

	scanner := bufio.NewScanner(filein)

	for scanner.Scan() {

		eachline := scanner.Text()
		trimmed := strings.TrimSpace(eachline)
		if len(trimmed) != 0 {
			noblanks = noblanks + eachline + "\n"
		}
	}
	return noblanks

}

// the dehyphenateString function is copy-pasted from Nick's code (see rescribe.xyz/utils/cmd/dehyphenator/main.go), written to dehyphenate
// a string or hocr file. Only one small change from the original: the string is dehyphenated and concatenated WITHOUT line breaks
func dehyphenateString(in string) string {
	var newlines []string
	lines := strings.Split(in, "\n")
	for i, line := range lines {
		words := strings.Split(line, " ")
		last := words[len(words)-1]
		// the - 2 here is to account for a trailing newline and counting from zero
		if len(last) > 0 && last[len(last) - 1] == '-' && i < len(lines) - 2 {
			nextwords := strings.Split(lines[i+1], " ")
			if len(nextwords) > 0 {
				line = line[0:len(line)-1] + nextwords[0]
			}
			if len(nextwords) > 1 {
				lines[i+1] = strings.Join(nextwords[1:], " ")
			} else {
				lines[i+1] = ""
			}
		}
		newlines = append(newlines, line)
	}
	return strings.Join(newlines, " ")
}


// the fullcrop function takes a text input and crops the first and the last line (if text is at least 2 lines long)
func fullcrop(noblanks string) string {


	alllines := strings.Split(noblanks, "\n")
	
	if len(alllines) <= 2 {
	return ""
	}	else {
	return strings.Join(alllines[1:len(alllines)-2], "\n")
	}

}

// the headcrop function takes a text input and crops the first line provided text is longer than 1 line
func headcrop(noblanks string) string {

	alllines := strings.Split(noblanks, "\n")

	switch {

	case len(alllines) == 2:
		return strings.Join(alllines[1:], "\n")

	case len(alllines) < 2:
		return ""

	default:
		return strings.Join(alllines[1:], "\n")

	}

}

// the footcrop function takes a text input and crops the last line provided text is longer than 1 line
func footcrop(noblanks string) string {

	alllines := strings.Split(noblanks, "\n")

	switch {

	case len(alllines) == 2:
		return strings.Join(alllines[0:len(alllines)-2], "\n")

	case len(alllines) < 2:
		return ""

	default:
		return strings.Join(alllines[0:len(alllines)-2], "\n")

	}

}

// the convertselect function selects the hocr from the bookdirectory above a given confidence threshold and
// converts it to text, trims each text and appends all into one textbase and saves it as a text file.
// the function returns one full version, one with headers and footers cropped, one with only
//headers cropped
func convertselect(bookdirectory, hocrfilename string, confthresh int) (string, string, string, string) {

	var alltxt string
	var croptxt string
	var killheadtxt string
	var footkilltxt string


	hocrfilepath := filepath.Join(bookdirectory, hocrfilename)

	confpath := filepath.Join(bookdirectory, "conf")

	readConf, err := os.Open(confpath)
	if err != nil {
		log.Fatalf("failed to open file: %s", err)
	}
	defer readConf.Close()

	scanner := bufio.NewScanner(readConf)
	var confline string
	var confvalue int

	for scanner.Scan() {
		confline = scanner.Text()
		if strings.Contains(confline, hocrfilename) {
			substring := strings.Split(confline, "	")
			if len(substring) != 2 {
				log.Fatalf("Bailing as conf file %s doesn't seem to be formatted correctly (wants 2 fields separated by '  ')\n", confpath)
			}
			confvalue, _ = strconv.Atoi(substring[1])
		}

	}
	readConf.Close()

	if confvalue > confthresh {
		hocrfiletext, err := hocr.GetText(hocrfilepath)
		if err != nil {
			log.Fatal(err)
		}
		
		
		trimbest := trimblanks(hocrfiletext)
		
		alltxt = dehyphenateString(trimbest)
			
		croptxt = dehyphenateString(fullcrop(trimbest))
	
		killheadtxt = dehyphenateString(headcrop(trimbest))
		
		footkilltxt = dehyphenateString(footcrop(trimbest))
		

	}
	return alltxt, croptxt, killheadtxt, footkilltxt
}

// the writetofile function takes a directory, filename and text input and creates a text file within the bookdirectory from them.
func writetofile(bookdirectory, textfilebase, txt string) error {
	alltxtfile := filepath.Join(bookdirectory, textfilebase)
	
	file, err := os.Create(alltxtfile)
	if err != nil {
		return fmt.Errorf("Error opening file %s: %v", alltxtfile, err)
	}
	defer file.Close()
	if _, err := file.WriteString(txt); err != nil {
		log.Println(err)
	}
return err

}

func main() {

	confthresh := flag.Int("c", 30, "Chosen confidence threshold. Default:30")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: command -c confidence-threshold bookdirectory \n")
		fmt.Fprintf(os.Stderr, "Creates different text versions from the hocr files of a bookdirectory.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	bookdirectory := flag.Arg(0)
	confthreshstring := strconv.Itoa(*confthresh)
	
	fmt.Println("Postprocessing", bookdirectory, "with threshold", *confthresh)

	bestpath := filepath.Join(bookdirectory, "best")

	readBest, err := ioutil.ReadFile(bestpath)
	if err != nil {
		log.Fatalf("failed to read file: %s", err)
	}

	Bestin := string([]byte(readBest))
	bestslice := strings.Split(Bestin, "\n")
	sort.Strings(bestslice)

	var all, crop, killhead, killfoot string

	for _, v := range bestslice {

		if v != "" {
			alltxt, croptxt, killheadtxt, footkilltxt := convertselect(bookdirectory, v, *confthresh)
			all = all + " " + alltxt
			crop = crop + " " + croptxt
			killhead = killhead + " " + killheadtxt
			killfoot = killfoot + " " + footkilltxt
		
		}
	}
	
	
	bookname:= filepath.Base(bookdirectory)
		b := bookname + "_" + confthreshstring

		err1 := writetofile(bookdirectory, b + "_all.txt", all)
		if err1 != nil {
		log.Fatalf("Ah shit, we're going down, Nick says ABORT! %v", err1)
		}
		
		err2 := writetofile(bookdirectory, b + "_crop.txt", crop)
		if err2 != nil {
		log.Fatalf("Ah shit, we're going down, Nick says ABORT! %v", err2)
		}
		
		err3 := writetofile(bookdirectory, b + "_nohead.txt", killhead)
		if err3 != nil {
		log.Fatalf("Ah shit, we're going down, Nick says ABORT! %v", err3)
		}
		
		err4 := writetofile(bookdirectory, b + "_nofoot.txt", killfoot)
		if err4 != nil {
		log.Fatalf("Ah shit, we're going down, Nick says ABORT! %v", err4)
		}

}
