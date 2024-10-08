// Copyright 2021-2022 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// rescribe is a modification of bookpipeline designed for local-only
// operation, which rolls uploading, processing, and downloading of
// a single book by the pipeline into one command.
package main

//go:generate go run getembeds.go

import (
	"archive/zip"
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/image/tiff"
	"rescribe.xyz/bookpipeline"
	"rescribe.xyz/bookpipeline/internal/pipeline"
	"rescribe.xyz/pdf"
	"rescribe.xyz/utils/pkg/hocr"
)

const usage = `Usage: rescribe [-v] [-gui] [-systess] [-tesscmd cmd] [-gbookcmd cmd] [-t training] bookdir/book.pdf [savedir]

Process and OCR a book using the Rescribe pipeline on a local machine.

OCR results are saved into the bookdir directory unless savedir is
specified.
`

const QueueTimeoutSecs = 2 * 60
const PauseBetweenChecks = 1 * time.Second
const LogSaveTime = 1 * time.Minute

var thresholds = []float64{0.1, 0.2, 0.3}

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type Clouder interface {
	Init() error
	ListObjects(bucket string, prefix string) ([]string, error)
	DeleteObjects(bucket string, keys []string) error
	Download(bucket string, key string, fn string) error
	Upload(bucket string, key string, path string) error
	CheckQueue(url string, timeout int64) (bookpipeline.Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	QueueHeartbeat(msg bookpipeline.Qmsg, qurl string, duration int64) (bookpipeline.Qmsg, error)
}

type Pipeliner interface {
	Clouder
	PreQueueId() string
	PreNoWipeQueueId() string
	WipeQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
	WIPStorageId() string
	GetLogger() *log.Logger
	Log(v ...interface{})
}

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		<-t.C
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if d > 0 {
		t.Reset(d)
	}
}

// unpackZip unpacks a byte array of a zip file into a directory
func unpackZip(b []byte, dir string) error {
	br := bytes.NewReader(b)
	zr, err := zip.NewReader(br, br.Size())
	if err != nil {
		return fmt.Errorf("Error opening zip: %v", err)
	}

	for _, f := range zr.File {
		fn := filepath.Join(dir, f.Name)
		if f.Mode().IsDir() {
			err = os.MkdirAll(fn, 0755)
			if err != nil {
				return fmt.Errorf("Error creating directory %s: %v", fn, err)
			}
			continue
		}
		w, err := os.Create(fn)
		if err != nil {
			return fmt.Errorf("Error creating file %s: %v", fn, err)
		}
		err = os.Chmod(fn, f.Mode())
		if err != nil {
			return fmt.Errorf("Error setting mode for file %s: %v", fn, err)
		}
		defer w.Close()
		r, err := f.Open()
		if err != nil {
			return fmt.Errorf("Error opening file %s: %v", f.Name, err)
		}
		defer r.Close()
		_, err = io.Copy(w, r)
		if err != nil {
			return fmt.Errorf("Error copying to file %s: %v", fn, err)
		}
		// explicitly close files to ensure we don't hit too many open files limit
		w.Close()
		r.Close()
	}

	return nil
}

