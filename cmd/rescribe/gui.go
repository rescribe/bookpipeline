// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var progressPoints = map[float64]string{
	0.2: "Preprocessing",
	0.5: "OCRing",
	0.9: "Analysing",
	1.0: "Done",
}

// copyStdoutToChan creates a pipe to copy anything written
// to the file also to a rune channel.
func copyStdoutToChan() (chan rune, error) {
	c := make(chan rune)

	origFile := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return c, fmt.Errorf("Error creating pipe for file redirection: %v", err)
	}
	os.Stdout = w

	bufReader := bufio.NewReader(r)

	go func() {
		defer func() {
			close(c)
			w.Close()
			os.Stdout = origFile
		}()
		for {
			r, _, err := bufReader.ReadRune()
			if err != nil && err != io.EOF {
				return
			}
			c <- r
			if err == io.EOF {
				return
			}
		}
	}()

	return c, nil
}

// copyStderrToChan creates a pipe to copy anything written
// to the file also to a rune channel.
// TODO: would be nice to merge this with copyStdoutToChan,
//       but a naive version using *os.File didn't work.
func copyStderrToChan() (chan rune, error) {
	c := make(chan rune)

	origFile := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return c, fmt.Errorf("Error creating pipe for file redirection: %v", err)
	}
	os.Stderr = w

	bufReader := bufio.NewReader(r)

	go func() {
		defer func() {
			close(c)
			w.Close()
			os.Stderr = origFile
		}()
		for {
			r, _, err := bufReader.ReadRune()
			if err != nil && err != io.EOF {
				return
			}
			c <- r
			if err == io.EOF {
				return
			}
		}
	}()

	return c, nil
}

// trainingSelectOnChange is a closure to handle change of the training
// select box. It does nothing in most cases, but if "Other..." has been
// selected, then it pops up a file chooser and adds the result to the
// list, also copying the file to the TESSDATA_PREFIX, and selects it.
func trainingSelectOnChange(sel *widget.Select, parent fyne.Window) func(string) {
	return func(str string) {
		if sel == nil {
			return
		}
		if str != "Other..." {
			return
		}
		d := dialog.NewFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err != nil || uri == nil {
				return
			}
			defer uri.Close()
			name := uri.URI().Name()
			newpath := filepath.Join(os.Getenv("TESSDATA_PREFIX"), name)
			f, err := os.Create(newpath)
			if err != nil {
				// TODO: surface error somewhere, prob with a dialog box
				return
			}
			defer f.Close()
			_, err = io.Copy(f, uri)
			if err != nil {
				// TODO: surface error somewhere, prob with a dialog box
				return
			}

			basicname := strings.TrimSuffix(name, ".traineddata")
			opts := append([]string{basicname}, sel.Options...)
			sel.Options = opts
			sel.SetSelectedIndex(0)
		}, parent)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".traineddata"}))
		d.Show()
	}
}

// mkTrainingSelect returns a select widget with all training
// files in TESSDATA_PREFIX/training, any other trainings listed
// in the extras slice, selecting the first entry.
func mkTrainingSelect(extras []string, parent fyne.Window) *widget.Select {
	prefix := os.Getenv("TESSDATA_PREFIX")
	trainings, err := filepath.Glob(prefix + "/*.traineddata")
	if err != nil {
		trainings = []string{}
	}
	for i, v := range trainings {
		trainings[i] = strings.TrimSuffix(strings.TrimPrefix(v, prefix), ".traineddata")
	}

	opts := append(extras, trainings...)
	opts = append(opts, "Other...")
	s := widget.NewSelect(opts, func(string) {})
	// OnChanged is set outside of NewSelect so the reference to s isn't nil
	s.OnChanged = trainingSelectOnChange(s, parent)
	s.SetSelectedIndex(0)
	return s
}

// formatProgressBarText uses the progressPoints map to set the text for the progress bar
// appropriately
func formatProgressBarText(bar *widget.ProgressBar) func() string {
	return func() string {
		for i, v := range progressPoints {
			if bar.Value == i {
				return v
			}
		}
		// OCRing gets special treatment as the bar can be updated within the range
		if bar.Value >= 0.5 && bar.Value < 0.9 {
			return progressPoints[0.5]
		}
		if bar.Value == 0 {
			return ""
		}
		return "Processing"
	}
}

