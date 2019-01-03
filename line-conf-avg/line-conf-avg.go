package main

import (
	"bufio"
	"flag"
	"fmt"
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

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: line-conf-avg [-html] [-nosort] prob1 [prob2] [...]\n")
		fmt.Fprintf(os.Stderr, "Prints a report of the average confidence for each line\n")
		flag.PrintDefaults()
	}
	var usehtml = flag.Bool("html", false, "output html page")
	var nosort = flag.Bool("nosort", false, "don't sort lines by confidence")
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

	if *nosort == false {
		sort.Sort(lines)
	}

	if *usehtml == false {
		for _, l := range lines {
			fmt.Printf("%s: %.2f%%\n", l.Filename, l.Avgconf)
		}
	} else {
		fmt.Printf("<!DOCTYPE html><html><head><meta charset='UTF-8'><title></title><style>td {border: 1px solid #444}</style></head><body>\n")
		fmt.Printf("<table>\n")
		for _, l := range lines {
			fmt.Printf("<tr>\n")
			fmt.Printf("<td><h1>%.4f%%</h1></td>\n", l.Avgconf)
			fmt.Printf("<td>%s</td>\n", l.Filebase)
			fmt.Printf("<td><img src='%s' /><br />%s</td>\n", l.Filebase + ".bin.png", l.Fulltext)
			fmt.Printf("</tr>\n")
		}
		fmt.Printf("</table>\n")
		fmt.Printf("</body></html>\n")
	}
}
