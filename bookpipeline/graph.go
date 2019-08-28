package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wcharczuk/go-chart"
)

const maxticks = 20
const cutoff = 70

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
	for _ = range xvalues {
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
	lowconf := graphconf[int(len(graphconf) / 10)].conf
	highconf := graphconf[int((len(graphconf) / 10) * 9)].conf
	yvalues = []float64{}
	for _ = range graphconf {
		yvalues = append(yvalues, lowconf)
	}
	minSeries := &chart.ContinuousSeries{
		Style: chart.Style{
			Show:            true,
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
			Show:            true,
			StrokeColor:     chart.ColorAlternateGray,
			StrokeDashArray: []float64{5.0, 5.0},
		},
		XValues: xvalues,
		YValues: yvalues,
	}

	graph := chart.Chart{
		Title: bookname,
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
			chart.AnnotationSeries{
				Annotations: annotations,
			},
		},
	}
	return graph.Render(chart.PNG, w)
}