func main() {
	deftesscmd := "tesseract"
	defgbookcmd := "getgbook"
	if runtime.GOOS == "windows" {
		deftesscmd = "C:\\Program Files\\Tesseract-OCR\\tesseract.exe"
		defgbookcmd = "getgbook.exe"
	}

	verbose := flag.Bool("v", false, "verbose")
	usegui := flag.Bool("gui", false, "Use graphical user interface")
	systess := flag.Bool("systess", false, "Use the system installed Tesseract, rather than the copy embedded in rescribe.")
	training := flag.String("t", "rescribev9_fast.traineddata", `Path to the tesseract training file to use.
These training files are included in rescribe, and are always available:
- eng.traineddata (English, modern print)
- lat.traineddata (Latin, modern print)
- rescribev9_fast.traineddata (Latin/English/French, printed ca 1500-1800)
	`)
	gbookcmd := flag.String("gbookcmd", defgbookcmd, "The getgbook executable to run. You may need to set this to the full path of getgbook.exe if you're on Windows.")
	tesscmd := flag.String("tesscmd", deftesscmd, "The Tesseract executable to run. You may need to set this to the full path of Tesseract.exe if you're on Windows.")
	wipe := flag.Bool("wipe", false, "Use wiper tool to remove noise like gutters from page before processing.")
	fullpdf := flag.Bool("fullpdf", false, "Use highest image quality for searchable PDF (requires lots of RAM).")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() > 2 {
		flag.Usage()
		return
	}

	var err error

	var verboselog *log.Logger
	if *verbose {
		verboselog = log.New(os.Stdout, "", 0)
	} else {
		var n NullWriter
		verboselog = log.New(n, "", 0)
	}

	tessdir := ""
	trainingPath := *training
	tessCommand := *tesscmd

	tessdir, err = ioutil.TempDir("", "tesseract")
	if err != nil {
		log.Fatalln("Error setting up tesseract directory:", err)
	}

	if !*systess && len(tesszip) > 0 {
		err = unpackZip(tesszip, tessdir)
		if err != nil {
			log.Fatalln("Error unpacking embedded Tesseract zip:", err)
		}
		switch runtime.GOOS {
		case "darwin":
			tessCommand = filepath.Join(tessdir, "tesseract")
		case "linux":
			tessCommand = filepath.Join(tessdir, "tesseract")
		case "windows":
			tessCommand = filepath.Join(tessdir, "tesseract.exe")
		}
	}

	_, err = exec.LookPath(tessCommand)
	if err != nil {
		log.Fatalf("No tesseract executable found [tried %s], either set -tesscmd and -systess on the command line or use the official build which includes an embedded copy of Tesseract.", tessCommand)
	}

	gbookCommand := *gbookcmd
	if len(gbookzip) > 0 {
		err = unpackZip(gbookzip, tessdir)
		if err != nil {
			log.Fatalln("Error unpacking embedded getgbook zip:", err)
		}
		switch runtime.GOOS {
		case "darwin":
			gbookCommand = filepath.Join(tessdir, "getgbook")
		case "linux":
			gbookCommand = filepath.Join(tessdir, "getgbook")
		case "windows":
			gbookCommand = filepath.Join(tessdir, "getgbook.exe")
		}
	}

	_, err = exec.LookPath(gbookCommand)
	if err != nil {
		log.Printf("No getgbook found [tried %s], google book downloading will be disabled, either set -gbookcmd on the command line or use the official build which includes an embedded getgbook.", gbookCommand)
		gbookCommand = ""
	}

	tessdatadir := filepath.Join(tessdir, "tessdata")
	err = os.MkdirAll(tessdatadir, 0755)
	if err != nil {
		log.Fatalln("Error setting up tessdata directory:", err)
	}
	if len(tessdatazip) > 0 {
		err = unpackZip(tessdatazip, tessdatadir)
		if err != nil {
			log.Fatalln("Error unpacking embedded tessdata zip:", err)
		}
	}

	// copy training path to the tessdir directory, so that we can keep that a
	// writeable space, which is needed opening other trainings in sandboxes
	// like flatpak
	in, err := os.Open(trainingPath)
	trainingPath = filepath.Join(tessdatadir, filepath.Base(trainingPath))
	if err != nil {
		in, err = os.Open(trainingPath)
		if err != nil {
			log.Fatalf("Error opening training file %s: %v", trainingPath, err)
		}
	}
	defer in.Close()
	newPath := trainingPath + ".new"
	out, err := os.Create(newPath)
	if err != nil {
		log.Fatalf("Error creating training file %s: %v", newPath, err)
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	if err != nil {
		log.Fatalf("Error copying training file to %s: %v", newPath, err)
	}
	in.Close()
	out.Close()
	err = os.Rename(newPath, trainingPath)
	if err != nil {
		log.Fatalf("Error moving new training file to %s: %v", trainingPath, err)
	}

	abstraining, err := filepath.Abs(trainingPath)
	if err != nil {
		log.Fatalf("Error getting absolute path of training %s: %v", trainingPath, err)
	}
	tessPrefix, trainingName := filepath.Split(abstraining)
	trainingName = strings.TrimSuffix(trainingName, ".traineddata")
	err = os.Setenv("TESSDATA_PREFIX", tessPrefix)
	if err != nil {
		log.Fatalln("Error setting TESSDATA_PREFIX:", err)
	}

	if flag.NArg() < 1 || *usegui {
		err := startGui(verboselog, tessCommand, gbookCommand, trainingName, tessdir)
		err = os.RemoveAll(tessdir)
		if err != nil {
			log.Printf("Error removing tesseract directory %s: %v", tessdir, err)
		}

		if err != nil {
			log.Fatalln("Error in gui:", err)
		}
		return
	}

	f, err := os.Open(trainingPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Training files %s or %s could not be opened.\n", *training, trainingPath)
		fmt.Fprintf(os.Stderr, "Set the `-t` flag with path to a tesseract .traineddata file.\n")
		os.Exit(1)
	}
	f.Close()

	bookdir := flag.Arg(0)
	bookname := strings.ReplaceAll(filepath.Base(bookdir), " ", "_")
	savedir := bookdir
	if flag.NArg() > 1 {
		savedir = flag.Arg(1)
	}

	ispdf := false

	fi, err := os.Stat(bookdir)
	if err != nil {
		log.Fatalln("Error opening book file/dir:", err)
	}

	var ctx context.Context
	ctx = context.Background()

	// TODO: support google book downloading, as done with the GUI

	// try opening as a PDF, and extracting
	if !fi.IsDir() {
		if flag.NArg() < 2 {
			savedir = strings.TrimSuffix(bookdir, ".pdf")
		}

		bookdir, err = extractPdfImgs(ctx, bookdir)
		if err != nil {
			log.Fatalln("Error opening file as PDF:", err)
		}
		// if this occurs then extractPdfImgs() will have recovered from
		// a panic in the pdf package
		if bookdir == "" {
			log.Fatalln("Error opening file as PDF: image type not supported, you will need to extract images manually.")
		}

		bookname = strings.TrimSuffix(bookname, ".pdf")

		ispdf = true
	}

	err = startProcess(ctx, verboselog, tessCommand, bookdir, bookname, trainingName, savedir, tessdir, !*wipe, *fullpdf)
	if err != nil {
		log.Fatalln(err)
	}

	if !*systess {
		err = os.RemoveAll(tessdir)
		if err != nil {
			log.Printf("Error removing tesseract directory %s: %v", tessdir, err)
		}
	}

	if ispdf {
		os.RemoveAll(filepath.Clean(filepath.Join(bookdir, "..")))
	}
}

// extractPdfImgs extracts all images embedded in a PDF to a
// temporary directory, which is returned on success.
func extractPdfImgs(ctx context.Context, path string) (string, error) {
	defer func() {
		// unfortunately the pdf library will panic if it sees an encoding
		// it can't decode, so recover from that and give a warning
		r := recover()
		if r != nil {
			fmt.Fprintf(os.Stderr, "Warning: Error extracting from PDF: %v\n", r)
		}
	}()

	p, err := pdf.Open(path)
	if err != nil {
		return "", err
	}

	bookname := strings.TrimSuffix(filepath.Base(path), ".pdf")

	tempdir, err := ioutil.TempDir("", "bookpipeline")
	if err != nil {
		return "", fmt.Errorf("Error setting up temporary directory: %v", err)
	}
	tempdir = filepath.Join(tempdir, bookname)
	err = os.Mkdir(tempdir, 0755)
	if err != nil {
		return "", fmt.Errorf("Error setting up temporary directory: %v", err)
	}

	for pgnum := 1; pgnum <= p.NumPage(); pgnum++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if p.Page(pgnum).V.IsNull() {
			continue
		}
		var rotate int64
		for v := p.Page(pgnum).V; !v.IsNull(); v = v.Key("Parent") {
			if r := v.Key("Rotate"); !r.IsNull() {
				rotate = r.Int64()
			}
		}
		res := p.Page(pgnum).Resources()
		if res.Kind() != pdf.Dict {
			continue
		}
		xobj := res.Key("XObject")
		if xobj.Kind() != pdf.Dict {
			continue
		}
		// BUG: for some PDFs this includes images multiple times for each page
		for _, k := range xobj.Keys() {
			obj := xobj.Key(k)
			if obj.Kind() != pdf.Stream {
				continue
			}

			fn := fmt.Sprintf("%04d-%s.jpg", pgnum, k)
			path := filepath.Join(tempdir, fn)
			w, err := os.Create(path)
			defer w.Close()
			if err != nil {
				return tempdir, fmt.Errorf("Error creating file to extract PDF image: %v\n", err)
			}
			r := obj.Reader()
			defer r.Close()
			_, err = io.Copy(w, r)
			if err != nil {
				return tempdir, fmt.Errorf("Error writing extracted image %s from PDF: %v\n", fn, err)
			}
			w.Close()
			r.Close()

			err = rmIfNotImage(path)
			if err != nil {
				return tempdir, fmt.Errorf("Error removing extracted image %s from PDF: %v\n", fn, err)
			}

			if rotate != 0 {
				err = rotateImage(path, rotate)
				if err != nil {
					return tempdir, fmt.Errorf("Error rotating extracted image %s from PDF: %v\n", fn, err)
				}
			}
		}
	}
	// TODO: check for places where there are multiple images per page, and only keep largest ones where that's the case

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	return tempdir, nil
}

