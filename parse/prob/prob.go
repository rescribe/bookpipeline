package prob

import (
	"bufio"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"

	"git.rescribe.xyz/testingtools/parse"
)

// TODO: probably switch to just relying on io.Reader
func getLineAvg(r *bufio.Reader) (float64, error) {
	var err error

	totalconf := float64(0)
	num := 0

	err = nil
	for err == nil {
		var line string
		line, err = r.ReadString('\n')
		fields := strings.Fields(line)

		if len(fields) == 2 {
			conf, converr := strconv.ParseFloat(fields[1], 64)
			if converr != nil {
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

// TODO: probably switch to just relying on io.Reader
// Note this only processes one line at a time
func GetLineDetails(name string, r *bufio.Reader) (parse.LineDetails, error) {
	var line parse.LineDetail
	lines := make(parse.LineDetails, 0)

	avg, err := getLineAvg(r)
	if err != nil {
		return lines, err
	}

	filebase := strings.Replace(name, ".prob", "", 1)

	txt, err := ioutil.ReadFile(filebase + ".txt")
	if err != nil {
		return lines, err
	}

	line.Name = name
	line.Avgconf = avg
	line.Text = string(txt)
	line.OcrName = filepath.Dir(filebase)

	var imgfn parse.ImgPath
	imgfn.Path = filebase + ".bin.png"
	line.Img = imgfn

	lines = append(lines, line)

	return lines, nil
}