// startGui starts the gui process
func startGui(log log.Logger, cmd string, training string, tessdir string) error {
	myApp := app.New()
	myWindow := myApp.NewWindow("Rescribe OCR")

	myWindow.Resize(fyne.NewSize(800, 400))

	var gobtn *widget.Button
	var fullContent *fyne.Container

	dir := widget.NewLabel("")

	dirIcon := widget.NewIcon(theme.FolderIcon())

	folderBtn := widget.NewButtonWithIcon("Choose folder", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err == nil && uri != nil {
				dir.SetText(uri.Path())
				dirIcon.SetResource(theme.FolderIcon())
				myWindow.SetContent(fullContent)
				gobtn.Enable()
			}
		}, myWindow)
	})

	pdfBtn := widget.NewButtonWithIcon("Choose PDF", theme.DocumentIcon(), func() {
		d := dialog.NewFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err == nil && uri != nil {
				uri.Close()
				dir.SetText(uri.URI().Path())
				dirIcon.SetResource(theme.DocumentIcon())
				myWindow.SetContent(fullContent)
				gobtn.Enable()
			}
		}, myWindow)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".pdf"}))
		d.Show()
	})

	gbookBtn := widget.NewButtonWithIcon("Get Google Book", theme.SearchIcon(), func() {
			// TODO
	})

	trainingLabel := widget.NewLabel("Training")

	trainingOpts := mkTrainingSelect([]string{training}, myWindow)

	progressBar := widget.NewProgressBar()
	progressBar.TextFormatter = formatProgressBarText(progressBar)

	logarea := widget.NewMultiLineEntry()
	logarea.Disable()

	gobtn = widget.NewButtonWithIcon("Start OCR", theme.UploadIcon(), func() {
		if dir.Text == "" {
			return
		}

		gobtn.Disable()

		progressBar.SetValue(0.1)

		stdout, err := copyStdoutToChan()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error copying stdout to chan: %v\n", err)
			return
		}

		// update log area with stdout in a concurrent goroutine, and parse it to update the progress bar
		go func() {
			for r := range stdout {
				logarea.SetText(logarea.Text + string(r))
				logarea.CursorRow = strings.Count(logarea.Text, "\n")

				lines := strings.Split(logarea.Text, "\n")
				lastline := lines[len(lines) - 1]
				for i, v := range progressPoints {
					if strings.HasPrefix(lastline, "  " + v) {
						// OCRing has a number of dots after it showing how many pages have been processed,
						// which we can use to update progress bar more often
						// TODO: calculate number of pages we expect, so this can be set accurately
						if v == "OCRing" {
							if progressBar.Value < 0.5 {
								progressBar.SetValue(0.5)
							}
							numdots := strings.Count(lastline, ".")
							newval := float64(0.5) + (float64(numdots) * float64(0.01))
							if newval >= 0.9 {
								newval = 0.89
							}
							progressBar.SetValue(newval)
							break
						}
						progressBar.SetValue(i)
					}
				}
			}
		}()

		stderr, err := copyStderrToChan()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error copying stderr to chan: %v\n", err)
			return
		}

		// update log area with stderr in a concurrent goroutine
		go func() {
			for r := range stderr {
				logarea.SetText(logarea.Text + string(r))
				logarea.CursorRow = strings.Count(logarea.Text, "\n")
			}
		}()

		err = startProcess(log, cmd, dir.Text, filepath.Base(dir.Text), trainingOpts.Selected, dir.Text, tessdir)
		if err != nil {
			// add a newline before this printing as another message from stdout
			// or stderr may well be half way through printing
			logarea.SetText(logarea.Text + fmt.Sprintf("\nError executing process: %v\n", err))
			logarea.CursorRow = strings.Count(logarea.Text, "\n")
			progressBar.SetValue(0.0)
			gobtn.SetText("Process OCR")
			gobtn.Enable()
			return
		}

		progressBar.SetValue(1.0)
		gobtn.SetText("Process OCR")
		gobtn.Enable()
	})
	gobtn.Disable()

	choices := container.New(layout.NewGridLayout(3), folderBtn, pdfBtn, gbookBtn)

	chosen := container.New(layout.NewBorderLayout(nil, nil, dirIcon, nil), dirIcon, dir)

	trainingBits := container.New(layout.NewBorderLayout(nil, nil, trainingLabel, nil), trainingLabel, trainingOpts)

	fullContent = container.NewVBox(choices, chosen, trainingBits, gobtn, progressBar, logarea)
	startContent := container.NewVBox(choices, trainingBits, gobtn, progressBar, logarea)

	myWindow.SetContent(startContent)

	myWindow.Show()
	myApp.Run()

	return nil
}
