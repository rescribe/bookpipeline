package main

// TODO: have logs go somewhere useful, like email
// TODO: check if images are prebinarised and if so skip multiple binarisation

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wcharczuk/go-chart"

	"rescribe.xyz/go.git/lib/hocr"
	"rescribe.xyz/go.git/preproc"
)

const usage = `Usage: bookpipeline [-v] [-t training]

Watches the preprocess, ocr and analyse queues for book names. When
one is found this general process is followed:

- The book name is hidden from the queue, and a 'heartbeat' is
  started which keeps it hidden (this will time out after 2 minutes
  if the program is terminated)
- The necessary files from bookname/ are downloaded
- The files are processed
- The resulting files are uploaded to bookname/
- The heartbeat is stopped
- The book name is removed from the queue it was taken from, and
  added to the next queue for future processing

`

const maxticks = 20
const cutoff = 70

// null writer to enable non-verbose logging to be discarded
type NullWriter bool

func (w NullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

const PauseBetweenChecks = 3 * time.Minute

type Clouder interface {
	Init() error
	ListObjects(bucket string, prefix string) ([]string, error)
	Download(bucket string, key string, fn string) error
	Upload(bucket string, key string, path string) error
	CheckQueue(url string) (Qmsg, error)
	AddToQueue(url string, msg string) error
	DelFromQueue(url string, handle string) error
	QueueHeartbeat(t *time.Ticker, msgHandle string, qurl string) error
}

type Pipeliner interface {
	Clouder
	PreQueueId() string
	OCRQueueId() string
	AnalyseQueueId() string
	WIPStorageId() string
	Logger() *log.Logger
}

type Qmsg struct {
	Handle, Body string
}

func download(dl chan string, process chan string, conn Pipeliner, dir string, errc chan error) {
	for key := range dl {
		fn := filepath.Join(dir, filepath.Base(key))
		err := conn.Download(conn.WIPStorageId(), key, fn)
		if err != nil {
			close(process)
			errc <- err
			return
		}
		process <- fn
	}
	close(process)
}

func up(c chan string, done chan bool, conn Pipeliner, bookname string, errc chan error) {
	for path := range c {
		name := filepath.Base(path)
		key := filepath.Join(bookname, name)
		err := conn.Upload(conn.WIPStorageId(), key, path)
		if err != nil {
			errc <- err
			return
		}
	}

	done <- true
}

func preprocess(pre chan string, up chan string, errc chan error, logger *log.Logger) {
	for path := range pre {
		logger.Println("Preprocessing", path)
		done, err := preproc.PreProcMulti(path, []float64{0.1, 0.2, 0.4, 0.5}, "binary", 0, true, 5, 30)
		if err != nil {
			close(up)
			errc <- err
			return
		}
		for _, p := range done {
			up <- p
		}
	}
	close(up)
}

func ocr(training string) func(chan string, chan string, chan error, *log.Logger) {
	return func (toocr chan string, up chan string, errc chan error, logger *log.Logger) {
		for path := range toocr {
			logger.Println("OCRing", path)
			name := strings.Replace(path, ".png", "", 1)
			cmd := exec.Command("tesseract", "-l", training, path, name, "hocr")
			err := cmd.Run()
			if err != nil {
				close(up)
				errc <- errors.New(fmt.Sprintf("Error ocring %s: %s", path, err))
				return
			}
			up <- name + ".hocr"
		}
		close(up)
	}
}

type Conf struct {
	path, code string
	conf float64
}
type GraphConf struct {
	pgnum, conf float64
}

func graph(confs map[string]*Conf, bookname string, w io.Writer) (error) {
	// Organise confs to sort them by page
	var graphconf []GraphConf
	for _, conf := range confs {
		name := filepath.Base(conf.path)
		numend := strings.Index(name, "_")
		pgnum, err := strconv.ParseFloat(name[0:numend], 64)
		if err != nil {
			continue
		}
		var c GraphConf
		c.pgnum = pgnum
		c.conf = conf.conf
		graphconf = append(graphconf, c)
	}
	sort.Slice(graphconf, func(i, j int) bool { return graphconf[i].pgnum < graphconf[j].pgnum })

	// Create main xvalues and yvalues, annotations and ticks
	var xvalues, yvalues []float64
	var annotations []chart.Value2
	var ticks []chart.Tick
	i := 0
	tickevery := len(graphconf) / maxticks
	for _, c := range graphconf {
		i = i + 1
		xvalues = append(xvalues, c.pgnum)
		yvalues = append(yvalues, c.conf)
		if c.conf < cutoff {
			annotations = append(annotations, chart.Value2{Label: fmt.Sprintf("%.0f", c.pgnum), XValue: c.pgnum, YValue: c.conf})
		}
		if tickevery % i == 0 {
			ticks = append(ticks, chart.Tick{c.pgnum, fmt.Sprintf("%.0f", c.pgnum)})
		}
	}
	mainSeries := chart.ContinuousSeries{
		XValues: xvalues,
		YValues: yvalues,
	}

	// Create 70% line
	yvalues = []float64{}
	for _, _ = range xvalues {
		yvalues = append(yvalues, cutoff)
	}
	cutoffSeries := chart.ContinuousSeries{
		XValues: xvalues,
		YValues: yvalues,
		Style: chart.Style{
			Show:            true,
			StrokeColor:     chart.ColorAlternateGreen,
			StrokeDashArray: []float64{10.0, 5.0},
		},
	}

	// Create lines marking top and bottom 10% confidence
	sort.Slice(graphconf, func(i, j int) bool { return graphconf[i].conf < graphconf[j].conf })
	cutoff := int(len(graphconf) / 10)
	mostconf := graphconf[cutoff:len(graphconf)-cutoff]
	sort.Slice(mostconf, func(i, j int) bool { return mostconf[i].pgnum < mostconf[j].pgnum })
	xvalues = []float64{}
	yvalues = []float64{}
	for _, c := range mostconf {
		xvalues = append(xvalues, c.pgnum)
		yvalues = append(yvalues, c.conf)
	}
	mostSeries := chart.ContinuousSeries{
		XValues: xvalues,
		YValues: yvalues,
	}
	minSeries := &chart.MinSeries{
		Style: chart.Style{
			Show:            true,
			StrokeColor:     chart.ColorAlternateGray,
			StrokeDashArray: []float64{5.0, 5.0},
		},
		InnerSeries: mostSeries,
	}
	maxSeries := &chart.MaxSeries{
		Style: chart.Style{
			Show:            true,
			StrokeColor:     chart.ColorAlternateGray,
			StrokeDashArray: []float64{5.0, 5.0},
		},
		InnerSeries: mostSeries,
	}

	graph := chart.Chart{
		Title: fmt.Sprintf("Confidence of pages from %s", bookname),
		TitleStyle: chart.StyleShow(),
		Width: 1920,
		Height: 1080,
		XAxis: chart.XAxis{
			Name: "Page number",
			NameStyle: chart.StyleShow(),
			Style: chart.StyleShow(),
			Range: &chart.ContinuousRange{
				Min: 0.0,
			},
			Ticks: ticks,
		},
		YAxis: chart.YAxis{
			Name: "Confidence",
			NameStyle: chart.StyleShow(),
			Style: chart.StyleShow(),
			Range: &chart.ContinuousRange{
				Min: 0.0,
				Max: 100.0,
			},
		},
		Series: []chart.Series{
			mainSeries,
			minSeries,
			maxSeries,
			cutoffSeries,
			chart.LastValueAnnotation(minSeries),
			chart.LastValueAnnotation(maxSeries),
			chart.AnnotationSeries{
				Annotations: annotations,
			},
			//chart.ContinuousSeries{
			//	YAxis: chart.YAxisSecondary,
			//	XValues: xvalues,
			//	YValues: yvalues,
			//},
		},
	}
	return graph.Render(chart.PNG, w)
}

func analyse(toanalyse chan string, up chan string, errc chan error, logger *log.Logger) {
	confs := make(map[string][]*Conf)
	bestconfs := make(map[string]*Conf)
	savedir := ""

	for path := range toanalyse {
		if savedir == "" {
			savedir = filepath.Dir(path)
		}
		logger.Println("Calculating confidence for", path)
		avg, err := hocr.GetAvgConf(path)
		if err != nil {
			close(up)
			errc <- errors.New(fmt.Sprintf("Error retreiving confidence for %s: %s", path, err))
			return
		}
		base := filepath.Base(path)
		codestart := strings.Index(base, "_bin")
		name := base[0:codestart]
		var c Conf
		c.path = path
		c.code = base[codestart:]
		c.conf = avg
		confs[name] = append(confs[name], &c)

	}

	fn := filepath.Join(savedir, "conf")
	logger.Println("Saving confidences in file", fn)
	f, err := os.Create(fn)
	if err != nil {
		close(up)
		errc <- errors.New(fmt.Sprintf("Error creating file %s: %s", fn, err))
		return
	}
	defer f.Close()

	logger.Println("Finding best confidence for each page, and saving all confidences")
	for base, conf := range confs {
		var best float64
		for _, c := range conf {
			if c.conf > best {
				best = c.conf
				bestconfs[base] = c
			}
			_, err = fmt.Fprintf(f, "%s\t%02.f\n", c.path, c.conf)
			if err != nil {
				close(up)
				errc <- errors.New(fmt.Sprintf("Error writing confidences file: %s", err))
				return
			}
		}
	}
	up <- fn

	logger.Println("Creating best file listing the best file for each page")
	fn = filepath.Join(savedir, "best")
	f, err = os.Create(fn)
	if err != nil {
		close(up)
		errc <- errors.New(fmt.Sprintf("Error creating file %s: %s", fn, err))
		return
	}
	defer f.Close()
	for _, conf := range bestconfs {
		_, err = fmt.Fprintf(f, "%s\n", filepath.Base(conf.path))
	}
	up <- fn

	logger.Println("Creating graph")
	fn = filepath.Join(savedir, "graph.png")
	f, err = os.Create(fn)
	if err != nil {
		close(up)
		errc <- errors.New(fmt.Sprintf("Error creating file %s: %s", fn, err))
		return
	}
	defer f.Close()
	err = graph(bestconfs, filepath.Base(savedir), f)
	if err != nil {
		close(up)
		errc <- errors.New(fmt.Sprintf("Error rendering graph: %s", err))
		return
	}
	up <- fn

	// TODO: generate a general report.txt with statistics etc for the book, send to up

	close(up)
}

func processBook(msg Qmsg, conn Pipeliner, process func(chan string, chan string, chan error, *log.Logger), match *regexp.Regexp, fromQueue string, toQueue string) error {
	bookname := msg.Body

	t := time.NewTicker(HeartbeatTime * time.Second)
	go conn.QueueHeartbeat(t, msg.Handle, fromQueue)

	d := filepath.Join(os.TempDir(), bookname)
	err := os.MkdirAll(d, 0755)
	if err != nil {
		t.Stop()
		return errors.New(fmt.Sprintf("Failed to create directory %s: %s", d, err))
	}

	dl := make(chan string)
	processc := make(chan string)
	upc := make(chan string)
	done := make(chan bool)
	errc := make(chan error)

	// these functions will do their jobs when their channels have data
	go download(dl, processc, conn, d, errc)
	go process(processc, upc, errc, conn.Logger())
	go up(upc, done, conn, bookname, errc)

	conn.Logger().Println("Getting list of objects to download")
	objs, err := conn.ListObjects(conn.WIPStorageId(), bookname)
	if err != nil {
		t.Stop()
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Failed to get list of files for book %s: %s", bookname, err))
	}
	var todl []string
	for _, n := range objs {
		if !match.MatchString(n) {
			conn.Logger().Println("Skipping item that doesn't match target", n)
			continue
		}
		todl = append(todl, n)
	}
	for _, a := range todl {
		dl <- a
	}
	close(dl)

	// wait for either the done or errc channel to be sent to
	select {
	case err = <-errc:
		t.Stop()
		_ = os.RemoveAll(d)
		return err
	case <-done:
	}

	if toQueue != "" {
		conn.Logger().Println("Sending", bookname, "to queue")
		err = conn.AddToQueue(toQueue, bookname)
		if err != nil {
			t.Stop()
			_ = os.RemoveAll(d)
			return errors.New(fmt.Sprintf("Error adding to queue %s: %s", bookname, err))
		}
	}

	t.Stop()

	conn.Logger().Println("Deleting original message from queue")
	err = conn.DelFromQueue(fromQueue, msg.Handle)
	if err != nil {
		_ = os.RemoveAll(d)
		return errors.New(fmt.Sprintf("Error deleting message from queue: %s", err))
	}

	err = os.RemoveAll(d)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to remove directory %s: %s", d, err))
	}

	return nil
}

