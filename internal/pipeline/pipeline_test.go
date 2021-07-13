// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"rescribe.xyz/bookpipeline"
	"strings"
	"testing"
)

// StrLog is a simple logger that saves to a string,
// so it can be printed out only when needed.
type StrLog struct {
	log string
}

func (t *StrLog) Write(p []byte) (n int, err error) {
	t.log += string(p)
	return len(p), nil
}

type PipelineTester interface {
	Pipeliner
	DeleteObjects(bucket string, keys []string) error
	TestQueueId() string
}

type connection struct {
	name string
	c    PipelineTester
}

// Test_download tests the download() function inside the pipeline
func Test_download(t *testing.T) {
	var slog StrLog
	vlog := log.New(&slog, "", 0)

	var conns []connection

	conns = append(conns, connection{name: "local", c: &bookpipeline.LocalConn{Logger: vlog}})

	if !testing.Short() {
		conns = append(conns, connection{name: "aws", c: &bookpipeline.AwsConn{Logger: vlog}})
	}

	cases := []struct {
		dl       string
		contents []byte
		process  string
		errs     []error
	}{
		{"notpresent", []byte(""), "", []error{errors.New("no such file or directory"), errors.New("NoSuchKey: The specified key does not exist")}},
		{"empty", []byte{}, "empty", []error{}},
		{"justastring", []byte("I am just a basic string"), "justastring", []error{}},
	}

	for _, conn := range conns {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s/%s", conn.name, c.dl), func(t *testing.T) {
				err := conn.c.Init()
				if err != nil {
					t.Fatalf("Could not initialise %s connection: %v\nLog: %s", conn.name, err, slog.log)
				}
				slog.log = ""
				tempDir := filepath.Join(os.TempDir(), "pipelinetest")
				err = os.MkdirAll(tempDir, 0700)
				if err != nil && !os.IsExist(err) {
					t.Fatalf("Could not create temporary directory %s: %v\nLog: %s", tempDir, err, slog.log)
				}

				// create and upload test file
				tempFile := filepath.Join(tempDir, "t")
				err = ioutil.WriteFile(tempFile, c.contents, 0600)
				if err != nil {
					t.Fatalf("Could not create temporary file %s: %v\nLog: %s", tempFile, err, slog.log)
				}
				if c.dl != "notpresent" {
					err = conn.c.Upload(conn.c.WIPStorageId(), c.dl, tempFile)
					if err != nil {
						t.Fatalf("Could not upload file %s: %v\nLog: %s", tempFile, err, slog.log)
					}
				}
				err = os.Remove(tempFile)
				if err != nil {
					t.Fatalf("Could not remove temporary upload file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				// download
				dlchan := make(chan string)
				processchan := make(chan string)
				errchan := make(chan error)

				go download(dlchan, processchan, conn.c, tempDir, errchan, vlog)

				dlchan <- c.dl
				close(dlchan)

				// check all is as expected
				select {
				case err = <-errchan:
					if len(c.errs) == 0 {
						t.Fatalf("Received an error when one was not expected, error: %v\nLog: %s", err, slog.log)
					}
					expectedErrFound := 0
					for _, v := range c.errs {
						if strings.Contains(err.Error(), v.Error()) {
							expectedErrFound = 1
						}
					}
					if expectedErrFound == 0 {
						t.Fatalf("Received a different error than was expected, expected one of: %v, got %v\nLog: %s", c.errs, err, slog.log)
					}
				case process := <-processchan:
					expected := tempDir + "/" + c.process
					if expected != process {
						t.Fatalf("Received a different addition to the process channel than was expected, expected: %v, got %v\nLog: %s", expected, process, slog.log)
					}
				}

				if c.dl == "notpresent" {
					return
				}

				tempFile = filepath.Join(tempDir, c.dl)
				dled, err := ioutil.ReadFile(tempFile)
				if err != nil {
					t.Fatalf("Could not read downloaded file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				if !bytes.Equal(dled, c.contents) {
					t.Fatalf("Downloaded file differs from expected, expected: '%s', got '%s'\nLog: %s", c.contents, dled, slog.log)
				}

				// cleanup
				err = conn.c.DeleteObjects(conn.c.WIPStorageId(), []string{c.dl})
				if err != nil {
					t.Fatalf("Could not delete storage object used for test %s: %v\nLog: %s", c.dl, err, slog.log)
				}

				err = os.Remove(tempFile)
				if err != nil {
					t.Fatalf("Could not remove temporary download file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				err = os.RemoveAll(tempDir)
				if err != nil {
					t.Fatalf("Could not remove temporary download directory %s: %v\nLog: %s", tempDir, err, slog.log)
				}
			})
		}
	}
}

// Test_up tests the up() function inside the pipeline
func Test_up(t *testing.T) {
	var slog StrLog
	vlog := log.New(&slog, "", 0)

	var conns []connection

	conns = append(conns, connection{name: "local", c: &bookpipeline.LocalConn{Logger: vlog}})

	if !testing.Short() {
		conns = append(conns, connection{name: "aws", c: &bookpipeline.AwsConn{Logger: vlog}})
	}

	cases := []struct {
		ul       string
		contents []byte
		process  string
		errs     []error
	}{
		{"notpresent", []byte(""), "", []error{errors.New("no such file or directory"), errors.New("NoSuchKey: The specified key does not exist")}},
		{"empty", []byte{}, "empty", []error{}},
		{"justastring", []byte("I am just a basic string"), "justastring", []error{}},
	}

	for _, conn := range conns {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s/%s", conn.name, c.ul), func(t *testing.T) {
				err := conn.c.Init()
				if err != nil {
					t.Fatalf("Could not initialise %s connection: %v\nLog: %s", conn.name, err, slog.log)
				}
				slog.log = ""
				tempDir := filepath.Join(os.TempDir(), "pipelinetest")
				err = os.MkdirAll(tempDir, 0700)
				if err != nil && !os.IsExist(err) {
					t.Fatalf("Could not create temporary directory %s: %v\nLog: %s", tempDir, err, slog.log)
				}

				// create test file
				tempFile := filepath.Join(tempDir, c.ul)
				if c.ul != "notpresent" {
					err = ioutil.WriteFile(tempFile, c.contents, 0600)
					if err != nil {
						t.Fatalf("Could not create temporary file %s: %v\nLog: %s", tempFile, err, slog.log)
					}
				}

				// upload
				ulchan := make(chan string)
				donechan := make(chan bool)
				errchan := make(chan error)

				go up(ulchan, donechan, conn.c, "pipelinetest", errchan, vlog)

				ulchan <- filepath.Join(tempDir, c.ul)
				close(ulchan)

				// check all is as expected
				select {
				case err = <-errchan:
					if len(c.errs) == 0 {
						t.Fatalf("Received an error when one was not expected, error: %v\nLog: %s", err, slog.log)
					}
					expectedErrFound := 0
					for _, v := range c.errs {
						if strings.Contains(err.Error(), v.Error()) {
							expectedErrFound = 1
						}
					}
					if expectedErrFound == 0 {
						t.Fatalf("Received a different error than was expected, expected one of: %v, got %v\nLog: %s", c.errs, err, slog.log)
					}
				case <-donechan:
				}

				if c.ul == "notpresent" {
					return
				}

				_, err = os.Stat(tempFile)
				if os.IsExist(err) {
					t.Fatalf("Uploaded file not removed as it should have been after uploading %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				err = conn.c.Download(conn.c.WIPStorageId(), "pipelinetest/"+c.ul, tempFile)
				if err != nil {
					t.Fatalf("Could not download file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				dled, err := ioutil.ReadFile(tempFile)
				if err != nil {
					t.Fatalf("Could not read downloaded file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				if !bytes.Equal(dled, c.contents) {
					t.Fatalf("Uploaded file differs from expected, expected: '%s', got '%s'\nLog: %s", c.contents, dled, slog.log)
				}

				// cleanup
				err = conn.c.DeleteObjects(conn.c.WIPStorageId(), []string{"pipelinetest/" + c.ul})
				if err != nil {
					t.Fatalf("Could not delete storage object used for test %s: %v\nLog: %s", c.ul, err, slog.log)
				}

				err = os.Remove(tempFile)
				if err != nil {
					t.Fatalf("Could not remove temporary download file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				err = os.RemoveAll(tempDir)
				if err != nil {
					t.Fatalf("Could not remove temporary download directory %s: %v\nLog: %s", tempDir, err, slog.log)
				}
			})
		}
	}
}

// Test_upAndQueue tests the upAndQueue() function inside the pipeline
func Test_upAndQueue(t *testing.T) {
	var slog StrLog
	vlog := log.New(&slog, "", 0)

	var conns []connection

	conns = append(conns, connection{name: "local", c: &bookpipeline.LocalConn{Logger: vlog}})

	if !testing.Short() {
		conns = append(conns, connection{name: "aws", c: &bookpipeline.AwsConn{Logger: vlog}})
	}

	cases := []struct {
		ul       string
		contents []byte
		process  string
		errs     []error
	}{
		{"notpresent", []byte(""), "", []error{errors.New("no such file or directory"), errors.New("NoSuchKey: The specified key does not exist")}},
		{"empty", []byte{}, "empty", []error{}},
		{"justastring", []byte("I am just a basic string"), "justastring", []error{}},
	}

	for _, conn := range conns {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s/%s", conn.name, c.ul), func(t *testing.T) {
				err := conn.c.Init()
				if err != nil {
					t.Fatalf("Could not initialise %s connection: %v\nLog: %s", conn.name, err, slog.log)
				}
				slog.log = ""
				tempDir := filepath.Join(os.TempDir(), "pipelinetest")
				err = os.MkdirAll(tempDir, 0700)
				if err != nil && !os.IsExist(err) {
					t.Fatalf("Could not create temporary directory %s: %v\nLog: %s", tempDir, err, slog.log)
				}

				// create test file
				tempFile := filepath.Join(tempDir, c.ul)
				if c.ul != "notpresent" {
					err = ioutil.WriteFile(tempFile, c.contents, 0600)
					if err != nil {
						t.Fatalf("Could not create temporary file %s: %v\nLog: %s", tempFile, err, slog.log)
					}
				}

				// upload
				ulchan := make(chan string)
				queueurl := conn.c.TestQueueId()
				donechan := make(chan bool)
				errchan := make(chan error)

				go upAndQueue(ulchan, donechan, queueurl, conn.c, "pipelinetest", "test", errchan, vlog)

				ulchan <- filepath.Join(tempDir, c.ul)
				close(ulchan)

				// check all is as expected
				select {
				case err = <-errchan:
					if len(c.errs) == 0 {
						t.Fatalf("Received an error when one was not expected, error: %v\nLog: %s", err, slog.log)
					}
					expectedErrFound := 0
					for _, v := range c.errs {
						if strings.Contains(err.Error(), v.Error()) {
							expectedErrFound = 1
						}
					}
					if expectedErrFound == 0 {
						t.Fatalf("Received a different error than was expected, expected one of: %v, got %v\nLog: %s", c.errs, err, slog.log)
					}
				case <-donechan:
				}

				msg, err := conn.c.CheckQueue(queueurl, 10)
				if err != nil {
					t.Fatalf("Error checking test queue: %v", err)
				}

				if c.ul == "notpresent" {
					if msg.Handle != "" {
						_ = conn.c.DelFromQueue(queueurl, msg.Handle)
						t.Fatalf("Queue was written to even when an error was received: %s", msg.Body)
					}
					// for the "notpresent" case we can skip the following
					// checks for the file being present and cleanup, having
					// already successfully detected that the appropriate error
					// was received by errchan
					return
				}

				_, err = os.Stat(tempFile)
				if os.IsExist(err) {
					t.Fatalf("Uploaded file not removed as it should have been after uploading %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				err = conn.c.Download(conn.c.WIPStorageId(), "pipelinetest/"+c.ul, tempFile)
				if err != nil {
					t.Fatalf("Could not download file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				dled, err := ioutil.ReadFile(tempFile)
				if err != nil {
					t.Fatalf("Could not read downloaded file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				if !bytes.Equal(dled, c.contents) {
					t.Fatalf("Uploaded file differs from expected, expected: '%s', got '%s'\nLog: %s", c.contents, dled, slog.log)
				}

				target, err := filepath.Rel(os.TempDir(), filepath.Join(tempDir, c.ul))
				if err != nil {
					t.Fatalf("Error removing TempDir prefix: %v", err)
				}
				queueExpected := target + " test"
				if msg.Body != queueExpected {
					_ = conn.c.DelFromQueue(queueurl, msg.Handle)
					t.Fatalf("Queue contents not as expected, expected: '%s', got '%s'\nLog: %s", queueExpected, msg.Body, slog.log)
				}

				// cleanup
				err = conn.c.DeleteObjects(conn.c.WIPStorageId(), []string{"pipelinetest/" + c.ul})
				if err != nil {
					t.Fatalf("Could not delete storage object used for test %s: %v\nLog: %s", c.ul, err, slog.log)
				}

				err = conn.c.DelFromQueue(queueurl, msg.Handle)
				if err != nil {
					t.Fatalf("Could not delete test message from queue: %v\nLog: %s", err, slog.log)
				}

				err = os.Remove(tempFile)
				if err != nil {
					t.Fatalf("Could not remove temporary download file %s: %v\nLog: %s", tempFile, err, slog.log)
				}

				err = os.RemoveAll(tempDir)
				if err != nil {
					t.Fatalf("Could not remove temporary download directory %s: %v\nLog: %s", tempDir, err, slog.log)
				}
			})
		}
	}
}
