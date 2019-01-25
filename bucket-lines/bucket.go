package main

import (
	"fmt"
	"io"
	"path/filepath"
	"os"
	"sort"
	"strconv"

	"git.rescribe.xyz/testingtools/lib/line"
)

type BucketSpec struct {
	Min float64
	Name string
}
type BucketSpecs []BucketSpec
func (b BucketSpecs) Len() int { return len(b) }
func (b BucketSpecs) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b BucketSpecs) Less(i, j int) bool { return b[i].Min < b[j].Min }

type BucketStat struct {
	name string
	num int
}
type BucketStats []BucketStat
func (b BucketStats) Len() int { return len(b) }
func (b BucketStats) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b BucketStats) Less(i, j int) bool { return b[i].num < b[j].num }

// Copies the image and text for a line into a directory based on
// the line confidence, as defined by the buckets struct
func bucketLine(l line.Detail, buckets BucketSpecs, dirname string) (string, error) {
	var bucket string

	todir := ""
	for _, b := range buckets {
		if l.Avgconf >= b.Min {
			todir = b.Name
			bucket = b.Name
		}
	}

	if todir == "" {
		return bucket, nil
	}

	avgstr := strconv.FormatFloat(l.Avgconf, 'G', -1, 64)
	if len(avgstr) > 2 {
		avgstr = avgstr[2:]
	}

	base := filepath.Join(dirname, todir, filepath.Base(l.OcrName) + "_" + l.Name + "_" + avgstr)

	err := os.MkdirAll(filepath.Join(dirname, todir), 0700)
	if err != nil {
		return bucket, err
	}

	f, err := os.Create(base + ".png")
	if err != nil {
		return bucket, err
	}
	defer f.Close()

	err = l.Img.CopyLineTo(f)
	if err != nil {
		return bucket, err
	}

	f, err = os.Create(base + ".txt")
	if err != nil {
		return bucket, err
	}
	defer f.Close()

	_, err = io.WriteString(f, l.Text)
	if err != nil {
		return bucket, err
	}

	return bucket, err
}

// Copies line images and text into directories based on their
// confidence, as defined by the buckets struct, and returns
// statistics of whire lines went in the process.
func BucketUp(lines line.Details, buckets BucketSpecs, dirname string) (BucketStats, error) {
	var all []string
	var stats BucketStats

	sort.Sort(lines)
	sort.Sort(buckets)
	for _, l := range lines {
		bname, err := bucketLine(l, buckets, dirname)
		if err != nil {
			return stats, err
		}
		all = append(all, bname)
	}

	for _, b := range all {
		i := sort.Search(len(stats), func(i int) bool { return stats[i].name == b })
		if i == len(stats) {
			newstat := BucketStat { b, 0 }
			stats = append(stats, newstat)
			i = len(stats) - 1
		}
		stats[i].num++
	}

	return stats, nil
}

// Prints statistics of where lines went when bucketing
func PrintBucketStats(w io.Writer, stats BucketStats) {
	var total int
	for _, s := range stats {
		total += s.num
	}

	fmt.Fprintf(w, "Copied %d lines\n", total)
	fmt.Fprintf(w, "---------------------------------\n")
	sort.Sort(stats)
	for _, s := range stats {
		fmt.Fprintf(w, "Lines in %7s: %2d%%\n", s.name, 100 * s.num / total)
	}
}
