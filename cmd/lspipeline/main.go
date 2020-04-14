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

// getBookStatus returns a list of in progress and done books.
// It determines this by finding all prefixes, and splitting them
// into two lists, those which have a 'graph.png' file (the done
// list), and those which do not (the inprogress list). They are
// sorted according to the date of the graph.png file, or the date
// of a random file with the prefix if no graph.png was found.
func getBookStatus(conn LsPipeliner) (inprogress []string, done []string, err error) {
	prefixes, err := conn.ListObjectPrefixes(conn.WIPStorageId())
	var inprogressmeta, donemeta ObjMetas
	if err != nil {
		log.Println("Error getting object prefixes:", err)
		return
	}
	// Search for graph.png to determine done books (and save the date of it to sort with)
	for _, p := range prefixes {
		objs, err := conn.ListObjectsWithMeta(conn.WIPStorageId(), p+"graph.png")
		if err != nil || len(objs) == 0 {
			inprogressmeta = append(inprogressmeta, bookpipeline.ObjMeta{Name: p})
		} else {
			donemeta = append(donemeta, bookpipeline.ObjMeta{Name: p, Date: objs[0].Date})
		}
	}
	// Get a random file from the inprogress list to get a date to sort by
	for _, i := range inprogressmeta {
		objs, err := conn.ListObjectsWithMeta(conn.WIPStorageId(), i.Name)
		if err != nil || len(objs) == 0 {
			continue
		}
		i.Date = objs[0].Date
	}
	sort.Sort(donemeta)
	for _, i := range donemeta {
		done = append(done, strings.TrimSuffix(i.Name, "/"))
	}
	sort.Sort(inprogressmeta)
	for _, i := range inprogressmeta {
		inprogress = append(inprogress, strings.TrimSuffix(i.Name, "/"))
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
