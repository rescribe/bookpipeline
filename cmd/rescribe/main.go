// Copyright 2021 Nick White.
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
	_ "embed"
	"flag"
	"fmt"
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

	"rescribe.xyz/bookpipeline"
	"rescribe.xyz/utils/pkg/hocr"

	"rescribe.xyz/bookpipeline/internal/pipeline"
)

const usage = `Usage: rescribe [-v] [-t training] bookdir [savedir]

Process and OCR a book using the Rescribe pipeline on a local machine.

OCR results are saved into the bookdir directory unless savedir is
specified.
`

//go:embed tessdata.20211001.zip
var tessdatazip []byte

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

// unpackTessZip unpacks a byte array of a zip file into a directory
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
	if runtime.GOOS == "windows" {
		deftesscmd = "C:\\Program Files\\Tesseract-OCR\\tesseract.exe"
	}

	verbose := flag.Bool("v", false, "verbose")
	systess := flag.Bool("systess", false, "Use the system installed Tesseract, rather than the copy embedded in rescribe.")
	training := flag.String("t", "rescribev8_fast.traineddata", `Path to the tesseract training file to use.
These training files are included in rescribe, and are always available:
- carolinemsv1_fast.traineddata (Caroline Miniscule)
- eng.traineddata (Modern English)
- lat.traineddata (Latin modern printing)
- rescribefrav2_fast.traineddata (French historic printing)
- rescribev8_fast.traineddata (Latin historic printing)
	`)
	tesscmd := flag.String("tesscmd", deftesscmd, "The Tesseract executable to run. You may need to set this to the full path of Tesseract.exe if you're on Windows.")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 || flag.NArg() > 2 {
		flag.Usage()
		return
	}

	var err error
	bookdir := flag.Arg(0)
	bookname := filepath.Base(bookdir)
	savedir := bookdir
	tessdir := ""
	if flag.NArg() > 1 {
		savedir = flag.Arg(1)
	}
	trainingPath := *training
	tessCommand := *tesscmd

	var verboselog *log.Logger
	if *verbose {
		verboselog = log.New(os.Stdout, "", 0)
	} else {
		var n NullWriter
		verboselog = log.New(n, "", 0)
	}

	tessdir, err = ioutil.TempDir("", "tesseract")
	if err != nil {
		log.Fatalln("Error setting up tesseract directory:", err)
	}

	if !*systess {
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

	tessdatadir := filepath.Join(tessdir, "tessdata")
	err = os.MkdirAll(tessdatadir, 0755)
	if err != nil {
		log.Fatalln("Error setting up tessdata directory:", err)
	}
	err = unpackZip(tessdatazip, tessdatadir)
	if err != nil {
		log.Fatalln("Error unpacking embedded tessdata zip:", err)
	}

	// if trainingPath doesn't exist, set it to the embedded training instead
	_, err = os.Stat(trainingPath)
	if err != nil && !os.IsExist(err) {
		trainingPath = filepath.Base(trainingPath)
		trainingPath = filepath.Join(tessdatadir, trainingPath)
	}

	f, err := os.Open(trainingPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Training files %s or %s could not be opened.\n", *training, trainingPath)
		fmt.Fprintf(os.Stderr, "Set the `-t` flag with path to a tesseract .traineddata file.\n")
		os.Exit(1)
	}
	f.Close()

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

	_, err = exec.Command(tessCommand, "--help").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Can't run Tesseract.\n")
		fmt.Fprintf(os.Stderr, "Ensure that Tesseract is installed and available, or don't use the -systess flag.\n")
		fmt.Fprintf(os.Stderr, "You may need to -tesscmd to the full path of Tesseract.exe if you're on Windows, like this:\n")
		fmt.Fprintf(os.Stderr, "  rescribe -tesscmd 'C:\\Program Files\\Tesseract OCR (x86)\\tesseract.exe' ...\n")
		fmt.Fprintf(os.Stderr, "Error message: %v\n", err)
		os.Exit(1)
	}

	tempdir, err := ioutil.TempDir("", "bookpipeline")
	if err != nil {
		log.Fatalln("Error setting up temporary directory:", err)
	}

	var conn Pipeliner
	conn = &bookpipeline.LocalConn{Logger: verboselog, TempDir: tempdir}

	conn.Log("Setting up session")
	err = conn.Init()
	if err != nil {
		log.Fatalln("Error setting up connection:", err)
	}
	conn.Log("Finished setting up session")

	fmt.Printf("Copying book to pipeline\n")

	err = uploadbook(bookdir, bookname, conn)
	if err != nil {
		_ = os.RemoveAll(tempdir)
		log.Fatalln(err)
	}

	fmt.Printf("Processing book\n")
	err = processbook(trainingName, tessCommand, conn)
	if err != nil {
		_ = os.RemoveAll(tempdir)
		log.Fatalln(err)
	}

	fmt.Printf("Saving finished book to %s\n", savedir)
	err = os.MkdirAll(savedir, 0755)
	if err != nil {
		log.Fatalf("Error creating save directory %s: %v", savedir, err)
	}
	err = downloadbook(savedir, bookname, conn)
	if err != nil {
		_ = os.RemoveAll(tempdir)
		log.Fatalln(err)
	}

	err = os.RemoveAll(tempdir)
	if err != nil {
		log.Fatalf("Error removing temporary directory %s: %v", tempdir, err)
	}

	if !*systess {
		err = os.RemoveAll(tessdir)
		if err != nil {
			log.Fatalf("Error removing tesseract directory %s: %v", tessdir, err)
		}
	}

	hocrs, err := filepath.Glob(fmt.Sprintf("%s%s*.hocr", savedir, string(filepath.Separator)))
	if err != nil {
		log.Fatalf("Error looking for .hocr files: %v", err)
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
	}

	// For simplicity, remove .binarised.pdf and rename .colour.pdf to .pdf
	_ = os.Remove(filepath.Join(savedir, bookname+".binarised.pdf"))
	_ = os.Rename(filepath.Join(savedir, bookname+".colour.pdf"), filepath.Join(savedir, bookname+".pdf"))
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

