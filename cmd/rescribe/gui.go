// Copyright 2021-2022 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"errors"
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
	0.11: "Downloading",
	0.12: "Processing PDF",
	0.2:  "Preprocessing",
	0.5:  "OCRing",
	0.9:  "Analysing",
	1.0:  "Done",
}

var trainingNames = map[string]string{
	"eng":             "English (modern print)",
	"lat":             "Latin (modern print)",
	"rescribev9_fast": "Latin/English/French (printed ca 1500-1800)",
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
// but a naive version using *os.File didn't work.
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
				sel.SetSelectedIndex(0)
				return
			}
			defer uri.Close()
			name := uri.URI().Name()
			newpath := filepath.Join(os.Getenv("TESSDATA_PREFIX"), name)
			f, err := os.Create(newpath)
			if err != nil {
				msg := fmt.Sprintf("Error creating temporary file to store custom training: %v\n", err)
				dialog.ShowError(errors.New(msg), parent)
				fmt.Fprintf(os.Stderr, msg)
				sel.SetSelectedIndex(0)
				return
			}
			defer f.Close()
			_, err = io.Copy(f, uri)
			if err != nil {
				msg := fmt.Sprintf("Error copying custom training to temporary file: %v\n", err)
				dialog.ShowError(errors.New(msg), parent)
				fmt.Fprintf(os.Stderr, msg)
				sel.SetSelectedIndex(0)
				return
			}

			basicname := strings.TrimSuffix(name, ".traineddata")
			opts := append([]string{basicname}, sel.Options...)
			sel.Options = opts
			sel.SetSelectedIndex(0)
		}, parent)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".traineddata"}))
		d.Resize(fyne.NewSize(740, 600))
		d.Show()
	}
}

// mkTrainingSelect returns a select widget with all training
// files in TESSDATA_PREFIX/training, any other trainings listed
// in the extras slice, selecting the first entry.
func mkTrainingSelect(extras []string, parent fyne.Window) *widget.Select {
	prefix := os.Getenv("TESSDATA_PREFIX")
	fn, err := filepath.Glob(prefix + "/*.traineddata")
	if err != nil {
		fn = []string{}
	}
	var opts []string
	for _, v := range append(extras, fn...) {
		t := strings.TrimSuffix(strings.TrimPrefix(v, prefix), ".traineddata")
		if t == "osd" {
			continue
		}
		for code, name := range trainingNames {
			if t == code {
				t = fmt.Sprintf("%s [%s]", name, code)
				break
			}
		}
		alreadythere := 0
		for _, opt := range opts {
			if t == opt {
				alreadythere = 1
				break
			}
		}
		if alreadythere == 0 {
			opts = append(opts, t)
		}
	}

	opts = append(opts, "Other...")
	s := widget.NewSelect(opts, func(string) {})
	// OnChanged is set outside of NewSelect so the reference to s isn't nil
	s.OnChanged = trainingSelectOnChange(s, parent)
	s.SetSelectedIndex(0)
	return s
}

