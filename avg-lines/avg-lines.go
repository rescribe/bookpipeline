package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"rescribe.xyz/go.git/lib/line"
	"rescribe.xyz/go.git/lib/hocr"
	"rescribe.xyz/go.git/lib/prob"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: avg-lines [-html] [-nosort] [prob1] [hocr1] [prob2] [...]\n")
		fmt.Fprintf(os.Stderr, "Prints a report of the average confidence for each line, sorted\n")
		fmt.Fprintf(os.Stderr, "from worst to best.\n")
		fmt.Fprintf(os.Stderr, "Both .hocr and .prob files can be processed.\n")
		fmt.Fprintf(os.Stderr, "For .hocr files, the x_wconf data is used to calculate confidence.\n")
		fmt.Fprintf(os.Stderr, "The .prob files are generated using ocropy-rpred's --probabilities\n")
		fmt.Fprintf(os.Stderr, "option.\n\n")
		flag.PrintDefaults()
	}
	var usehtml = flag.Bool("html", false, "Output html page")
	var nosort = flag.Bool("nosort", false, "Don't sort lines by confidence")
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	var err error
	lines := make(line.Details, 0)

	for _, f := range flag.Args() {
		var newlines line.Details
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

	if *nosort == false {
		sort.Sort(lines)
	}

	if *usehtml == false {
		for _, l := range lines {
			fmt.Printf("%s %s: %.2f%%\n", l.OcrName, l.Name, l.Avgconf)
		}
	} else {
		fmt.Printf("<!DOCTYPE html><html><head><meta charset='UTF-8'><title></title><style>td {border: 1px solid #444}</style></head><body>\n")
		fmt.Printf("<table>\n")
		for _, l := range lines {
			fmt.Printf("<tr>\n")
			fmt.Printf("<td><h1>%.4f%%</h1></td>\n", l.Avgconf)
			fmt.Printf("<td>%s %s</td>\n", l.OcrName, l.Name)
			// TODO: think about this, what do we want to do here? if showing imgs is important,
			//       will need to copy them somewhere, so works with hocr too
			//fmt.Printf("<td><img src='%s' /><br />%s</td>\n", l.Filebase + ".bin.png", l.Fulltext)
			fmt.Printf("</tr>\n")
		}
		fmt.Printf("</table>\n")
		fmt.Printf("</body></html>\n")
	}
}