// rmIfNotImage attempts to decode a given file as an image. If it is
// decode-able as PNG, then rename file extension from .jpg to .png,
// if it is decode-able as TIFF then convert to PNG and rename file
// extension appropriately, if it fails to be read as PNG, TIFF or
// JPEG it will just be deleted.
func rmIfNotImage(f string) error {
	r, err := os.Open(f)
	defer r.Close()
	if err != nil {
		return fmt.Errorf("Failed to open image %s: %v\n", f, err)
	}
	_, err = png.Decode(r)
	r.Close()
	if err == nil {
		b := strings.TrimSuffix(f, ".jpg")
		err = os.Rename(f, b+".png")
		if err != nil {
			return fmt.Errorf("Error renaming %s to %s: %v", f, b+".png", err)
		}
		return nil
	}

	r, err = os.Open(f)
	defer r.Close()
	if err != nil {
		return fmt.Errorf("Failed to open image %s: %v\n", f, err)
	}
	_, err = jpeg.Decode(r)
	r.Close()
	if err == nil {
		return nil
	}

	r, err = os.Open(f)
	defer r.Close()
	if err != nil {
		return fmt.Errorf("Failed to open image %s: %v\n", f, err)
	}
	t, err := tiff.Decode(r)
	if err == nil {
		b := strings.TrimSuffix(f, ".jpg")
		n, err := os.Create(b + ".png")
		defer n.Close()
		if err != nil {
			return fmt.Errorf("Failed to create file to store new png %s from tiff %s: %v\n", b+".png", f, err)
		}
		err = png.Encode(n, t)
		if err != nil {
			return fmt.Errorf("Failed to encode tiff as png for %s: %v\n", f, err)
		}
		r.Close()
		err = os.Remove(f)
		if err != nil {
			return fmt.Errorf("Failed to remove original tiff %s: %v\n", f, err)
		}
		return nil
	}

	r.Close()
	err = os.Remove(f)
	if err != nil {
		return fmt.Errorf("Failed to remove invalid image %s: %v", f, err)
	}

	return nil
}

