package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// splitByPb is a split function for the scanner that splits by the
// '<pb' token.
func splitByPb(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := strings.Index(string(data[:]), "<pb"); i >= 0 {
		return i + 1, data[0:i], nil
	}
	// If we're at EOF, we have a final section, so just return the lot.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}

type Page struct {
	number int
	text   string
}

func addPage(pgs *[]Page, number int, text string) {
	added := 0
	for i, pg := range *pgs {
		if pg.number == number {
			(*pgs)[i].text = pg.text + text
			added = 1
		}
	}
	if added == 0 {
		newpg := Page{number, text}
		*pgs = append(*pgs, newpg)
	}	
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: eeboxmltohocr in.xml outbase\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	f, err := os.Open(flag.Arg(0))
	defer f.Close()
	if err != nil {
		log.Fatalf("Could not open file %s: %v\n", flag.Arg(0), err)
	}
	scanner := bufio.NewScanner(f)

	scanner.Split(splitByPb)

	var pgs []Page

	for scanner.Scan() {
		t := scanner.Text()
		r := regexp.MustCompile(`pb [^>]*facs="tcp:.*?:(.*?)"`).FindStringSubmatch(t)
		if len(r) <= 1 {
			continue
		}
		pgnum, err := strconv.Atoi(r[1])
		if err != nil {
			continue
		}

		content := t[strings.Index(t, ">")+1:]
		ungap := regexp.MustCompile(`(?s)<gap[ >].+?</gap>`).ReplaceAllString(content, "")
		unxml := regexp.MustCompile(`<.+?>`).ReplaceAllString(ungap, "")

		finaltxt := strings.TrimLeft(unxml, " \n")
		if len(finaltxt) == 0 {
			continue
		}

		addPage(&pgs, pgnum, finaltxt)
	}

	for _, pg := range pgs {
		fn := fmt.Sprintf("%s-%03d.hocr", flag.Arg(1), pg.number - 1)
		f, err := os.Create(fn)
		if err != nil {
			log.Fatalf("Could not create file %s: %v\n", fn, err)
		}
		defer f.Close()

		_, err = io.WriteString(f, hocrHeader + pg.text + hocrFooter)
		if err != nil {
			log.Fatalf("Could not write file %s: %v\n", fn, err)
		}
	}
}

const hocrHeader = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN"
    "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="en" lang="en">
 <head>
  <title></title>
  <meta http-equiv="Content-Type" content="text/html;charset=utf-8"/>
  <meta name='ocr-system' content='tesseract 4.0.0' />
  <meta name='ocr-capabilities' content='ocr_page ocr_carea ocr_par ocr_line ocrx_word ocrp_wconf'/>
 </head>
 <body>
  <div class='ocr_page' id='page_1' title='bbox 0 0 600 1200'>
   <div class='ocr_carea' id='block_1_1' title="bbox 0 0 600 1200">
    <p class='ocr_par' id='par_1_1' lang='lat' title="bbox 0 0 600 1200">
     <span class='ocr_line' id='line_1_1' title="bbox 0 0 600 1200"
>
      <span class='ocrx_word' id='word_1_1' title='bbox 0 0 600 1200'>`

const hocrFooter = `</span>
     </span>
    </p>
   </div>
  </div>
 </body>
</html>`