// formatProgressBar uses the progressPoints map to set the text for the progress bar
// appropriately
func formatProgressBar(bar *widget.ProgressBar) func() string {
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

// updateProgress parses the last line of a log and updates a progress
// bar appropriately.
func updateProgress(log string, progressBar *widget.ProgressBar) {
	lines := strings.Split(log, "\n")
	lastline := lines[len(lines)-1]
	for i, v := range progressPoints {
		if strings.HasPrefix(lastline, "  "+v) {
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

// start sets up the gui to start the core process, and if all is well
// it starts it
func start(ctx context.Context, log *log.Logger, cmd string, tessdir string, gbookcmd string, dir string, training string, win fyne.Window, logarea *widget.Entry, progressBar *widget.ProgressBar, abortbtn *widget.Button, wipe bool, bigpdf bool, disableWidgets []fyne.Disableable) {
	if dir == "" {
		return
	}

	stdout, err := copyStdoutToChan()
	if err != nil {
		msg := fmt.Sprintf("Internal error\n\nError copying stdout to chan: %v\n", err)
		dialog.ShowError(errors.New(msg), win)
		fmt.Fprintf(os.Stderr, msg)
		return
	}
	go func() {
		for r := range stdout {
			logarea.SetText(logarea.Text + string(r))
			logarea.CursorRow = strings.Count(logarea.Text, "\n")
			updateProgress(logarea.Text, progressBar)
		}
	}()

	stderr, err := copyStderrToChan()
	if err != nil {
		msg := fmt.Sprintf("Internal error\n\nError copying stdout to chan: %v\n", err)
		dialog.ShowError(errors.New(msg), win)
		fmt.Fprintf(os.Stderr, msg)
		return
	}
	go func() {
		for r := range stderr {
			logarea.SetText(logarea.Text + string(r))
			logarea.CursorRow = strings.Count(logarea.Text, "\n")
		}
	}()

	// Do this in a goroutine so the GUI remains responsive
	go func() {
		letsGo(ctx, log, cmd, tessdir, gbookcmd, dir, training, win, logarea, progressBar, abortbtn, wipe, bigpdf, disableWidgets)
	}()
}

// letsGo starts the core process
func letsGo(ctx context.Context, log *log.Logger, cmd string, tessdir string, gbookcmd string, dir string, training string, win fyne.Window, logarea *widget.Entry, progressBar *widget.ProgressBar, abortbtn *widget.Button, wipe bool, bigpdf bool, disableWidgets []fyne.Disableable) {
	bookdir := dir
	savedir := dir
	bookname := strings.ReplaceAll(filepath.Base(dir), " ", "_")

	f, err := os.Stat(bookdir)
	if err != nil && !strings.HasPrefix(bookdir, "Google Book: ") {
		msg := fmt.Sprintf("Error opening %s: %v", bookdir, err)
		dialog.ShowError(errors.New(msg), win)
		fmt.Fprintf(os.Stderr, msg)

		progressBar.SetValue(0.0)
		for _, v := range disableWidgets {
			v.Enable()
		}
		abortbtn.Disable()
		return
	}

	for _, v := range disableWidgets {
		v.Disable()
	}

	abortbtn.Enable()

	progressBar.SetValue(0.1)

	if strings.HasPrefix(dir, "Google Book: ") {
		if gbookcmd == "" {
			msg := fmt.Sprintf("No getgbook found, can't download Google Book. Either set -gbookcmd on the command line, or use the official build which includes an embedded copy of getgbook.\n")
			dialog.ShowError(errors.New(msg), win)
			fmt.Fprintf(os.Stderr, msg)
			progressBar.SetValue(0.0)
			for _, v := range disableWidgets {
				v.Enable()
			}
			abortbtn.Disable()
			return
		}
		progressBar.SetValue(0.11)
		start := len("Google Book: ")
		bookname = dir[start : start+12]

		start = start + 12 + len(" Save to: ")
		bookdir = dir[start:]
		savedir = bookdir

		fmt.Printf("Downloading Google Book\n")
		d, err := getGoogleBook(ctx, gbookcmd, bookname, bookdir)
		if err != nil {
			if !strings.HasSuffix(err.Error(), "signal: killed") {
				msg := fmt.Sprintf("Error downloading Google Book %s\n", bookname)
				dialog.ShowError(errors.New(msg), win)
				fmt.Fprintf(os.Stderr, msg)
			}
			progressBar.SetValue(0.0)
			for _, v := range disableWidgets {
				v.Enable()
			}
			abortbtn.Disable()
			return
		}
		bookdir = d
		savedir = d
		bookname = filepath.Base(d)
	}

	if strings.HasSuffix(dir, ".pdf") && !f.IsDir() {
		progressBar.SetValue(0.12)
		bookdir, err = extractPdfImgs(ctx, bookdir)
		if err != nil {
			if !strings.HasSuffix(err.Error(), "context canceled") {
				msg := fmt.Sprintf("Error opening PDF %s: %v\n", bookdir, err)
				dialog.ShowError(errors.New(msg), win)
				fmt.Fprintf(os.Stderr, msg)
			}

			progressBar.SetValue(0.0)
			for _, v := range disableWidgets {
				v.Enable()
			}
			abortbtn.Disable()
			return
		}

		// happens if extractPdfImgs recovers from a PDF panic,
		// which will occur if we encounter an image we can't decode
		if bookdir == "" {
			msg := fmt.Sprintf("Error opening PDF\nThe format of this PDF is not supported, extract the images to .jpg manually into a\nfolder first, using a tool like the PDF image extractor at https://pdfcandy.com/extract-images.html.\n")
			dialog.ShowError(errors.New(msg), win)
			fmt.Fprintf(os.Stderr, msg)

			progressBar.SetValue(0.0)
			for _, v := range disableWidgets {
				v.Enable()
			}
			abortbtn.Disable()
			return
		}

		savedir = strings.TrimSuffix(savedir, ".pdf")
		bookname = strings.TrimSuffix(bookname, ".pdf")
	}

	if strings.Contains(training, "[") {
		start := strings.Index(training, "[") + 1
		end := strings.Index(training, "]")
		training = training[start:end]
	}

	err = startProcess(ctx, log, cmd, bookdir, bookname, training, savedir, tessdir, wipe, bigpdf)
	if err != nil && strings.HasSuffix(err.Error(), "context canceled") {
		progressBar.SetValue(0.0)
		return
	}
	if err != nil {
		msg := fmt.Sprintf("Error during processing: %v\n", err)
		if strings.HasSuffix(err.Error(), "No images found") && strings.HasSuffix(dir, ".pdf") && !f.IsDir() {
			msg = fmt.Sprintf("Error opening PDF\nNo images found in the PDF. Most likely the format of this PDF is not supported,\nextract the images to .jpg manually into a folder first, using a tool like\nthe PDF image extractor at https://pdfcandy.com/extract-images.html.\n")
		}
		dialog.ShowError(errors.New(msg), win)
		fmt.Fprintf(os.Stderr, msg)

		progressBar.SetValue(0.0)
		for _, v := range disableWidgets {
			v.Enable()
		}
		abortbtn.Disable()
		return
	}

	progressBar.SetValue(1.0)

	for _, v := range disableWidgets {
		v.Enable()
	}
	abortbtn.Disable()

	msg := fmt.Sprintf("OCR process finished successfully.\n\nYour completed files have been saved in:\n%s", savedir)
	dialog.ShowInformation("OCR Complete", msg, win)
}

// startGui starts the gui process
func startGui(log *log.Logger, cmd string, gbookcmd string, training string, tessdir string) error {
	myApp := app.New()
	myWindow := myApp.NewWindow("Rescribe OCR")

	myWindow.Resize(fyne.NewSize(800, 400))

	var abortbtn, gobtn *widget.Button
	var chosen *fyne.Container

	dir := widget.NewLabel("")

	dirIcon := widget.NewIcon(theme.FolderIcon())

	folderBtn := widget.NewButtonWithIcon("Choose folder", theme.FolderOpenIcon(), func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			dir.SetText(uri.Path())
			dirIcon.SetResource(theme.FolderIcon())
			chosen.Show()
			gobtn.Enable()
		}, myWindow)
		d.Resize(fyne.NewSize(740, 600))
		d.Show()
	})

	pdfBtn := widget.NewButtonWithIcon("Choose PDF", theme.DocumentIcon(), func() {
		d := dialog.NewFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err != nil || uri == nil {
				return
			}
			uri.Close()
			dir.SetText(uri.URI().Path())
			dirIcon.SetResource(theme.DocumentIcon())
			chosen.Show()
			gobtn.Enable()
		}, myWindow)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".pdf"}))
		d.Resize(fyne.NewSize(740, 600))
		d.Show()
	})

	gbookBtn := widget.NewButtonWithIcon("Get Google Book", theme.SearchIcon(), func() {
		dirEntry := widget.NewEntry()
		bookId := widget.NewEntry()
		homeDir, err := os.UserHomeDir()
		if err == nil {
			dirEntry.SetText(homeDir)
		}
		dirEntry.Validator = func(s string) error {
			if s == "" {
				return fmt.Errorf("No save folder set")
			}
			return nil
		}
		dirBtn := widget.NewButtonWithIcon("Browse", theme.FolderIcon(), func() {
			d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
				if err != nil || uri == nil {
					return
				}
				dirEntry.SetText(uri.Path())
			}, myWindow)
			d.Resize(fyne.NewSize(740, 600))
			d.Show()
		})
		bookId.Validator = func(s string) error {
			_, err := getBookIdFromUrl(s)
			return err
		}
		f1 := widget.NewFormItem("Book ID / URL", bookId)
		saveDir := container.New(layout.NewBorderLayout(nil, nil, nil, dirBtn), dirEntry, dirBtn)
		f2 := widget.NewFormItem("Save in folder", saveDir)
		d := dialog.NewForm("Enter Google Book ID or URL", "OK", "Cancel", []*widget.FormItem{f1, f2}, func(b bool) {
			if b != true {
				return
			}
			id, err := getBookIdFromUrl(bookId.Text)
			if err != nil {
				return
			}
			if dirEntry.Text == "" {
				dirEntry.SetText(homeDir)
			}
			dir.SetText(fmt.Sprintf("Google Book: %s Save to: %s", id, dirEntry.Text))
			dirIcon.SetResource(theme.SearchIcon())
			chosen.Show()
			gobtn.Enable()
		}, myWindow)
		d.Resize(fyne.NewSize(600, 200))
		d.Show()
	})

	wipe := widget.NewCheck("Automatically clean image sides", func(bool) {})

	bigpdf := widget.NewCheck("Use highest image quality for searchable PDF (requires lots of RAM)", func(bool) {})
	bigpdf.Checked = false

	trainingLabel := widget.NewLabel("Language / Script")

	trainingOpts := mkTrainingSelect([]string{training}, myWindow)

	progressBar := widget.NewProgressBar()
	progressBar.TextFormatter = formatProgressBar(progressBar)

	logarea := widget.NewMultiLineEntry()

	detail := widget.NewAccordion(widget.NewAccordionItem("Log", logarea))

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())

	gobtn = widget.NewButtonWithIcon("Start OCR", theme.UploadIcon(), func() {})

	disableWidgets := []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn}

	abortbtn = widget.NewButtonWithIcon("Abort", theme.CancelIcon(), func() {
		fmt.Printf("\nAbort\n")
		cancel()
		progressBar.SetValue(0.0)
		for _, v := range disableWidgets {
			v.Enable()
		}
		abortbtn.Disable()
		ctx, cancel = context.WithCancel(context.Background())
	})
	abortbtn.Disable()

	gobtn.OnTapped = func() {
		start(ctx, log, cmd, tessdir, gbookcmd, dir.Text, trainingOpts.Selected, myWindow, logarea, progressBar, abortbtn, !wipe.Checked, bigpdf.Checked, disableWidgets)
	}

	gobtn.Disable()

	choices := container.New(layout.NewGridLayout(3), folderBtn, pdfBtn, gbookBtn)

	chosen = container.New(layout.NewBorderLayout(nil, nil, dirIcon, nil), dirIcon, dir)
	chosen.Hide()

	trainingBits := container.New(layout.NewBorderLayout(nil, nil, trainingLabel, nil), trainingLabel, trainingOpts)

	startBox := container.NewVBox(choices, chosen, trainingBits, wipe, bigpdf, gobtn, abortbtn, progressBar)
	startContent := container.NewBorder(startBox, nil, nil, nil, detail)

	myWindow.SetContent(startContent)

	myWindow.Show()
	myApp.Run()

	return nil
}