// rotateImage rotates an image at the given path by the given angle
func rotateImage(path string, angle int64) error {
	switch angle {
	case 90:
		// proceed with the rest of the function
	case 180, 270:
		// rotate the image again first, as many times as necessary.
		// this is inefficient but easy.
		err := rotateImage(path, angle-90)
		if err != nil {
			return fmt.Errorf("error with a rotation run: %w", err)
		}
	default:
		return fmt.Errorf("Rotation angle of %d is not supported", angle)
	}

	r, err := os.Open(path)
	defer r.Close()
	if err != nil {
		return fmt.Errorf("Failed to open image: %w", err)
	}
	img, err := png.Decode(r)
	if err != nil {
		r.Close()
		r, err = os.Open(path)
		defer r.Close()
		if err != nil {
			return fmt.Errorf("Failed to open image: %w", err)
		}
		img, err = jpeg.Decode(r)
	}
	if err != nil {
		r.Close()
		r, err = os.Open(path)
		defer r.Close()
		if err != nil {
			return fmt.Errorf("Failed to open image: %w", err)
		}
		img, err = tiff.Decode(r)
	}
	if err != nil {
		return fmt.Errorf("Failed to decode image as png, jpeg or tiff: %w", err)
	}

	b := img.Bounds()

	orig := image.NewRGBA(b)
	draw.Draw(orig, b, img, b.Min, draw.Src)

	newb := image.Rectangle{
		Min: image.Point{X: 0, Y: 0},
		Max: image.Point{X: b.Dy(), Y: b.Dx()},
	}
	new := image.NewRGBA(newb)

	for x := b.Min.X; x < b.Max.X; x++ {
		desty := newb.Min.Y + x
		for y := b.Max.Y; y > b.Min.Y; y-- {
			destx := b.Dy() - y + newb.Min.X
			new.SetRGBA(destx, desty, orig.RGBAAt(x, y))
		}
	}

	err = r.Close()
	if err != nil {
		return fmt.Errorf("Failed to close image: %w", err)
	}
	w, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Failed to create rotated image: %w", err)
	}
	defer w.Close()

	if !strings.HasSuffix(path, ".jpg") {
		err = jpeg.Encode(w, new, nil)
	} else {
		err = png.Encode(w, new)
	}
	if err != nil {
		return fmt.Errorf("Failed to encode rotated image: %w", err)
	}

	return nil
}

