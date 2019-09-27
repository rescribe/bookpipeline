package bookpipeline

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
)

const maxticks = 40
const goodCutoff = 70
const mediumCutoff = 65
const badCutoff = 60

type Conf struct {
	Path, Code string
	Conf       float64
}

type GraphConf struct {
	Pgnum, Conf float64
}

func createLine(xvalues []float64, y float64, c drawing.Color) chart.ContinuousSeries {
	var yvalues []float64
	for range xvalues {
		yvalues = append(yvalues, y)
	}
	return chart.ContinuousSeries{
		XValues: xvalues,
		YValues: yvalues,
		Style: chart.Style{
			StrokeColor: c,
		},
	}
}

func Graph(confs map[string]*Conf, bookname string, w io.Writer) error {
	// Organise confs to sort them by page
	var graphconf []GraphConf
	for _, conf := range confs {
		name := filepath.Base(conf.Path)
		var numend int
		numend = strings.Index(name, "_")
		if numend == -1 {
			numend = strings.Index(name, ".")
		}
		pgnum, err := strconv.ParseFloat(name[0:numend], 64)
		if err != nil {
			continue
		}
		var c GraphConf
		c.Pgnum = pgnum
		c.Conf = conf.Conf
		graphconf = append(graphconf, c)
	}
	sort.Slice(graphconf, func(i, j int) bool { return graphconf[i].Pgnum < graphconf[j].Pgnum })

	// Create main xvalues and yvalues, annotations and ticks
	var xvalues, yvalues []float64
	var annotations []chart.Value2
	var ticks []chart.Tick
	tickevery := len(graphconf) / maxticks
	for i, c := range graphconf {
		xvalues = append(xvalues, c.Pgnum)
		yvalues = append(yvalues, c.Conf)
		if c.Conf < goodCutoff {
			annotations = append(annotations, chart.Value2{Label: fmt.Sprintf("%.0f", c.Pgnum), XValue: c.Pgnum, YValue: c.Conf})
		}
		if i%tickevery == 0 {
			ticks = append(ticks, chart.Tick{c.Pgnum, fmt.Sprintf("%.0f", c.Pgnum)})
		}
	}
	// make last tick the final page
	final := graphconf[len(graphconf)-1]
	ticks[len(ticks)-1] = chart.Tick{final.Pgnum, fmt.Sprintf("%.0f", final.Pgnum)}
	mainSeries := chart.ContinuousSeries{
		XValues: xvalues,
		YValues: yvalues,
	}

	// Create lines
	goodCutoffSeries := createLine(xvalues, goodCutoff, chart.ColorAlternateGreen)
	mediumCutoffSeries := createLine(xvalues, mediumCutoff, chart.ColorOrange)
	badCutoffSeries := createLine(xvalues, badCutoff, chart.ColorRed)

	// Create lines marking top and bottom 10% confidence
	sort.Slice(graphconf, func(i, j int) bool { return graphconf[i].Conf < graphconf[j].Conf })
	lowconf := graphconf[int(len(graphconf)/10)].Conf
	highconf := graphconf[int((len(graphconf)/10)*9)].Conf
	yvalues = []float64{}
	for range graphconf {
		yvalues = append(yvalues, lowconf)
	}
	minSeries := &chart.ContinuousSeries{
		Style: chart.Style{
			StrokeColor:     chart.ColorAlternateGray,
			StrokeDashArray: []float64{5.0, 5.0},
		},
		XValues: xvalues,
		YValues: yvalues,
	}
	yvalues = []float64{}
	for _ = range graphconf {
		yvalues = append(yvalues, highconf)
	}
	maxSeries := &chart.ContinuousSeries{
		Style: chart.Style{
			StrokeColor:     chart.ColorAlternateGray,
			StrokeDashArray: []float64{5.0, 5.0},
		},
		XValues: xvalues,
		YValues: yvalues,
	}

	graph := chart.Chart{
		Title:      bookname,
		Width:      3840,
		Height:     2160,
		XAxis: chart.XAxis{
			Name:      "Page number",
			Range: &chart.ContinuousRange{
				Min: 0.0,
			},
			Ticks: ticks,
		},
		YAxis: chart.YAxis{
			Name:      "Confidence",
			Range: &chart.ContinuousRange{
				Min: 0.0,
				Max: 100.0,
			},
		},
		Series: []chart.Series{
			mainSeries,
			minSeries,
			maxSeries,
			goodCutoffSeries,
			mediumCutoffSeries,
			badCutoffSeries,
			chart.AnnotationSeries{
				Annotations: annotations,
			},
		},
	}
	return graph.Render(chart.PNG, w)
}
