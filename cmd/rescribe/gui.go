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
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// copyStdoutToChan creates a pipe to copy anything written
// to stdout instead to a rune channel
func copyStdoutToChan() (chan rune, error) {
	c := make(chan rune)

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return c, fmt.Errorf("Error creating pipe for stdout redirection: %v", err)
	}
	os.Stdout = w

	bufReader := bufio.NewReader(r)

	go func() {
		defer func() {
			close(c)
			w.Close()
			os.Stdout = origStdout
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

// startGui starts the gui process
func startGui(log log.Logger, cmd string, training string, systess bool, tessdir string) error {
	myApp := app.New()
	myWindow := myApp.NewWindow("Rescribe OCR")

	var gobtn *widget.Button

	dir := widget.NewEntry()
	dir.SetPlaceHolder("Folder to process")
	dir.OnChanged = func(s string) {
		// TODO: also check if string is a directory, and only enable if so
		if dir.Text != "" {
			gobtn.Enable()
		} else {
			gobtn.Disable()
		}
	}

	openbtn := widget.NewButtonWithIcon("Choose folder", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err == nil && uri != nil {
				dir.SetText(uri.Path())
			}
	}, myWindow)})

	progressBar := widget.NewProgressBar()

	logarea := widget.NewMultiLineEntry()
	logarea.Disable()


	// TODO: have the button be pressed if enter is pressed
	gobtn = widget.NewButtonWithIcon("Process OCR", theme.UploadIcon(), func() {
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

		// update log area with output from outC in a concurrent goroutine
		go func() {
			for r := range stdout {
				logarea.SetText(logarea.Text + string(r))
				logarea.CursorRow = strings.Count(logarea.Text, "\n")
				// TODO: set text on progress bar, or a label below it, to latest line printed, rather than just using a whole multiline entry like this
				// TODO: parse the stdout and set progressBar based on that
			}
		}()

		err = startProcess(log, cmd, dir.Text, filepath.Base(dir.Text), training, systess, dir.Text, tessdir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error executing process: %v\n", err)
			return
		}

		progressBar.SetValue(1.0)
		gobtn.SetText("Process OCR")
		gobtn.Enable()
	})
	gobtn.Disable()

	diropener := container.New(layout.NewGridLayout(2), dir, openbtn)

	content := container.NewVBox(diropener, gobtn, progressBar, logarea)

	myWindow.SetContent(content)

	myWindow.Show()
	myApp.Run()

	return nil
}