func startProcess(ctx context.Context, logger *log.Logger, tessCommand string, bookdir string, bookname string, trainingName string, savedir string, tessdir string, nowipe bool, fullpdf bool) error {
	cmd := exec.Command(tessCommand, "--help")
	pipeline.HideCmd(cmd)
	_, err := cmd.Output()
	if err != nil {
		errmsg := "Error, Can't run Tesseract\n"
		errmsg += "Ensure that Tesseract is installed and available, or don't use the -systess flag.\n"
		errmsg += "You may need to -tesscmd to the full path of Tesseract.exe if you're on Windows, like this:\n"
		errmsg += "  rescribe -tesscmd 'C:\\Program Files\\Tesseract OCR (x86)\\tesseract.exe' ...\n"
		errmsg += fmt.Sprintf("Error message: %v", err)
		return fmt.Errorf(errmsg)
	}

	tempdir, err := ioutil.TempDir("", "bookpipeline")
	if err != nil {
		return fmt.Errorf("Error setting up temporary directory: %v", err)
	}

	var conn Pipeliner
	conn = &bookpipeline.LocalConn{Logger: logger, TempDir: tempdir}

	conn.Log("Setting up session")
	err = conn.Init()
	if err != nil {
		return fmt.Errorf("Error setting up connection: %v", err)
	}
	conn.Log("Finished setting up session")

	fmt.Printf("Copying book to pipeline\n")

	err = uploadbook(ctx, bookdir, bookname, conn, nowipe)
	if err != nil {
		_ = os.RemoveAll(tempdir)
		return fmt.Errorf("Error uploading book: %v", err)
	}

	fmt.Printf("Processing book\n")
	err = processbook(ctx, trainingName, tessCommand, conn, fullpdf)
	if err != nil {
		_ = os.RemoveAll(tempdir)
		return fmt.Errorf("Error processing book: %v", err)
	}

	fmt.Printf("Saving finished book to %s\n", savedir)
	err = os.MkdirAll(savedir, 0755)
	if err != nil {
		return fmt.Errorf("Error creating save directory %s: %v", savedir, err)
	}
	err = downloadbook(savedir, bookname, conn)
	if err != nil {
		_ = os.RemoveAll(tempdir)
		return fmt.Errorf("Error saving book: %v", err)
	}

	err = os.RemoveAll(tempdir)
	if err != nil {
		return fmt.Errorf("Error removing temporary directory %s: %v", tempdir, err)
	}

	hocrs, err := filepath.Glob(fmt.Sprintf("%s%s*.hocr", savedir, string(filepath.Separator)))
	if err != nil {
		return fmt.Errorf("Error looking for .hocr files: %v", err)
	}

	err = addFullTxt(hocrs, bookname)
	if err != nil {
		log.Fatalf("Error creating full txt version: %v", err)
	}

	for _, v := range hocrs {
		err = addTxtVersion(v)
		if err != nil {
			log.Fatalf("Error creating txt version of %s: %v", v, err)
		}

		err = os.MkdirAll(filepath.Join(savedir, "hocr"), 0755)
		if err != nil {
			log.Fatalf("Error creating hocr directory: %v", err)
		}

		err = os.Rename(v, filepath.Join(savedir, "hocr", filepath.Base(v)))
		if err != nil {
			log.Fatalf("Error moving hocr %s to hocr directory: %v", v, err)
		}

		pngname := strings.Replace(v, ".hocr", ".png", 1)
		err = os.MkdirAll(filepath.Join(savedir, "png"), 0755)
		if err != nil {
			log.Fatalf("Error creating png directory: %v", err)
		}

		err = os.Rename(pngname, filepath.Join(savedir, "png", filepath.Base(pngname)))
		if err != nil {
			log.Fatalf("Error moving png %s to png directory: %v", pngname, err)
		}

	}

	// For simplicity, remove .binarised.pdf and rename .colour.pdf to .pdf
	// providing they both exist, otherwise just rename whichever exists
	// to .pdf.
	binpath := filepath.Join(savedir, bookname+".binarised.pdf")
	colourpath := filepath.Join(savedir, bookname+".colour.pdf")
	fullsizepath := filepath.Join(savedir, bookname+".original.pdf")
	pdfpath := filepath.Join(savedir, bookname+" searchable.pdf")

	// If full size pdf is requested, replace colour.pdf with it
	if fullpdf {
		_ = os.Rename(fullsizepath, colourpath)
	}

	_, err = os.Stat(binpath)
	binexists := err == nil || os.IsExist(err)
	_, err = os.Stat(colourpath)
	colourexists := err == nil || os.IsExist(err)

	if binexists && colourexists {
		_ = os.Remove(binpath)
		_ = os.Rename(colourpath, pdfpath)
	} else if binexists {
		_ = os.Rename(binpath, pdfpath)
	} else if colourexists {
		_ = os.Rename(colourpath, pdfpath)
	}

	return nil
}

