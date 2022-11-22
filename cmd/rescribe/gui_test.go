// Copyright 2022 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/widget"
)

func TestFormatProgressBar(t *testing.T) {
	cases := []struct {
		val float64
		str string
	}{
		{0.0, ""},
		{0.01, "Processing"},
		{0.11, "Downloading"},
		{0.12, "Processing PDF"},
		{0.2, "Preprocessing"},
		{0.5, "OCRing"},
		{0.55, "OCRing"},
		{0.89, "OCRing"},
		{0.9, "Analysing"},
		{1.0, "Done"},
		{1.1, "Processing"},
	}

	_ = app.New() // shouldn't be needed for test but we get a panic without it
	bar := widget.NewProgressBar()

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_%.1f", c.str, c.val), func(t *testing.T) {
			bar.Value = c.val
			got := formatProgressBar(bar)()
			if got != c.str {
				t.Fatalf("Expected %s, got %s", c.str, got)
			}
		})
	}
}

func TestUpdateProgress(t *testing.T) {
	cases := []struct {
		log string
		val float64
	}{
		{"Downloading", 0.11},
		{"Preprocessing", 0.2},
		{"Preprocessing\nOCRing", 0.5},
		{"Preprocessing\nOCRing...", 0.53},
		{"OCRing........................................", 0.89},
		{"OCRing..\nAnalysing", 0.9},
		{"Done", 1.0},
		{"Weirdness", 0.0},
	}

	_ = app.New() // shouldn't be needed for test but we get a panic without it
	bar := widget.NewProgressBar()

	for _, c := range cases {
		t.Run(c.log, func(t *testing.T) {
			l := strings.ReplaceAll("  "+c.log, "\n", "\n  ")
			bar.Value = 0.0
			updateProgress(l, bar)
			got := bar.Value
			if got != c.val {
				t.Fatalf("Expected %f, got %f", c.val, got)
			}
		})
	}
}
