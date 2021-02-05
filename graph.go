// Copyright 2019 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package bookpipeline

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

const maxticks = 40
const goodCutoff = 70
const mediumCutoff = 65
const badCutoff = 60
const yticknum = 40

type Conf struct {
	Path, Code string
	Conf       float64
}

type GraphConf struct {
	Pgnum, Conf float64
}

// createLine creates a horizontal line with a particular y value for
// a graph
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

// Graph creates a graph of the confidence of each page in a book
func Graph(confs map[string]*Conf, bookname string, w io.Writer) error {
	return GraphOpts(confs, bookname, "Page number", true, w)
}

// GraphOpts creates a graph of confidences
func GraphOpts(confs map[string]*Conf, bookname string, xaxis string, guidelines bool, w io.Writer) error {
	if len(confs) < 2 {
		return errors.New("Not enough valid confidences")
	}

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

	// If we failed to get any page numbers, just fake the lot
	if len(graphconf) == 0 {
		i := float64(1)
		for _, conf := range confs {
			var c GraphConf
			c.Pgnum = i
			c.Conf = conf.Conf
			graphconf = append(graphconf, c)
			i++
		}
	}

	sort.Slice(graphconf, func(i, j int) bool { return graphconf[i].Pgnum < graphconf[j].Pgnum })

	// Create main xvalues, yvalues ticks
	var xvalues, yvalues []float64
	var ticks []chart.Tick
	var yticks []chart.Tick
	tickevery := len(graphconf) / maxticks
	if tickevery < 1 {
		tickevery = 1
	}
	for i, c := range graphconf {
		xvalues = append(xvalues, c.Pgnum)
		yvalues = append(yvalues, c.Conf)
		if i%tickevery == 0 {
			ticks = append(ticks, chart.Tick{c.Pgnum, fmt.Sprintf("%.0f", c.Pgnum)})
		}
	}
	// Make last tick the final page
	final := graphconf[len(graphconf)-1]
	ticks[len(ticks)-1] = chart.Tick{final.Pgnum, fmt.Sprintf("%.0f", final.Pgnum)}
	for i := 0; i <= yticknum; i++ {
		n := float64(i*100) / yticknum
		yticks = append(yticks, chart.Tick{n, fmt.Sprintf("%.1f", n)})
	}

	mainSeries := chart.ContinuousSeries{
		Style: chart.Style{
			StrokeColor: chart.ColorBlue,
			FillColor:   chart.ColorAlternateBlue,
		},
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
	for range graphconf {
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

	// Create annotations
	var annotations []chart.Value2
	for _, c := range graphconf {
		if !guidelines || (c.Conf > highconf || c.Conf < lowconf) {
			annotations = append(annotations, chart.Value2{Label: fmt.Sprintf("%.0f", c.Pgnum), XValue: c.Pgnum, YValue: c.Conf})
		}
	}
	annotations = append(annotations, chart.Value2{Label: fmt.Sprintf("%.0f", lowconf), XValue: xvalues[len(xvalues)-1], YValue: lowconf})
	annotations = append(annotations, chart.Value2{Label: fmt.Sprintf("%.0f", highconf), XValue: xvalues[len(xvalues)-1], YValue: highconf})

	graph := chart.Chart{
		Title:  bookname,
		Width:  3840,
		Height: 2160,
		XAxis: chart.XAxis{
			Name: xaxis,
			Range: &chart.ContinuousRange{
				Min: 0.0,
			},
			Ticks: ticks,
		},
		YAxis: chart.YAxis{
			Name: "Confidence",
			Range: &chart.ContinuousRange{
				Min: 0.0,
				Max: 100.0,
			},
			Ticks: yticks,
		},
		Series: []chart.Series{
			mainSeries,
			chart.AnnotationSeries{
				Annotations: annotations,
			},
		},
	}
	if guidelines {
		for _, s := range []chart.Series{
			minSeries,
			maxSeries,
			goodCutoffSeries,
			mediumCutoffSeries,
			badCutoffSeries,
		} {
			graph.Series = append(graph.Series, s)
		}
	}
	return graph.Render(chart.PNG, w)
}
