// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package pipeline

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
)

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type fileWalk chan string

func (f fileWalk) Walk(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if !info.IsDir() {
		f <- path
	}
	return nil
}

func CheckImages(dir string) error {
	checker := make(fileWalk)
	go func() {
		_ = filepath.Walk(dir, checker.Walk)
		close(checker)
	}()

	for path := range checker {
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("Opening image %s failed: %v", path, err)
		}
		_, _, err = image.Decode(f)
		if err != nil {
			return fmt.Errorf("Decoding image %s failed: %v", path, err)
		}
	}

	return nil
}

func DetectQueueType(dir string, conn Pipeliner) string {
	// Auto detect type of queue to send to based on file extension
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

func UploadImages(dir string, bookname string, conn Pipeliner) error {
	walker := make(fileWalk)
	go func() {
		_ = filepath.Walk(dir, walker.Walk)
		close(walker)
	}()

	for path := range walker {
		name := filepath.Base(path)
		err := conn.Upload(conn.WIPStorageId(), filepath.Join(bookname, name), path)
		if err != nil {
			return fmt.Errorf("Failed to upload %s: %v", path, err)
		}
	}

	return nil
}