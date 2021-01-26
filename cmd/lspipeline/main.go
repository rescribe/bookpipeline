// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// lspipeline lists useful things related to the book pipeline.
package main

import (
	"flag"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: lspipeline [-i key] [-n num] [-nobooks]

Lists useful things related to the pipeline.

- Instances running
- Messages in each queue
- Books not completed
- Books done
- Last n lines of bookpipeline logs from each running instance
`

type LsPipeliner interface {
	Init() error
	PreQueueId() string
	WipeQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
	GetQueueDetails(url string) (string, string, error)
	GetInstanceDetails() ([]bookpipeline.InstanceDetails, error)
	ListObjectsWithMeta(bucket string, prefix string) ([]bookpipeline.ObjMeta, error)
	ListObjectPrefixes(bucket string) ([]string, error)
	WIPStorageId() string
}

// NullWriter is used so non-verbose logging may be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type queueDetails struct {
	name, numAvailable, numInProgress string
}

func getInstances(conn LsPipeliner, detailsc chan bookpipeline.InstanceDetails) {
	details, err := conn.GetInstanceDetails()
	if err != nil {
		log.Println("Error getting instance details:", err)
	}
	for _, d := range details {
		detailsc <- d
	}
	close(detailsc)
}

func getQueueDetails(conn LsPipeliner, qdetails chan queueDetails) {
	queues := []struct{ name, id string }{
		{"preprocess", conn.PreQueueId()},
		{"wipeonly", conn.WipeQueueId()},
		{"ocrpage", conn.OCRPageQueueId()},
		{"analyse", conn.AnalyseQueueId()},
	}
	for _, q := range queues {
		avail, inprog, err := conn.GetQueueDetails(q.id)
		if err != nil {
			log.Println("Error getting queue details:", err)
		}
		var qd queueDetails
		qd.name = q.name
		qd.numAvailable = avail
		qd.numInProgress = inprog
		qdetails <- qd
	}
	close(qdetails)
}

type ObjMetas []bookpipeline.ObjMeta

// used by sort.Sort
func (o ObjMetas) Len() int {
	return len(o)
}

// used by sort.Sort
func (o ObjMetas) Swap(i, j int) {
	o[i], o[j] = o[j], o[i]
}

// used by sort.Sort
func (o ObjMetas) Less(i, j int) bool {
	return o[i].Date.Before(o[j].Date)
}

// getBookDetails determines whether a book is done and what date
// it was completed, or if it has not finished, the date of any
// book file.
func getBookDetails(conn LsPipeliner, key string) (date time.Time, done bool, err error) {
	// First try to get the graph.png file from the book, which marks
	// it as done
	objs, err := conn.ListObjectsWithMeta(conn.WIPStorageId(), key+"graph.png")
	if err == nil && len(objs) > 0 {
		return objs[0].Date, true, nil
	}

	// Otherwise get any file from the book to get a date to sort by
	objs, err = conn.ListObjectsWithMeta(conn.WIPStorageId(), key)
	if err != nil {
		return time.Time{}, false, err
	}
	if len(objs) == 0 {
		return time.Time{}, false, fmt.Errorf("No files found for book %s", key)
	}
	return objs[0].Date, false, nil
}

// getBookDetailsWg gets the details for a book putting it into either the
// done or inprogress channels as appropriate, and using a sync.WaitGroup Done
// signal so it can be tracked. On error it sends to the errc channel.
func getBookDetailsWg(conn LsPipeliner, key string, done chan bookpipeline.ObjMeta, inprogress chan bookpipeline.ObjMeta, errc chan error, wg *sync.WaitGroup) {
	defer wg.Done()
	date, isdone, err := getBookDetails(conn, key)
	if err != nil {
		errc <- err
		return
	}
	meta := bookpipeline.ObjMeta{Name: strings.TrimSuffix(key, "/"), Date: date}
	if isdone {
		done <- meta
	} else {
		inprogress <- meta
	}
}

// getBookStatus returns a list of in progress and done books.
// It determines this by finding all prefixes, and splitting them
// into two lists, those which have a 'graph.png' file (the done
// list), and those which do not (the inprogress list). They are
// sorted according to the date of the graph.png file, or the date
// of a random file with the prefix if no graph.png was found.
func getBookStatus(conn LsPipeliner) (inprogress []string, done []string, err error) {
	prefixes, err := conn.ListObjectPrefixes(conn.WIPStorageId())
	if err != nil {
		log.Println("Error getting object prefixes:", err)
		return
	}

	// 100,000 size buffer is to ensure we never block, as we're using waitgroup
	// rather than channel blocking to determine when to continue. Probably there
	// is a better way to do this, though, like reading the done and inprogress
	// channels in a goroutine and doing wg.Done() when each is read there instead.
	donec := make(chan bookpipeline.ObjMeta, 100000)
	inprogressc := make(chan bookpipeline.ObjMeta, 100000)
	errc := make(chan error, 100000)

	var wg sync.WaitGroup
	for _, p := range prefixes {
		wg.Add(1)
		go getBookDetailsWg(conn, p, donec, inprogressc, errc, &wg)
	}
	wg.Wait()
	close(donec)
	close(inprogressc)

	select {
		case err = <-errc:
			return inprogress, done, err
		default:
			break
	}

	var inprogressmeta, donemeta ObjMetas

	for i := range donec {
		donemeta = append(donemeta, i)
	}
	for i := range inprogressc {
		inprogressmeta = append(inprogressmeta, i)
	}

	sort.Sort(donemeta)
	sort.Sort(inprogressmeta)

	for _, i := range donemeta {
		done = append(done, i.Name)
	}
	for _, i := range inprogressmeta {
		inprogress = append(inprogress, i.Name)
	}

	return
}

// getBookStatusChan runs getBookStatus and sends its results to
// channels for the done and receive arrays.
func getBookStatusChan(conn LsPipeliner, inprogressc chan string, donec chan string) {
	inprogress, done, err := getBookStatus(conn)
	if err != nil {
		log.Println("Error getting book status:", err)
		close(inprogressc)
		close(donec)
		return
	}
	for _, i := range inprogress {
		inprogressc <- i
	}
	close(inprogressc)
	for _, i := range done {
		donec <- i
	}
	close(donec)
}

func getRecentSSHLogs(ip string, id string, n int) (string, error) {
	addr := fmt.Sprintf("%s@%s", "admin", ip)
	logcmd := fmt.Sprintf("journalctl -n %d -u bookpipeline", n)
	var cmd *exec.Cmd
	if id == "" {
		cmd = exec.Command("ssh", "-o", "StrictHostKeyChecking no", addr, logcmd)
	} else {
		cmd = exec.Command("ssh", "-o", "StrictHostKeyChecking no", "-i", id, addr, logcmd)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func getRecentSSHLogsChan(ips []string, id string, lognum int, logs chan string) {
	for _, ip := range ips {
		sshlog, err := getRecentSSHLogs(ip, id, lognum)
		if err != nil {
			log.Printf("Error getting SSH logs for %s: %s\n", ip, err)
			continue
		}
		logs <- fmt.Sprintf("%s\n%s", ip, sshlog)
	}
	close(logs)
}

func main() {
	keyfile := flag.String("i", "", "private key file for SSH")
	lognum := flag.Int("n", 5, "number of lines to include in SSH logs")
	nobooks := flag.Bool("nobooks", false, "disable listing books completed and not completed (which takes some time)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	var verboselog *log.Logger
	var n NullWriter
	verboselog = log.New(n, "", 0)

	var conn LsPipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}
	err := conn.Init()
	if err != nil {
		log.Fatalln("Failed to set up cloud connection:", err)
	}

	instances := make(chan bookpipeline.InstanceDetails, 100)
	queues := make(chan queueDetails)
	inprogress := make(chan string, 100)
	done := make(chan string, 100)
	logs := make(chan string, 10)

	go getInstances(conn, instances)
	go getQueueDetails(conn, queues)
	if !*nobooks {
		go getBookStatusChan(conn, inprogress, done)
	}

	var ips []string

	fmt.Println("# Instances")
	for i := range instances {
		fmt.Printf("ID: %s, Type: %s, LaunchTime: %s, State: %s", i.Id, i.Type, i.LaunchTime, i.State)
		if i.Name != "" {
			fmt.Printf(", Name: %s", i.Name)
		}
		if i.Ip != "" {
			fmt.Printf(", IP: %s", i.Ip)
			if i.State == "running" && i.Name != "workhorse" {
				ips = append(ips, i.Ip)
			}
		}
		if i.Spot != "" {
			fmt.Printf(", SpotRequest: %s", i.Spot)
		}
		fmt.Printf("\n")
	}

	go getRecentSSHLogsChan(ips, *keyfile, *lognum, logs)

	fmt.Println("\n# Queues")
	for i := range queues {
		fmt.Printf("%s: %s available, %s in progress\n", i.name, i.numAvailable, i.numInProgress)
	}

	if len(ips) > 0 {
		fmt.Println("\n# Recent logs")
		for i := range logs {
			fmt.Printf("\n%s", i)
		}
	}

	if !*nobooks {
		fmt.Println("\n# Books not completed")
		for i := range inprogress {
			fmt.Println(i)
		}

		fmt.Println("\n# Books done")
		for i := range done {
			fmt.Println(i)
		}
	}
}
