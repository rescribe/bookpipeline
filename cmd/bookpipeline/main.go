// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// bookpipeline is the core command of the bookpipeline package, which
// watches queues for messages and does various OCR related tasks when
// it receives them, saving the results in cloud storage.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"time"

	"rescribe.xyz/bookpipeline"

	"rescribe.xyz/bookpipeline/internal/pipeline"
)

const usage = `Usage: bookpipeline [-v] [-c conn] [-np] [-nw] [-nop] [-na] [-t training] [-shutdown true/false] [-autostop secs]

Watches the preprocess, wipeonly, ocrpage and analyse queues for messages.
When one is found this general process is followed:

- The book name is hidden from the queue, and a 'heartbeat' is
  started which keeps it hidden (this will time out after 2 minutes
  if the program is terminated)
- The necessary files from bookname/ are downloaded
- The files are processed
- The resulting files are uploaded to bookname/
- The heartbeat is stopped
- The book name is removed from the queue it was taken from, and
  added to the next queue for future processing

Optionally important messages can be emailed by the process; to enable
this put a text file in {UserConfigDir}/bookpipeline/mailsettings with
the contents: {smtpserver} {port} {username} {password} {from} {to}
`

const QueueTimeoutSecs = 2 * 60
const PauseBetweenChecks = 3 * time.Minute
const LogSaveTime = 1 * time.Minute

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
	TestQueueId() string
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

