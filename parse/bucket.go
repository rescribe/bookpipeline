package parse

import (
	"io"
	"path/filepath"
	"os"
	"sort"
	"strconv"
)

type BucketSpec struct {
	Min float64
	Name string
}
type BucketSpecs []BucketSpec
func (b BucketSpecs) Len() int { return len(b) }
func (b BucketSpecs) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b BucketSpecs) Less(i, j int) bool { return b[i].Min < b[j].Min }

func bucketLine(l LineDetail, buckets BucketSpecs, dirname string) error {
	todir := ""
	for _, b := range buckets {
		if l.Avgconf >= b.Min {
			todir = b.Name
		}
	}

	if todir == "" {
		return nil
	}

	avgstr := strconv.FormatFloat(l.Avgconf, 'G', -1, 64)
	if len(avgstr) > 2 {
		avgstr = avgstr[2:]
	}

	base := filepath.Join(dirname, todir, filepath.Base(l.OcrName) + "_" + l.Name + "_" + avgstr)

	err := os.MkdirAll(filepath.Join(dirname, todir), 0700)
	if err != nil {
		return err
	}

	f, err := os.Create(base + ".png")
	if err != nil {
		return err
	}
	defer f.Close()

	err = l.Img.CopyLineTo(f)
	if err != nil {
		return err
	}

	f, err = os.Create(base + ".txt")
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.WriteString(f, l.Text)
	if err != nil {
		return err
	}

	return nil
}

// Copies line images and text into directories based on their
// confidence, as defined by the buckets struct
func BucketUp(lines LineDetails, buckets BucketSpecs, dirname string) error {
	sort.Sort(buckets)
	// TODO: record and print out summary of % in each bucket category (see how tools did it)
	for _, l := range lines {
		err := bucketLine(l, buckets, dirname)
		if err != nil {
			return err
		}
	}

	return nil
}