func addTxtVersion(hocrfn string) error {
	dir := filepath.Dir(hocrfn)
	err := os.MkdirAll(filepath.Join(dir, "text"), 0755)
	if err != nil {
		log.Fatalf("Error creating text directory: %v", err)
	}

	t, err := hocr.GetText(hocrfn)
	if err != nil {
		return fmt.Errorf("Error getting text from hocr file %s: %v", hocrfn, err)
	}

	basefn := filepath.Base(hocrfn)
	for _, v := range thresholds {
		basefn = strings.TrimSuffix(basefn, fmt.Sprintf("_bin%.1f.hocr", v))
	}
	fn := filepath.Join(dir, "text", basefn+".txt")

	err = ioutil.WriteFile(fn, []byte(t), 0644)
	if err != nil {
		return fmt.Errorf("Error creating text file %s: %v", fn, err)
	}

	return nil
}

func addFullTxt(hocrs []string, bookname string) error {
	if len(hocrs) == 0 {
		return nil
	}
	var full string
	for i, v := range hocrs {
		t, err := hocr.GetText(v)
		if err != nil {
			return fmt.Errorf("Error getting text from hocr file %s: %v", v, err)
		}
		if i > 0 {
			full += "\n"
		}
		full += t
	}

	dir := filepath.Dir(hocrs[0])
	fn := filepath.Join(dir, bookname+".txt")
	err := ioutil.WriteFile(fn, []byte(full), 0644)
	if err != nil {
		return fmt.Errorf("Error creating text file %s: %v", fn, err)
	}

	return nil
}

