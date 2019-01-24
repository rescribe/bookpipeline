package prob

import (
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"

	"git.rescribe.xyz/testingtools/parse"
)

func getLineAvg(f string) (float64, error) {
	totalconf := float64(0)
	num := 0

	prob, err := ioutil.ReadFile(f)
        if err != nil {
		return 0, err
	}

	for _, line := range strings.Split(string(prob), "\n") {
		fields := strings.Fields(line)

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
func GetLineDetails(probfn string) (parse.LineDetails, error) {
	var line parse.LineDetail
	lines := make(parse.LineDetails, 0)

	avg, err := getLineAvg(probfn)
	if err != nil {
		return lines, err
	}

	filebase := strings.Replace(probfn, ".prob", "", 1)

	txt, err := ioutil.ReadFile(filebase + ".txt")
	if err != nil {
		return lines, err
	}

	line.Name = filepath.Base(filebase)
	line.Avgconf = avg
	line.Text = string(txt)
	line.OcrName = filepath.Dir(filebase)

	var imgfn parse.ImgPath
	imgfn.Path = filebase + ".bin.png"
	line.Img = imgfn

	lines = append(lines, line)

	return lines, nil
}
