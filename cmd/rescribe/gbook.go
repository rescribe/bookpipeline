// Copyright 2022 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"unicode"

	"rescribe.xyz/bookpipeline/internal/pipeline"
)

const maxPartLength = 48

// formatAuthors formats a list of authors by just selecting
// the first one listed, and returning the uppercased final
// name.
func formatAuthors(authors []string) string {
	if len(authors) == 0 {
		return ""
	}

	s := authors[0]

	parts := strings.Fields(s)
	if len(parts) > 1 {
		s = parts[len(parts)-1]
	}

	s = strings.ToUpper(s)

	if len(s) > maxPartLength {
		s = s[:maxPartLength]
	}

	s = strings.Map(stripNonLetters, s)

	return s
}

// mapTitle is a function for strings.Map to strip out
// unwanted characters from the title.
func stripNonLetters(r rune) rune {
	if !unicode.IsLetter(r) {
		return -1
	}
	return r
}

// formatTitle formats a title to our preferences, notably
// by stripping spaces and punctuation characters.
func formatTitle(title string) string {
	s := strings.Map(stripNonLetters, title)
	if len(s) > maxPartLength {
		s = s[:maxPartLength]
	}
	return s
}

// getMetadata queries Google Books for metadata we care about
// and returns it formatted as we need it.
func getMetadata(id string) (string, string, string, error) {
	var author, title, year string
	url := fmt.Sprintf("https://www.googleapis.com/books/v1/volumes/%s", id)

	// designed to be unmarshalled by encoding/json's Unmarshal()
	type bookInfo struct {
		VolumeInfo struct {
			Title		 string
			Authors	   []string
			PublishedDate string
		}
	}

	resp, err := http.Get(url)
	if err != nil {
		return author, title, year, fmt.Errorf("Error downloading metadata %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return author, title, year, fmt.Errorf("Error downloading metadata %s: %v", url, err)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return author, title, year, fmt.Errorf("Error reading metadata %s: %v", url, err)
	}

	v := bookInfo{}
	err = json.Unmarshal(b, &v)
	if err != nil {
		return author, title, year, fmt.Errorf("Error parsing metadata %s: %v", url, err)
	}

	author = formatAuthors(v.VolumeInfo.Authors)
	title = formatTitle(v.VolumeInfo.Title)
	year = v.VolumeInfo.PublishedDate

	return author, title, year, nil
}

// moveFile just copies a file to the destination without
// using os.Rename, as that can fail if crossing filesystem
// boundaries
func moveFile(from string, to string) error {
	ffrom, err := os.Open(from)
	if err != nil {
		return err
	}
	defer ffrom.Close()

	fto, err := os.Create(to)
	if err != nil {
		return err
	}
	defer fto.Close()

	_, err = io.Copy(fto, ffrom)
	if err != nil {
		return err
	}

	ffrom.Close()
	err = os.Remove(from)
	if err != nil {
		return err
	}

	return nil
}

// getGoogleBook downloads all images of a book to a directory
// named YEAR_AUTHORSURNAME_Title_bookid inside basedir, returning
// the directory path
func getGoogleBook(ctx context.Context, id string, basedir string) (string, error) {
	author, title, year, err := getMetadata(id)
	if err != nil {
		return "", err
	}

	tmpdir, err := ioutil.TempDir("", "bookpipeline")
	if err != nil {
		return "", fmt.Errorf("Error setting up temporary directory: %v", err)
	}

	// TODO: use embedded version if necessary
	cmd := exec.CommandContext(ctx, "getgbook", id)
	pipeline.HideCmd(cmd)
	cmd.Dir = tmpdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("Error running getgbook %s: %v", id, err)
	}

	select {
	case <-ctx.Done():
		_ = os.Remove(tmpdir)
		return "", ctx.Err()
	default:
	}

	// getgbook downloads into id directory, so move files out of
	// there directly into dir
	tmpdir = path.Join(tmpdir, id)
	f, err := os.Open(tmpdir)
	if err != nil {
		return "", fmt.Errorf("Failed to open %s to move files: %v", tmpdir, err)
	}
	files, err := f.Readdir(0)
	if err != nil {
		return "", fmt.Errorf("Failed to readdir %s to move files: %v", tmpdir, err)
	}

	d := fmt.Sprintf("%s_%s_%s_%s", year, author, title, id)
	dir := path.Join(basedir, d)
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return "", fmt.Errorf("Couldn't create directory %s: %v", dir, err)
	}

	for _, v := range files {
		orig := path.Join(tmpdir, v.Name())
		new := path.Join(dir, v.Name())
		err = moveFile(orig, new)
		if err != nil {
			return dir, fmt.Errorf("Failed to move %s to %s: %v", orig, new, err)
		}
	}

	err = os.Remove(tmpdir)
	if err != nil {
		return dir, fmt.Errorf("Failed to remove temporary directory %s: %v", tmpdir, err)
	}

	return dir, nil
}