func main() {
	verbose := flag.Bool("v", false, "verbose")
	training := flag.String("t", "rescribealphav5", "tesseract training file to use")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	var verboselog *log.Logger
	if *verbose {
		verboselog = log.New(os.Stdout, "", log.LstdFlags)
	} else {
		var n NullWriter
		verboselog = log.New(n, "", log.LstdFlags)
	}

	origPattern := regexp.MustCompile(`[0-9]{4}.jpg$`) // TODO: match alternative file naming
	preprocessedPattern := regexp.MustCompile(`_bin[0-9].[0-9].png$`)
	ocredPattern := regexp.MustCompile(`.hocr$`)

	var conn Pipeliner
	conn = &awsConn{region: "eu-west-2", logger: verboselog}

	verboselog.Println("Setting up AWS session")
	err := conn.Init()
	if err != nil {
		log.Fatalln("Error setting up cloud connection:", err)
	}
	verboselog.Println("Finished setting up AWS session")

	var checkPreQueue <-chan time.Time
	var checkOCRQueue <-chan time.Time
	var checkAnalyseQueue <-chan time.Time
	checkPreQueue = time.After(0)
	checkOCRQueue = time.After(0)
	checkAnalyseQueue = time.After(0)

	for {
		select {
		case <-checkPreQueue:
			msg, err := conn.CheckQueue(conn.PreQueueId())
			checkPreQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking preprocess queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on preprocess queue, sleeping")
				continue
			}
			err = processBook(msg, conn, preprocess, origPattern, conn.PreQueueId(), conn.OCRQueueId())
			if err != nil {
				log.Println("Error during preprocess", err)
			}
		case <-checkOCRQueue:
			msg, err := conn.CheckQueue(conn.OCRQueueId())
			checkOCRQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking OCR queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on OCR queue, sleeping")
				continue
			}
			err = processBook(msg, conn, ocr(*training), preprocessedPattern, conn.OCRQueueId(), conn.AnalyseQueueId())
			if err != nil {
				log.Println("Error during OCR process", err)
			}
		case <-checkAnalyseQueue:
			msg, err := conn.CheckQueue(conn.AnalyseQueueId())
			checkAnalyseQueue = time.After(PauseBetweenChecks)
			if err != nil {
				log.Println("Error checking analyse queue", err)
				continue
			}
			if msg.Handle == "" {
				verboselog.Println("No message received on analyse queue, sleeping")
				continue
			}
			err = processBook(msg, conn, analyse, ocredPattern, conn.AnalyseQueueId(), "")
			if err != nil {
				log.Println("Error during analysis", err)
			}
		}
	}
}
