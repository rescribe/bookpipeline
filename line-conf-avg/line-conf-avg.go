package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: line-conf-avg prob1 [prob2] [...]\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

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

		fmt.Printf("%s: %.2f%%\n", f, avg)
	}
}