func uploadbook(ctx context.Context, dir string, name string, conn Pipeliner, nowipe bool) error {
	_, err := os.Stat(dir)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("Error: directory %s not found", dir)
	}
	err = pipeline.CheckImages(ctx, dir)
	if err != nil {
		return fmt.Errorf("Error with images in %s: %v", dir, err)
	}
	err = pipeline.UploadImages(ctx, dir, name, conn)
	if err != nil {
		return fmt.Errorf("Error saving images to process from %s: %v", dir, err)
	}

	qid := pipeline.DetectQueueType(dir, conn, nowipe)
	fmt.Printf("Uploading to queue %s\n", qid)

	err = conn.AddToQueue(qid, name)
	if err != nil {
		return fmt.Errorf("Error adding book job to queue %s: %v", qid, err)
	}

	return nil
}

func downloadbook(dir string, name string, conn Pipeliner) error {
	err := pipeline.DownloadBestPages(dir, name, conn)
	if err != nil {
		return fmt.Errorf("No images found")
	}

	err = pipeline.DownloadBestPngs(dir, name, conn)
	if err != nil {
		return fmt.Errorf("No images found")
	}

	err = pipeline.DownloadPdfs(dir, name, conn)
	if err != nil {
		return fmt.Errorf("Error downloading PDFs: %v", err)
	}

	err = pipeline.DownloadAnalyses(dir, name, conn)
	if err != nil {
		return fmt.Errorf("Error downloading analyses: %v", err)
	}

	return nil
}

