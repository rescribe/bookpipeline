package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"

	"rescribe.xyz/bookpipeline"
)

const usage = `Usage: lspipeline [-i key] [-n num]

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
	OCRQueueId() string
	OCRPageQueueId() string
	AnalyseQueueId() string
	GetQueueDetails(url string) (string, string, error)
	GetInstanceDetails() ([]bookpipeline.InstanceDetails, error)
	ListObjectsWithMeta(bucket string, prefix string) ([]bookpipeline.ObjMeta, error)
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
		{"ocr", conn.OCRQueueId()},
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

// sortBookList sorts a list of book names by date.
// It uses a list of filenames and dates in an ObjMeta slice to
// determine the date for a book name.
func sortBookList(list []string, fileinfo []bookpipeline.ObjMeta) ([]string, error) {
	var listinfo ObjMetas

	for _, name := range list {
		found := false
		for _, f := range fileinfo {
			parts := strings.Split(f.Name, "/")
			prefix := parts[0]
			if name == prefix {
				listinfo = append(listinfo, bookpipeline.ObjMeta{Name: name, Date: f.Date})
				found = true
				break
			}
		}
		if !found {
			return list, errors.New("Failed to find metadata for list")
		}
	}

	// sort listinfo by date
	sort.Sort(listinfo)

	var l []string
	for _, i := range listinfo {
		l = append(l, i.Name)
	}
	return l, nil
}

// getBookStatus returns a list of in progress and done books.
// It determines this by listing all objects, and splitting the
// prefixes into two lists, those which have a 'graph.png' file,
// which are classed as done, and those which are not. These are
// sorted by date according to file metadata.
func getBookStatus(conn LsPipeliner) (inprogress []string, done []string, err error) {
	allfiles, err := conn.ListObjectsWithMeta(conn.WIPStorageId(), "")
	if err != nil {
		log.Println("Error getting list of objects:", err)
		return inprogress, done, err
	}
	for _, f := range allfiles {
		parts := strings.Split(f.Name, "/")
		if parts[1] != "graph.png" {
			continue
		}
		prefix := parts[0]
		found := false
		for _, i := range done {
			if i == prefix {
				found = true
				continue
			}
		}
		if !found {
			done = append(done, prefix)
		}
	}

	for _, f := range allfiles {
		parts := strings.Split(f.Name, "/")
		prefix := parts[0]
		found := false
		for _, i := range done {
			if i == prefix {
				found = true
				continue
			}
		}
		for _, i := range inprogress {
			if i == prefix {
				found = true
				continue
			}
		}
		if !found {
			inprogress = append(inprogress, prefix)
		}
	}

	inprogress, err = sortBookList(inprogress, allfiles)
	if err != nil {
		log.Println("Error sorting list of objects:", err)
		err = nil
	}
	done, err = sortBookList(done, allfiles)
	if err != nil {
		log.Println("Error sorting list of objects:", err)
		err = nil
	}

	return inprogress, done, err
}

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
	go getBookStatusChan(conn, inprogress, done)

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

	fmt.Println("\n# Books not completed")
	for i := range inprogress {
		fmt.Println(i)
	}

	fmt.Println("\n# Books done")
	for i := range done {
		fmt.Println(i)
	}

	if len(ips) > 0 {
		fmt.Println("\n# Recent logs")
		for i := range logs {
			fmt.Printf("\n%s", i)
		}
	}
}
