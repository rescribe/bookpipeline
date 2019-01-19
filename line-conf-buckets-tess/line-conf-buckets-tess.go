package main

// TODO: combine this with line-conf-buckets, separating the parsing
//       out to a separate library, probably
// see https://github.com/OCR-D/ocrd-train/issues/7 and https://github.com/OCR-D/ocrd-train/
// for tips on creating lines of tif/txt. best thing is to use hocr-extract-images to extract
// images for each line, based on tesseract's hocr output. can then copy the ground truth
// for that
// initial plan for this is to identify the lines which are best, and extract the text, then
// later can extract the images from them
//
// ok, am parsing the hocr now, workflow should be:
// - run hocr-extract-images (outside of this) and have a directory of images named line-000.png
// - run this with hocr and hocr-images dir
//   this then saves the text for the line alongside copying the image from the dir into a fresh dir, according to the line confidence
//
// actually, *should* be able to extract the images quite straightforwardly straight from go, which would be cool. so try to build that.
// should be super easy, with SubImage, see end of https://blog.golang.org/go-image-package

import (
	"encoding/xml"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type LineDetail struct {
	name string
	avgconf float64
	img image.Image
	text string
	hocrname string
}

type LineDetails []LineDetail

// Used by sort.Sort.
func (l LineDetails) Len() int { return len(l) }

// Used by sort.Sort.
func (l LineDetails) Less(i, j int) bool {
	return l[i].avgconf < l[j].avgconf
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

type Hocr struct {
	Lines []OcrLine `xml:"body>div>div>p>span"`
}

type OcrLine struct {
	Class string `xml:"class,attr"`
	Id string `xml:"id,attr"`
	Title string `xml:"title,attr"`
	Words []OcrWord `xml:"span"`
	Text string `xml:",chardata"`
}

type OcrWord struct {
	Class string `xml:"class,attr"`
	Id string `xml:"id,attr"`
	Title string `xml:"title,attr"`
	// TODO: also capture OcrChar where it exists, to grab text from it
	// TODO: grab text from these elements, to save for the line
}

// Returns the confidence for a word based on its x_wconf value
func wordConf(s string) (float64, error) {
	re, err := regexp.Compile(`x_wconf ([0-9.]+)`)
	if err != nil {
		return 0.0, err
	}
	conf := re.FindStringSubmatch(s)
	return strconv.ParseFloat(conf[1], 64)
}

func boxCoords(s string) ([4]int, error) {
	var coords [4]int
	re, err := regexp.Compile(`bbox ([0-9]+) ([0-9]+) ([0-9]+) ([0-9]+)`)
	if err != nil {
		return coords, err
	}
	coordstr := re.FindStringSubmatch(s)
	for i := range coords {
		c, err := strconv.Atoi(coordstr[i+1])
		if err != nil {
			return coords, err
		}
		coords[i] = c
	}
	return coords, nil
	
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: line-conf-buckets hocr1 [hocr2] [...]\n")
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

	var hocr Hocr

	for _, f := range flag.Args() {
		file, err := ioutil.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}

		err = xml.Unmarshal(file, &hocr)
		if err != nil {
			log.Fatal(err)
		}

		pngfn := strings.Replace(f, ".hocr", ".png", 1)
		pngf, err := os.Open(pngfn)
		if err != nil {
			log.Fatal(err)
		}
		defer pngf.Close()
		img, err := png.Decode(pngf)
		if err != nil {
			log.Fatal(err)
		}

		for _, l := range hocr.Lines {
			totalconf := float64(0)
			num := 0
			for _, w := range l.Words {
				c, err := wordConf(w.Title)
				if err != nil {
					log.Fatal(err)
				}
				num++
				totalconf += c
			}

			coords, err := boxCoords(l.Title)
			if err != nil {
				log.Fatal(err)
			}

			var line LineDetail
			line.name = l.Id
			line.avgconf = totalconf/float64(num)
			line.text = l.Text // TODO: get text from OcrWord and OcrChar (if available)
			line.hocrname = strings.Replace(filepath.Base(f), ".hocr", "", 1)
			line.img = img.(*image.Gray).SubImage(image.Rect(coords[0], coords[1], coords[2], coords[3]))
			lines = append(lines, line)
		}
	}

	sort.Sort(lines)

	worstnum := 0
	mediumnum := 0
	bestnum := 0

	outdir := "buckets" // TODO: set this from cmdline
	todir := ""

	for _, l := range lines {
		switch {
		case l.avgconf < 0.95: 
			todir = "bad"
			worstnum++
		case l.avgconf < 0.98:
			todir = "95to98"
			mediumnum++
		default:
			todir = "98plus"
			bestnum++
		}

		avgstr := strconv.FormatFloat(l.avgconf, 'G', -1, 64)
		avgstr = strings.Replace(avgstr, ".", "", 1)
		fmt.Printf("Line: %s, avg: %f, avgstr: %s\n", l.name, l.avgconf, avgstr)
		outname := filepath.Join(outdir, todir, l.hocrname + "_" + l.name + "_" + avgstr + ".png")

		err := os.MkdirAll(filepath.Join(outdir, todir), 0700)
		if err != nil {
			log.Fatal(err)
		}
	
		outfile, err := os.Create(outname)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s\n", outname)
			log.Fatal(err)
		}
		defer outfile.Close()

		err = png.Encode(outfile, l.img)
		if err != nil {
			log.Fatal(err)
		}
		// TODO: do same with saving line

		// TODO: copy the line.img and line.text into the appropriate place, using hocrname/name.ext
		// TODO: test whether the line.img works properly with multiple hocrs, as it could be that as it's a pointer, it always points to the latest image (don't think so, but not sure)
	}

	total := worstnum + mediumnum + bestnum

	fmt.Printf("Copied lines to %s\n", outdir)
	fmt.Printf("---------------------------------\n")
	fmt.Printf("Lines 98%%+ quality:     %d%%\n", 100 * bestnum / total)
	fmt.Printf("Lines 95-98%% quality:   %d%%\n", 100 * mediumnum / total)
	fmt.Printf("Lines <95%% quality:     %d%%\n", 100 * worstnum / total)
}
