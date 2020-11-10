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

func DownloadBestPages(name string, conn Pipeliner, pluspngs bool) error {
	fn := filepath.Join(name, "best")
	err := conn.Download(conn.WIPStorageId(), fn, fn)
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
		fn = filepath.Join(name, s.Text())
		conn.Log("Downloading file", fn)
		err = conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			return fmt.Errorf("Failed to download file %s: %v", fn, err)
		}
	}

	if !pluspngs {
		return nil
	}

	s = bufio.NewScanner(f)
	for s.Scan() {
		txtfn := filepath.Join(name, s.Text())
		fn = strings.Replace(txtfn, ".hocr", ".png", 1)
		conn.Log("Downloading file", fn)
		err = conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			return fmt.Errorf("Failed to download file", fn, err)
		}
	}
	return nil
}

func DownloadPdfs(name string, conn Pipeliner) error {
	for _, suffix := range []string{".colour.pdf", ".binarised.pdf"} {
		fn := filepath.Join(name, name+suffix)
		err := conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			return fmt.Errorf("Failed to download PDF %s: %v", fn, err)
		}
	}
	return nil
}

func DownloadAnalyses(name string, conn Pipeliner) error {
	for _, a := range []string{"conf", "graph.png"} {
		fn := filepath.Join(name, a)
		err := conn.Download(conn.WIPStorageId(), fn, fn)
		if err != nil {
			return fmt.Errorf("Failed to download analysis file %s: %v", fn, err)
		}
	}
	return nil
}

func DownloadAll(name string, conn Pipeliner) error {
	objs, err := conn.ListObjects(conn.WIPStorageId(), name)
	if err != nil {
		return fmt.Errorf("Failed to get list of files for book", name, err)
	}
	for _, i := range objs {
		conn.Log("Downloading", i)
		err = conn.Download(conn.WIPStorageId(), i, i)
		if err != nil {
			return fmt.Errorf("Failed to download file", i, err)
		}
	}
	return nil
}
