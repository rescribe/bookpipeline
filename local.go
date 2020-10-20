// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package bookpipeline

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const qidPre = "queuePre"
const qidWipe = "queueWipe"
const qidOCR = "queueOCR"
const qidAnalyse = "queueAnalyse"
const storageId = "storage"

// LocalConn is a simple implementation of the pipeliner interface
// that doesn't rely on any "cloud" services, instead doing everything
// on the local machine. This is particularly useful for testing.
type LocalConn struct {
	// these should be set before running Init(), or left to defaults
	TempDir string
	Logger *log.Logger
}

// MinimalInit does the bare minimum initialisation
func (a *LocalConn) MinimalInit() error {
	var err error
	if a.TempDir == "" {
		a.TempDir = filepath.Join(os.TempDir(), "bookpipeline")
	}
	err = os.Mkdir(a.TempDir, 0700)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("Error creating temporary directory: %v", err)
	}

	err = os.Mkdir(filepath.Join(a.TempDir, storageId), 0700)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("Error creating storage directory: %v", err)
	}

	if a.Logger == nil {
		a.Logger = log.New(os.Stdout, "", 0)
	}

	return nil
}

// Init just does the same as MinimalInit
func (a *LocalConn) Init() error {
	err := a.MinimalInit()
	if err != nil {
		return err
	}

	return nil
}

// CheckQueue checks for any messages in a queue
func (a *LocalConn) CheckQueue(url string, timeout int64) (Qmsg, error) {
	f, err := os.Open(filepath.Join(a.TempDir, url))
	if err != nil {
		f, err = os.Create(filepath.Join(a.TempDir, url))
	}
	if err != nil {
		return Qmsg{}, err
	}
	if err != nil {
		return Qmsg{}, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	s, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return Qmsg{}, err
	}
	s = strings.TrimRight(s, "\n")

	return Qmsg{Body: s, Handle: s}, nil
}

// QueueHeartbeat is a no-op with LocalConn
func (a *LocalConn) QueueHeartbeat(msg Qmsg, qurl string, duration int64) (Qmsg, error) {
	return Qmsg{}, nil
}

// GetQueueDetails gets the number of in progress and available
// messages for a queue. These are returned as strings.
func (a *LocalConn) GetQueueDetails(url string) (string, string, error) {
	b, err := ioutil.ReadFile(filepath.Join(a.TempDir, url))
	if err != nil {
		return "", "", err
	}
	s := string(b)
	n := strings.Count(s, "\n")

	return fmt.Sprintf("%d", n), "0", nil
}

func (a *LocalConn) PreQueueId() string {
	return qidPre
}

func (a *LocalConn) WipeQueueId() string {
	return qidWipe
}

func (a *LocalConn) OCRPageQueueId() string {
	return qidOCR
}

func (a *LocalConn) AnalyseQueueId() string {
	return qidAnalyse
}

func (a *LocalConn) WIPStorageId() string {
	return storageId
}

func prefixwalker(dirpath string, prefix string, list *[]ObjMeta) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		//n := filepath.Base(path)
		n := strings.TrimPrefix(path, dirpath)
		o := ObjMeta{Name: n, Date: info.ModTime()}
		*list = append(*list, o)
		return nil
	}
}

func (a *LocalConn) ListObjects(bucket string, prefix string) ([]string, error) {
	var names []string
	list, err := a.ListObjectsWithMeta(bucket, prefix)
	if err != nil {
		return names, err
	}
	for _, v := range list {
		names = append(names, v.Name)
	}
	return names, nil
}

func (a *LocalConn) ListObjectsWithMeta(bucket string, prefix string) ([]ObjMeta, error) {
	var list []ObjMeta
	err := filepath.Walk(filepath.Join(a.TempDir, bucket), prefixwalker(filepath.Join(a.TempDir, bucket), prefix, &list))
	return list, err
}

// AddToQueue adds a message to a queue
func (a *LocalConn) AddToQueue(url string, msg string) error {
	f, err := os.OpenFile(filepath.Join(a.TempDir, url), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(msg + "\n")
	return err
}

// DelFromQueue deletes a message from a queue
func (a *LocalConn) DelFromQueue(url string, handle string) error {
	b, err := ioutil.ReadFile(filepath.Join(a.TempDir, url))
	if err != nil {
		return err
	}
	s := string(b)

	i := strings.Index(s, handle)
	if i == -1 {
		return fmt.Errorf("Warning: %s not found in queue %s, so not deleted", handle, url)
	}

	// store the joining of part before and part after handle
	var complete string
	if len(s) >= len(handle) + 1 {
		if i > 0 {
			complete = s[:i]
		}
		// the '+1' is for the newline character
		complete += s[i + len(handle) + 1:]
	}

	f, err := os.Create(filepath.Join(a.TempDir, url))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(complete)
	return err
}

// Download just copies the file from TempDir/bucket/key to path
func (a *LocalConn) Download(bucket string, key string, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fin, err := os.Open(filepath.Join(a.TempDir, bucket, key))
	if err != nil {
		return err
	}
	defer fin.Close()
	_, err = io.Copy(f, fin)
	return err
}

// Upload just copies the file from path to TempDir/bucket/key
func (a *LocalConn) Upload(bucket string, key string, path string) error {
	d := filepath.Join(a.TempDir, bucket, filepath.Dir(key))
	err := os.Mkdir(d, 0700)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("Error creating temporary directory: %v", err)
	}
	f, err := os.Create(filepath.Join(a.TempDir, bucket, key))
	if err != nil {
		return err
	}
	defer f.Close()

	fin, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fin.Close()
	_, err = io.Copy(f, fin)
	return err
}

func (a *LocalConn) GetLogger() *log.Logger {
	return a.Logger
}

// Log records an item in the with the Logger. Arguments are handled
// as with fmt.Println.
func (a *LocalConn) Log(v ...interface{}) {
	a.Logger.Println(v...)
}
