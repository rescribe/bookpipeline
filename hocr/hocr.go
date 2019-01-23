package hocr

// TODO: separate out linedetail to a general structure that can incorporate
//       line-conf-buckets too, in a different file (and rename package to
//       something more generic). Try to use interfaces to fill it with hocr
//       or ocropy data simply. Could design it so LineDetail is an interface,
//       that can do CopyLines() and BucketUp(). Actually only BucketUp() would
//       be needed, as copylines can be internal to that. Then can create more
//       methods as and when needed. BucketUp() can take some form of arguments
//       that can make it flexible, like []struct { dirname string, minconf float64 }
// TODO: Parse line name to zero pad line numbers, so they come out in the correct order

import (
	"encoding/xml"
	"image"
	"regexp"
	"strconv"
	"strings"
)

type LineDetail struct {
	Name string
	Avgconf float64
	Img image.Image
	Text string
	Hocrname string
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
	Chars []OcrChar `xml:"span"`
	Text string `xml:",chardata"`
}

type OcrChar struct {
	Class string `xml:"class,attr"`
	Id string `xml:"id,attr"`
	Title string `xml:"title,attr"`
	Chars []OcrChar `xml:"span"`
	Text string `xml:",chardata"`
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

func noText(s string) bool {
	t := strings.Trim(s, " \n")
	return len(t) == 0
}

func Parse(b []byte) (Hocr, error) {
	var hocr Hocr

	err := xml.Unmarshal(b, &hocr)
	if err != nil {
		return hocr, err
	}

	return hocr, nil
}

func GetLineDetails(h Hocr, i image.Image, name string) (LineDetails, error) {
	lines := make(LineDetails, 0)

	for _, l := range h.Lines {
		totalconf := float64(0)
		num := 0
		for _, w := range l.Words {
			c, err := wordConf(w.Title)
			if err != nil {
				return lines, err
			}
			num++
			totalconf += c
		}

		coords, err := boxCoords(l.Title)
		if err != nil {
			return lines, err
		}

		var line LineDetail
		line.Name = l.Id
		line.Avgconf = (totalconf/float64(num)) / 100
		linetext := ""

		linetext = l.Text
		if(noText(linetext)) {
			linetext = ""
			for _, w := range l.Words {
				if(w.Class != "ocrx_word") {
					continue
				}
				linetext += w.Text + " "
			}
		}
		if(noText(linetext)) {
			linetext = ""
			for _, w := range l.Words {
				if(w.Class != "ocrx_word") {
					continue
				}
				for _, c := range w.Chars {
					if(c.Class != "ocrx_cinfo") {
						continue
					}
					linetext += c.Text
				}
				linetext += " "
			}
		}
		line.Text = strings.TrimRight(linetext, " ")
		line.Text += "\n"
		line.Hocrname = name
		line.Img = i.(*image.Gray).SubImage(image.Rect(coords[0], coords[1], coords[2], coords[3]))
		lines = append(lines, line)
	}
	return lines, nil
}
