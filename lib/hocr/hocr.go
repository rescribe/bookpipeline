package hocr

import (
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
)

type Hocr struct {
	Lines []OcrLine `xml:"body>div>div>p>span"`
}

type OcrLine struct {
	Class string    `xml:"class,attr"`
	Id    string    `xml:"id,attr"`
	Title string    `xml:"title,attr"`
	Words []OcrWord `xml:"span"`
	Text  string    `xml:",chardata"`
}

type OcrWord struct {
	Class string    `xml:"class,attr"`
	Id    string    `xml:"id,attr"`
	Title string    `xml:"title,attr"`
	Chars []OcrChar `xml:"span"`
	Text  string    `xml:",chardata"`
}

type OcrChar struct {
	Class string    `xml:"class,attr"`
	Id    string    `xml:"id,attr"`
	Title string    `xml:"title,attr"`
	Chars []OcrChar `xml:"span"`
	Text  string    `xml:",chardata"`
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