func main() {
	verbose := flag.Bool("v", false, "verbose")
	training := flag.String("t", "rescribev9", "default tesseract training file to use (without the .traineddata part)")
	nopreproc := flag.Bool("np", false, "disable preprocessing")
	nowipe := flag.Bool("nw", false, "disable wipeonly")
	noocrpg := flag.Bool("nop", false, "disable ocr on individual pages")
	noanalyse := flag.Bool("na", false, "disable analysis")
	autostop := flag.Int64("autostop", 300, "automatically stop process if no work has been available for this number of seconds (to disable autostop set to 0)")
	autoshutdown := flag.Bool("shutdown", false, "automatically shut down host computer if there has been no work to do for the duration set with -autostop")
	conntype := flag.String("c", "aws", "connection type ('aws' or 'local')")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	var verboselog *log.Logger
	if *verbose {
		verboselog = log.New(os.Stdout, "", 0)
	} else {
		var n NullWriter
		verboselog = log.New(n, "", 0)
	}

	origPattern := regexp.MustCompile(`[0-9]{4}.jpg$`)
	wipePattern := regexp.MustCompile(`[0-9]{4,6}(.bin)?.png$`)
	ocredPattern := regexp.MustCompile(`.hocr$`)

	var ctx context.Context
	ctx = context.Background()

	var conn Pipeliner
	switch *conntype {
	case "aws":
		conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}
	case "local":
		conn = &bookpipeline.LocalConn{Logger: verboselog}
	default:
		log.Fatalln("Unknown connection type")
	}

	var err error
	if *conntype != "local" {
		_, err = pipeline.GetMailSettings()
		if err != nil {
			conn.Log("Warning: disabling email notifications as mail setting retrieval failed: ", err)
		}
	}

	conn.Log("Setting up session")
	err = conn.Init()
	if err != nil {
		log.Fatalln("Error setting up connection:", err)
	}
	conn.Log("Finished setting up session")

	starttime := time.Now().Unix()
	hostname, err := os.Hostname()

	var checkPreQueue <-chan time.Time
	var checkPreNoWipeQueue <-chan time.Time
	var checkWipeQueue <-chan time.Time
	var checkOCRPageQueue <-chan time.Time
	var checkAnalyseQueue <-chan time.Time
	var stopIfQuiet *time.Timer
	var savelognow *time.Ticker
	if !*nopreproc {
		checkPreQueue = time.After(0)
	}
	if !*nowipe {
		checkWipeQueue = time.After(0)
	}
	if !*noocrpg {
		checkOCRPageQueue = time.After(0)
	}
	if !*noanalyse {
		checkAnalyseQueue = time.After(0)
	}
	checkPreNoWipeQueue = time.After(0)
	var quietTime = time.Duration(*autostop) * time.Second
	stopIfQuiet = time.NewTimer(quietTime)
	if quietTime == 0 {
		stopIfQuiet.Stop()
	}

	savelognow = time.NewTicker(LogSaveTime)
	if *conntype == "local" {
		savelognow.Stop()
	}

	for {
		select {
		case <-checkPreQueue:
			msg, err := conn.CheckQueue(conn.PreQueueId(), QueueTimeoutSecs)
			checkPreQueue = time.After(PauseBetweenChecks)
			if err != nil {
				conn.Log("Error checking preprocess queue", err)
				continue
			}
			if msg.Handle == "" {
				conn.Log("No message received on preprocess queue, sleeping")
				continue
			}
			conn.Log("Message received on preprocess queue, processing", msg.Body)
			stopTimer(stopIfQuiet)
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Preprocess([]float64{0.1, 0.2, 0.4, 0.5}, false), origPattern, conn.PreQueueId(), conn.OCRPageQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				conn.Log("Error during preprocess", err)
			}
		case <-checkPreNoWipeQueue:
			msg, err := conn.CheckQueue(conn.PreNoWipeQueueId(), QueueTimeoutSecs)
			checkPreNoWipeQueue = time.After(PauseBetweenChecks)
			if err != nil {
				conn.Log("Error checking preprocess (no wipe) queue", err)
				continue
			}
			if msg.Handle == "" {
				conn.Log("No message received on preprocess (no wipe) queue, sleeping")
				continue
			}
			conn.Log("Message received on preprocess (no wipe) queue, processing", msg.Body)
			stopTimer(stopIfQuiet)
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Preprocess([]float64{0.1, 0.2, 0.4, 0.5}, true), origPattern, conn.PreQueueId(), conn.OCRPageQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				conn.Log("Error during preprocess (no wipe)", err)
			}
		case <-checkWipeQueue:
			msg, err := conn.CheckQueue(conn.WipeQueueId(), QueueTimeoutSecs)
			checkWipeQueue = time.After(PauseBetweenChecks)
			if err != nil {
				conn.Log("Error checking wipeonly queue", err)
				continue
			}
			if msg.Handle == "" {
				conn.Log("No message received on wipeonly queue, sleeping")
				continue
			}
			stopTimer(stopIfQuiet)
			conn.Log("Message received on wipeonly queue, processing", msg.Body)
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Wipe, wipePattern, conn.WipeQueueId(), conn.OCRPageQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				conn.Log("Error during wipe", err)
			}
		case <-checkOCRPageQueue:
			msg, err := conn.CheckQueue(conn.OCRPageQueueId(), QueueTimeoutSecs)
			checkOCRPageQueue = time.After(PauseBetweenChecks)
			if err != nil {
				conn.Log("Error checking OCR Page queue", err)
				continue
			}
			if msg.Handle == "" {
				continue
			}
			// Have OCRPageQueue checked immediately after completion, as chances are high that
			// there will be more pages that should be done without delay
			checkOCRPageQueue = time.After(0)
			stopTimer(stopIfQuiet)
			conn.Log("Message received on OCR Page queue, processing", msg.Body)
			err = pipeline.OcrPage(ctx, msg, conn, pipeline.Ocr(*training, ""), conn.OCRPageQueueId(), conn.AnalyseQueueId())
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				conn.Log("Error during OCR Page process", err)
			}
		case <-checkAnalyseQueue:
			msg, err := conn.CheckQueue(conn.AnalyseQueueId(), QueueTimeoutSecs)
			checkAnalyseQueue = time.After(PauseBetweenChecks)
			if err != nil {
				conn.Log("Error checking analyse queue", err)
				continue
			}
			if msg.Handle == "" {
				conn.Log("No message received on analyse queue, sleeping")
				continue
			}
			stopTimer(stopIfQuiet)
			conn.Log("Message received on analyse queue, processing", msg.Body)
			err = pipeline.ProcessBook(ctx, msg, conn, pipeline.Analyse(conn), ocredPattern, conn.AnalyseQueueId(), "")
			resetTimer(stopIfQuiet, quietTime)
			if err != nil {
				conn.Log("Error during analysis", err)
			}
		case <-savelognow.C:
			conn.Log("Saving logs")
			err = pipeline.SaveLogs(conn, starttime, hostname)
			if err != nil {
				conn.Log("Error saving logs", err)
			}
		case <-stopIfQuiet.C:
			if quietTime == 0 {
				continue
			}
			if !*autoshutdown {
				conn.Log("Stopping pipeline")
				_ = pipeline.SaveLogs(conn, starttime, hostname)
				return
			}
			conn.Log("Shutting down")
			_ = pipeline.SaveLogs(conn, starttime, hostname)
			cmd := exec.Command("sudo", "systemctl", "poweroff")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err != nil {
				conn.Log("Error shutting down, error:", err,
					", stdout:", stdout.String(), ", stderr:", stderr.String())
			}
		}
	}
}
