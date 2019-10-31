package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: fonttobytes font.ttf")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		return
	}

	b, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalln(err)
	}
	s := fmt.Sprintf("%v", b)
	s1 := strings.Replace(s, "[", "{", -1)
	s2 := strings.Replace(s1, "]", "}", -1)
	s3 := strings.Replace(s2, " ", ", ", -1)
	fmt.Printf("[]byte%s\n", s3)
}