func uploadbook(dir string, name string, conn Pipeliner) error {
	_, err := os.Stat(dir)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("Error: directory %s not found", dir)
	}
	err = pipeline.CheckImages(dir)
	if err != nil {
		return fmt.Errorf("Error with images in %s: %v", dir, err)
	}
	err = pipeline.UploadImages(dir, name, conn)
	if err != nil {
		return fmt.Errorf("Error saving images to process from %s: %v", dir, err)
	}

	qid := pipeline.DetectQueueType(dir, conn)

	err = conn.AddToQueue(qid, name)
	if err != nil {
		return fmt.Errorf("Error adding book job to queue %s: %v", qid, err)
	}

	return nil
}

func downloadbook(dir string, name string, conn Pipeliner) error {
	err := pipeline.DownloadBestPages(dir, name, conn, false)
	if err != nil {
		return fmt.Errorf("Error downloading best pages: %v", err)
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

func processbook(training string, tesscmd string, conn Pipeliner) error {
	origPattern := regexp.MustCompile(`[0-9]{4}.jpg$`)
	wipePattern := regexp.MustCompile(`[0-9]{4,6}(.bin)?.png$`)
	ocredPattern := regexp.MustCompile(`.hocr$`)

	var checkPreQueue <-chan time.Time
	var checkWipeQueue <-chan time.Time
	var checkOCRPageQueue <-chan time.Time
	var checkAnalyseQueue <-chan time.Time
	var stopIfQuiet *time.Timer
	checkPreQueue = time.After(0)
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
			err = pipeline.ProcessBook(msg, conn, pipeline.Preprocess(thresholds), origPattern, conn.PreQueueId(), conn.OCRPageQueueId())
			fmt.Printf("  OCRing pages ") // this is expected to be added to with dots by OCRPage output
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				return fmt.Errorf("Error during preprocess: %v", err)
			}
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
			err = pipeline.ProcessBook(msg, conn, pipeline.Wipe, wipePattern, conn.WipeQueueId(), conn.OCRPageQueueId())
			fmt.Printf("  OCRing pages ") // this is expected to be added to with dots by OCRPage output
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				return fmt.Errorf("Error during wipe: %v", err)
			}
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
			err = pipeline.OcrPage(msg, conn, pipeline.Ocr(training, tesscmd), conn.OCRPageQueueId(), conn.AnalyseQueueId())
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
			err = pipeline.ProcessBook(msg, conn, pipeline.Analyse(conn), ocredPattern, conn.AnalyseQueueId(), "")
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