func processbook(ctx context.Context, training string, tesscmd string, conn Pipeliner, fullpdf bool) error {
	origPattern := regexp.MustCompile(`[0-9]{4}.(jpg|png)$`)
	wipePattern := regexp.MustCompile(`[0-9]{4,6}(.bin)?.(jpg|png)$`)
	ocredPattern := regexp.MustCompile(`.hocr$`)

	var checkPreQueue <-chan time.Time
	var checkPreNoWipeQueue <-chan time.Time
	var checkWipeQueue <-chan time.Time
	var checkOCRPageQueue <-chan time.Time
	var checkAnalyseQueue <-chan time.Time
	var stopIfQuiet *time.Timer
	checkPreQueue = time.After(0)
	checkPreNoWipeQueue = time.After(0)
	checkWipeQueue = time.After(0)
	checkOCRPageQueue = time.After(0)
	checkAnalyseQueue = time.After(0)
	var quietTime = 1 * time.Second
	stopIfQuiet = time.NewTimer(quietTime)
	if quietTime == 0 {
		stopIfQuiet.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-checkPreNoWipeQueue:
			msg, err := conn.CheckQueue(conn.PreNoWipeQueueId(), QueueTimeoutSecs)
			checkPreNoWipeQueue = time.After(PauseBetweenChecks)
			if err != nil {
				return fmt.Errorf("Error checking preprocess no wipe queue: %v", err)
			}
			if msg.Handle == "" {
				conn.Log("No message received on preprocess no wipe queue, sleeping")
				continue
			}
			stopTimer(stopIfQuiet)
			conn.Log("Message received on preprocess no wipe queue, processing", msg.Body)
			fmt.Printf("  Preprocessing book (binarising only, no wiping)\n")
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Preprocess(thresholds, true), origPattern, conn.PreNoWipeQueueId(), conn.OCRPageQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				return fmt.Errorf("Error during preprocess (no wipe): %v", err)
			}
			fmt.Printf("  OCRing pages ") // this is expected to be added to with dots by OCRPage output
		case <-checkPreQueue:
			msg, err := conn.CheckQueue(conn.PreQueueId(), QueueTimeoutSecs)
			checkPreQueue = time.After(PauseBetweenChecks)
			if err != nil {
				return fmt.Errorf("Error checking preprocess queue: %v", err)
			}
			if msg.Handle == "" {
				conn.Log("No message received on preprocess queue, sleeping")
				continue
			}
			stopTimer(stopIfQuiet)
			conn.Log("Message received on preprocess queue, processing", msg.Body)
			fmt.Printf("  Preprocessing book (binarising and wiping)\n")
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Preprocess(thresholds, false), origPattern, conn.PreQueueId(), conn.OCRPageQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				return fmt.Errorf("Error during preprocess: %v", err)
			}
			fmt.Printf("  OCRing pages ") // this is expected to be added to with dots by OCRPage output
		case <-checkWipeQueue:
			msg, err := conn.CheckQueue(conn.WipeQueueId(), QueueTimeoutSecs)
			checkWipeQueue = time.After(PauseBetweenChecks)
			if err != nil {
				return fmt.Errorf("Error checking wipeonly queue, %v", err)
			}
			if msg.Handle == "" {
				conn.Log("No message received on wipeonly queue, sleeping")
				continue
			}
			stopTimer(stopIfQuiet)
			conn.Log("Message received on wipeonly queue, processing", msg.Body)
			fmt.Printf("  Preprocessing book (wiping only)\n")
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Wipe, wipePattern, conn.WipeQueueId(), conn.OCRPageQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				return fmt.Errorf("Error during wipe: %v", err)
			}
			fmt.Printf("  OCRing pages ") // this is expected to be added to with dots by OCRPage output
		case <-checkOCRPageQueue:
			msg, err := conn.CheckQueue(conn.OCRPageQueueId(), QueueTimeoutSecs)
			checkOCRPageQueue = time.After(PauseBetweenChecks)
			if err != nil {
				return fmt.Errorf("Error checking OCR Page queue: %v", err)
			}
			if msg.Handle == "" {
				continue
			}
			// Have OCRPageQueue checked immediately after completion, as chances are high that
			// there will be more pages that should be done without delay
			checkOCRPageQueue = time.After(0)
			stopTimer(stopIfQuiet)
			conn.Log("Message received on OCR Page queue, processing", msg.Body)
			fmt.Printf(".")
			err = pipeline.OcrPage(ctx, msg, conn, pipeline.Ocr(training, tesscmd), conn.OCRPageQueueId(), conn.AnalyseQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				return fmt.Errorf("\nError during OCR Page process: %v", err)
			}
		case <-checkAnalyseQueue:
			msg, err := conn.CheckQueue(conn.AnalyseQueueId(), QueueTimeoutSecs)
			checkAnalyseQueue = time.After(PauseBetweenChecks)
			if err != nil {
				return fmt.Errorf("Error checking analyse queue: %v", err)
			}
			if msg.Handle == "" {
				conn.Log("No message received on analyse queue, sleeping")
				continue
			}
			stopTimer(stopIfQuiet)
			conn.Log("Message received on analyse queue, processing", msg.Body)
			fmt.Printf("\n  Analysing OCR and compiling PDFs\n")
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Analyse(conn, fullpdf), ocredPattern, conn.AnalyseQueueId(), "")
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				return fmt.Errorf("Error during analysis: %v", err)
			}
		case <-stopIfQuiet.C:
			conn.Log("Processing finished")
			return nil
		}
	}
}
