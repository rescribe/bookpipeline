// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package pipeline

import (
	"errors"
	"testing"
)

func Test_CheckImages(t *testing.T) {
	cases := []struct {
		dir string
		err error
	}{
		{"testdata/good", nil},
		{"testdata/bad", errors.New("Decoding image testdata/bad/bad.png failed: png: invalid format: invalid checksum")},
		{"testdata/nonexistent", nil},
	}

	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			err := CheckImages(c.dir)
			if err == nil && c.err != nil {
				t.Fatalf("Expected error '%v', got no error", c.err)
			}
			if err != nil && c.err == nil {
				t.Fatalf("Expected no error, got error '%v'", err)
			}
			if err != nil && c.err != nil && err.Error() != c.err.Error() {
				t.Fatalf("Got an unexpected error, expected '%v', got '%v'", c.err, err)
			}
		})
	}
}
