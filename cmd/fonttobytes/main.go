// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"compress/zlib"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
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

	font, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalln(err)
	}

	// compress with zlib
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	w.Write(font)
	w.Close()

	// this could be done more simply with %+v, but that takes up
	// significantly more space due to printing each byte in hex
	// rather than dec format.

	fmt.Printf("[]byte{")
	for i, b := range buf.Bytes() {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%d", b)
	}
	fmt.Printf("}\n")
}
