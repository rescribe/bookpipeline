// Copyright 2022 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"testing"
)

func TestGetBookIdFromUrl(t *testing.T) {
	cases := []struct {
		url string
		id  string
	}{
		{"https://books.google.it/books?id=QjQepCuN8JYC", "QjQepCuN8JYC"},
		{"https://www.google.it/books/edition/_/VJbr-Oe2au0C", "VJbr-Oe2au0C"},
	}

	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			id, err := getBookIdFromUrl(c.url)
			if err != nil {
				t.Fatalf("Error running test: %v", err)
			}
			if id != c.id {
				t.Fatalf("Expected %s, got %s", c.id, id)
			}
		})
	}
}

func FuzzGetBookIdFromUrl(f *testing.F) {
	cases := []string {
		"https://books.google.it/books?id=QjQepCuN8JYC",
		"https://www.google.it/books/edition/_/VJbr-Oe2au0C",
	}

	for _, c := range cases {
		f.Add(c)
	}

	f.Fuzz(func(t *testing.T, url string) {
		getBookIdFromUrl(url)
	})
}
