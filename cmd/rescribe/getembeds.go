// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// +build ignore

// this downloads the needed files to embed into the binary,
// and is run by `go generate`
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
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
	if r.StatusCode != 200 {
		return fmt.Errorf("Error getting url %s: got code %v", url, r.StatusCode)
	}

	_, err = io.Copy(f, r.Body)
	if err != nil {
		return fmt.Errorf("Error saving %s: %v", fn, err)
	}

	return nil
}

// present returns true if the file is present and matches the
// checksum, false otherwise
func present(url string, sum string) bool {
	fn := path.Base(url)
	_, err := os.Stat(fn)
	if err != nil && !os.IsExist(err) {
		return false
	}

	b, err := ioutil.ReadFile(fn)
	if err != nil {
		return false
	}

	expected, err := hex.DecodeString(sum)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding checksum for %s: %v\n", url, err)
		os.Exit(1)
	}

	actual := sha256.Sum256(b)

	var a []byte
	for _, v := range actual {
		a = append(a, v)
	}

	if !bytes.Equal(a, expected) {
		return false
	}

	return true
}

func main() {
	urls := []struct{
		url string
		sum string
	}{
		{"https://rescribe.xyz/rescribe/embeds/tessdata.20211001.zip", "5c90ae69b9e449d85e84b4806a54d6739b572730525010483e512a62a527b030"},
		{"https://rescribe.xyz/rescribe/embeds/tessdata.20220321.zip", "c6dddf99ad719b29fd6bde1a416a51674bd1834d2df8e519313d584e759a8e0e"},
		{"https://rescribe.xyz/rescribe/embeds/tesseract-linux-v5.0.0-alpha.20210510.zip", "81cfba632b8aaf0a00180b1aa62d357d50f343b0e9bd51b941ee14c289ccd889"},
		{"https://rescribe.xyz/rescribe/embeds/tesseract-osx-v4.1.1.20191227.zip", "5f567b95f1dea9d0581ad42ada4d1f1160a38ea22ae338f9efe190015265636b"},
		{"https://rescribe.xyz/rescribe/embeds/tesseract-osx-m1-v4.1.1.20210802.zip", "c9a454633f7e5175e2d50dd939d30a6e5bdfb3b8c78590a08b5aa21edbf32ca4"},
		{"https://rescribe.xyz/rescribe/embeds/tesseract-w32-v5.0.0-alpha.20210506.zip", "96734f3db4bb7c3b9a241ab6d89ab3e8436cea43b1cbbcfb13999497982f63e3"},
		{"https://rescribe.xyz/rescribe/embeds/getgbook-darwin-cac42fb.zip", "b41fd429be53cdce13ecc7991fe6f8913428657ad70a7790cfcc776e56060887"},
		{"https://rescribe.xyz/rescribe/embeds/getgbook-linux-cac42fb.zip", "c3b40a1c13da613d383f990bda5dd72425a7f26b89102d272a3388eb3d05ddb6"},
		{"https://rescribe.xyz/rescribe/embeds/getgbook-w32-c2824685.zip", "1c258a77a47d6515718fbbd7e54d5c2b516291682a878d122add55901c9f2914"},
	}
	for _, v := range urls {
		if present(v.url, v.sum) {
			fmt.Printf("Skipping downloading of already present %s\n", path.Base(v.url))
			continue
		}

		fmt.Printf("Downloading %s\n", v.url)
		err := dl(v.url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading %s: %v\n", v.url, err)
			os.Exit(1)
		}

		if !present(v.url, v.sum) {
			fmt.Fprintf(os.Stderr, "Error: downloaded %s does not match expected checksum\n", v.url)
			os.Exit(1)
		}
	}
}
