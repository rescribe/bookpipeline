// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package pipeline

import (
	"errors"
	"log"
	"os"
	"rescribe.xyz/bookpipeline"
	"testing"
)

func Test_CheckImages(t *testing.T) {
	cases := []struct {
		dir string
		err error
	}{
		{"testdata/good", nil},
		{"testdata/bad", errors.New("Decoding image testdata/bad/bad.png failed: png: invalid format: invalid checksum")},
		{"testdata/notreadable", errors.New("Opening image testdata/notreadable/1.png failed: open testdata/notreadable/1.png: permission denied")},
	}

	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			if c.dir == "testdata/notreadable" {
				err := os.Chmod("testdata/notreadable/1.png", 0000)
				if err != nil {
					t.Fatalf("Error preparing test by setting file to be unreadable: %v", err)
				}
			}

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

			if c.dir == "testdata/notreadable" {
				err := os.Chmod("testdata/notreadable/1.png", 0644)
				if err != nil {
					t.Fatalf("Error resetting test by setting file to be readable: %v", err)
				}
			}
		})
	}
}

func Test_DetectQueueType(t *testing.T) {
	conn := &bookpipeline.LocalConn{}

	cases := []struct {
		dir string
		qid string
	}{
		{"testdata/mostlypng", conn.WipeQueueId()},
		{"testdata/mostlyjpg", conn.PreQueueId()},
		{"testdata/equalpngandjpg", conn.PreQueueId()},
		{"testdata/nonexistent", conn.PreQueueId()},
	}

	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			qid := DetectQueueType(c.dir, conn)
			if qid != c.qid {
				t.Fatalf("Error, expected qid %v, got qid %v", qid, c.qid)
			}
		})
	}
}

func Test_UploadImages(t *testing.T) {
	var slog StrLog
        vlog := log.New(&slog, "", 0)
	var conns []connection

	conns = append(conns, connection{name: "local", c: &bookpipeline.LocalConn{Logger: vlog}})

	if !testing.Short() {
		conns = append(conns, connection{name: "aws", c: &bookpipeline.AwsConn{Logger: vlog}})
	}

	for _, conn := range conns {
		t.Run(conn.name, func(t *testing.T) {
			err := conn.c.Init()
			if err != nil {
				t.Fatalf("Could not initialise %s connection: %v\nLog: %s", conn.name, err, slog.log)
			}
			err = conn.c.TestInit()
			if err != nil {
				t.Fatalf("Failed in test initialisalisation for %s: %v\nLog: %s", conn.name, err, slog.log)
			}
			slog.log = ""

			err = UploadImages("testdata/good", "good", conn.c, conn.c.TestStorageId())
			if err != nil {
				t.Fatalf("Error in UploadImages for %s: %v\nLog: %s", conn.name, err, slog.log)
			}

			// TODO: decide on whether to use TestStorageId or just the WIPStorageId as with other tests, and align other tests to this if needed
			// TODO: download all files and test that they match
			// TODO: remove test files from conn storage
		})
	}
}
