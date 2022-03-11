// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package pipeline

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DownloadBestPages(dir string, name string, conn Downloader) error {
	key := filepath.Join(name, "best")
	fn := filepath.Join(dir, "best")
	err := conn.Download(conn.WIPStorageId(), key, fn)
	if err != nil {
		return fmt.Errorf("Failed to download 'best' file: %v", err)
	}
	f, err := os.Open(fn)
	if err != nil {
		return fmt.Errorf("Failed to open best file: %v", err)
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		key = filepath.Join(name, s.Text())
		fn = filepath.Join(dir, s.Text())
		conn.Log("Downloading file", key)
		err = conn.Download(conn.WIPStorageId(), key, fn)
		if err != nil {
			return fmt.Errorf("Failed to download file %s: %v", key, err)
		}
	}
	return nil
}

func DownloadBestPngs(dir string, name string, conn Downloader) error {
	key := filepath.Join(name, "best")
	fn := filepath.Join(dir, "best")
	err := conn.Download(conn.WIPStorageId(), key, fn)
	if err != nil {
		return fmt.Errorf("Failed to download 'best' file: %v", err)
	}
	f, err := os.Open(fn)
	if err != nil {
		return fmt.Errorf("Failed to open best file: %v", err)
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		imgname := strings.Replace(s.Text(), ".hocr", ".png", 1)
		key = filepath.Join(name, imgname)
		fn = filepath.Join(dir, imgname)
		conn.Log("Downloading file", key)
		err = conn.Download(conn.WIPStorageId(), key, fn)
		if err != nil {
			return fmt.Errorf("Failed to download file %s: %v", key, err)
		}
	}
	return nil
}

func DownloadPdfs(dir string, name string, conn Downloader) error {
	anydone := false
	errmsg := ""
	for _, suffix := range []string{".colour.pdf", ".binarised.pdf", ".original.pdf"} {
		key := filepath.Join(name, name+suffix)
		fn := filepath.Join(dir, name+suffix)
		err := conn.Download(conn.WIPStorageId(), key, fn)
		if err != nil {
			_ = os.Remove(fn)
			errmsg += fmt.Sprintf("Failed to download PDF %s: %v\n", key, err)
		} else {
			anydone = true
		}
	}
	if anydone == false {
		return fmt.Errorf("No PDFs could be downloaded, error(s): %v", errmsg)
	}
	return nil
}

func DownloadAnalyses(dir string, name string, conn Downloader) error {
	for _, a := range []string{"conf", "graph.png"} {
		key := filepath.Join(name, a)
		fn := filepath.Join(dir, a)
		err := conn.Download(conn.WIPStorageId(), key, fn)
		// ignore errors with graph.png, as it will not exist in the case of a 1 page book
		if err != nil && a != "graph.png" {
			return fmt.Errorf("Failed to download analysis file %s: %v", key, err)
		}
	}
	return nil
}

func DownloadAll(dir string, name string, conn DownloadLister) error {
	objs, err := conn.ListObjects(conn.WIPStorageId(), name)
	if err != nil {
		return fmt.Errorf("Failed to get list of files for book %s: %v", name, err)
	}
	for _, i := range objs {
		base := filepath.Base(i)
		fn := filepath.Join(dir, base)
		conn.Log("Downloading", i)
		err = conn.Download(conn.WIPStorageId(), i, fn)
		if err != nil {
			return fmt.Errorf("Failed to download file %s: %v", i, err)
		}
	}
	return nil
}
