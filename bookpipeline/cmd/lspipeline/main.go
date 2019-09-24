package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"rescribe.xyz/go.git/bookpipeline"
)

const usage = `Usage: lspipeline [-v]

Lists useful things related to the pipeline.

- Instances running
- Messages in each queue
- Books not completed
- Books done
- Last 5 lines of bookpipeline logs from each running instance (with -v)
`

type LsPipeliner interface {
	Init() error
	PreQueueId() string
	OCRQueueId() string
	AnalyseQueueId() string
	GetQueueDetails(url string) (string, string, error)
	GetInstanceDetails() ([]bookpipeline.InstanceDetails, error)
	ListObjects(bucket string, prefix string) ([]string, error)
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
		{"ocr", conn.OCRQueueId()},
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

// getBookStatus returns a list of in progress and done books.
// It determines this by listing all objects, and splitting the
// prefixes into two lists, those which have a 'graph.png' file,
// which are classed as done, and those which are not.
func getBookStatus(conn LsPipeliner) (inprogress []string, done []string, err error) {
	allfiles, err := conn.ListObjects(conn.WIPStorageId(), "")
	if err != nil {
		log.Println("Error getting list of objects:", err)
		return inprogress, done, err
	}
	for _, f := range allfiles {
		parts := strings.Split(f, "/")
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
		parts := strings.Split(f, "/")
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

func main() {
	verbose := flag.Bool("v", false, "verbose")
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

	var conn LsPipeliner
	conn = &bookpipeline.AwsConn{Region: "eu-west-2", Logger: verboselog}
	err := conn.Init()
	if err != nil {
		log.Fatalln("Failed to set up cloud connection:", err)
	}

	instances := make(chan bookpipeline.InstanceDetails, 100)
	queues := make(chan queueDetails)
	inprogress := make(chan string)
	done := make(chan string)

	go getInstances(conn, instances)
	go getQueueDetails(conn, queues)
	go getBookStatusChan(conn, inprogress, done)

	fmt.Println("# Instances")
	for i := range instances {
		fmt.Printf("ID: %s, Type: %s, LaunchTime: %s, State: %s", i.Id, i.Type, i.LaunchTime, i.State)
		if i.Name != "" {
			fmt.Printf(", Name: %s", i.Name)
		}
		if i.Ip != "" {
			fmt.Printf(", IP: %s", i.Ip)
		}
		if i.Spot != "" {
			fmt.Printf(", SpotRequest: %s", i.Spot)
		}
		fmt.Printf("\n")
	}

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

	// TODO: See remaining items in the usage statement
}
