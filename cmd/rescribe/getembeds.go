// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// +build ignore

// this downloads the needed files to embed into the binary,
// and is run by `go generate`
package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"net/http"
)

func dl(url string) error {
	fn := path.Base(url)

	f, err := os.Create(fn)
	if err != nil {
		return fmt.Errorf("Error creating file %s: %v", fn, err)
	}
	defer f.Close()

	r, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("Error getting url %s: %v", url, err)
	}
	defer r.Body.Close()

	_, err = io.Copy(f, r.Body)
	if err != nil {
		return fmt.Errorf("Error saving %s: %v", fn, err)
	}

	return nil
}

func main() {
	urls := []string {
		"https://rescribe.xyz/rescribe/embeds/tessdata.20210622.zip",
		"https://rescribe.xyz/rescribe/embeds/tesseract-linux-v5.0.0-alpha.20210510.zip",
		"https://rescribe.xyz/rescribe/embeds/tesseract-w32-v5.0.0-alpha.20210506.zip",
	}
	for _, v := range urls {
		fmt.Printf("Downloading %s\n", v)
		err := dl(v)
		if err != nil {
			fmt.Printf("Error downloading %s: %v\n", v, err)
			os.Exit(1)
		}
	}
}
