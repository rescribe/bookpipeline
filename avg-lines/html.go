package main

import (
	"fmt"
	"os"
	"path/filepath"

	"rescribe.xyz/go.git/lib/line"
)

func copylineimg(fn string, l line.Detail) error {
	f, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer f.Close()

	return l.Img.CopyLineTo(f)
}

func htmlout(dir string, lines line.Details) error {
	err := os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}

	fn := filepath.Join(dir, "index.html")
	f, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "<!DOCTYPE html><html><head><meta charset='UTF-8'><title></title>"+
		"<style>td {border: 1px solid #444}</style></head><body>\n<table>\n")
	if err != nil {
		return err
	}
	for _, l := range lines {
		fn = filepath.Base(l.OcrName) + "_" + l.Name + ".png"
		err = copylineimg(filepath.Join(dir, fn), l)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(f, "<tr>\n"+
			"<td><h1>%.4f%%</h1></td>\n"+
			"<td>%s %s</td>\n"+
			"<td><img src='%s' width='100%%' /><br />%s</td>\n"+
			"</tr>\n",
			l.Avgconf, l.OcrName, l.Name, fn, l.Text)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(f, "</table>\n</body></html>\n")
	if err != nil {
		return err
	}

	return nil
}
