// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type fileWalk chan string

// Walk sends the path of all files to the channel, with the exception of
// any file which starts with "."
func (f fileWalk) Walk(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	// skip files starting with . to prevent automatically generated
	// files like .DS_Store getting in the way
	if strings.HasPrefix(filepath.Base(path), ".") {
		return nil
	}
	if !info.IsDir() {
		f <- path
	}
	return nil
}

// CheckImages checks that all files with a ".jpg" or ".png" suffix
// in a directory are images that can be decoded (skipping dotfiles)
func CheckImages(ctx context.Context, dir string) error {
	checker := make(fileWalk)
	go func() {
		_ = filepath.Walk(dir, checker.Walk)
		close(checker)
	}()

	n := 0
	for path := range checker {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		suffix := filepath.Ext(path)
		lsuffix := strings.ToLower(suffix)
		if lsuffix != ".jpg" && lsuffix != ".png" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("Opening image %s failed: %v", path, err)
		}
		_, _, err = image.Decode(f)
		if err != nil {
			return fmt.Errorf("Decoding image %s failed: %v", path, err)
		}
		n++
	}

	if n == 0 {
		return fmt.Errorf("No images found")
	}

	return nil
}

// DetectQueueType detects which queue to use based on the preponderance
// of files of a particular extension in a directory
func DetectQueueType(dir string, conn Queuer) string {
	pngdirs, _ := filepath.Glob(dir + "/*.png")
	jpgdirs, _ := filepath.Glob(dir + "/*.jpg")
	pngcount := len(pngdirs)
	jpgcount := len(jpgdirs)
	if pngcount > jpgcount {
		return conn.WipeQueueId()
	} else {
		return conn.PreQueueId()
	}
}

// UploadImages uploads all files with a suffix of ".jpg" or ".png"
// (except those which start with a ".") from a directory (recursively)
// into conn.WIPStorageId(), prefixed with the given bookname and a
// slash. It also appends all file names with sequential numbers, like
// 0001, to ensure they are appropriately named for further processing
// in the pipeline.
func UploadImages(ctx context.Context, dir string, bookname string, conn Uploader) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Errorf("Failed to read directory %s: %v", dir, err)
	}

	filenum := 0
	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if file.IsDir() {
			continue
		}
		origsuffix := filepath.Ext(file.Name())
		lsuffix := strings.ToLower(origsuffix)
		if lsuffix != ".jpg" && lsuffix != ".png" {
			continue
		}
		origname := file.Name()
		origbase := strings.TrimSuffix(origname, origsuffix)
		origpath := filepath.Join(dir, origname)

		newname := fmt.Sprintf("%s_%04d%s", origbase, filenum, origsuffix)
		err = conn.Upload(conn.WIPStorageId(), filepath.Join(bookname, newname), origpath)
		if err != nil {
			return fmt.Errorf("Failed to upload %s: %v", origpath, err)
		}

		filenum++
	}

	return nil
}
