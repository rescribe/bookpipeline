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

	logarea := widget.NewMultiLineEntry()
	logarea.Disable()

	// TODO: have the button be pressed if enter is pressed
	gobtn = widget.NewButtonWithIcon("Start OCR", theme.UploadIcon(), func() {
		if dir.Text == "" {
			return
		}

		gobtn.Disable()
		gobtn.SetText("Processing...")

		progressBar.SetValue(0.5)

		stdout, err := copyStdoutToChan()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error copying stdout to chan: %v\n", err)
			return
		}

		// update log area with stdout in a concurrent goroutine
		go func() {
			for r := range stdout {
				logarea.SetText(logarea.Text + string(r))
				logarea.CursorRow = strings.Count(logarea.Text, "\n")
				// TODO: set text on progress bar to latest line printed using progressBar.TextFormatter, rather than just using a whole multiline entry like this
				// TODO: parse the stdout and set progressBar based on that
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
				// TODO: set text on progress bar, or a label below it, to latest line printed, rather than just using a whole multiline entry like this
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
