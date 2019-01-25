package prob

import (
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"

	"rescribe.xyz/go.git/lib/line"
)

func getLineAvg(f string) (float64, error) {
	totalconf := float64(0)
	num := 0

	prob, err := ioutil.ReadFile(f)
	if err != nil {
		return 0, err
	}

	for _, l := range strings.Split(string(prob), "\n") {
		fields := strings.Fields(l)

		if len(fields) == 2 {
			conf, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				continue
			}
			totalconf += conf
			num += 1
		}
	}
	if num <= 0 {
		return 0, nil
	}
	avg := totalconf / float64(num)
	return avg, nil
}

// Note this only processes one line at a time
func GetLineDetails(probfn string) (line.Details, error) {
	var l line.Detail
	lines := make(line.Details, 0)

	avg, err := getLineAvg(probfn)
	if err != nil {
		return lines, err
	}

	filebase := strings.Replace(probfn, ".prob", "", 1)

	txt, err := ioutil.ReadFile(filebase + ".txt")
	if err != nil {
		return lines, err
	}

	l.Name = filepath.Base(filebase)
	l.Avgconf = avg
	l.Text = string(txt)
	l.OcrName = filepath.Base(filepath.Dir(filebase))

	var imgfn line.ImgPath
	imgfn.Path = filebase + ".bin.png"
	l.Img = imgfn

	lines = append(lines, l)

	return lines, nil
}
