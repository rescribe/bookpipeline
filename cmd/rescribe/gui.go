// Copyright 2021 Nick White.
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
	0.2: "Preprocessing",
	0.5: "OCRing",
	0.9: "Analysing",
	1.0: "Done",
}

var trainingNames = map[string]string{
	"carolinemsv1_fast": "Caroline Miniscule",
	"eng": "English (modern printing)",
	"lat": "Latin (modern printing)",
	"rescribefrav2_fast": "French (early printing)",
	"rescribev8_fast": "Latin (early printing)",
}

// getBookIdFromUrl returns a 12 character Google Book ID from
// a Google URL, or an error if one can't be found.
func getBookIdFromUrl(url string) (string, error) {
	lurl := strings.ToLower(url)
	if len(url) == 12 && !strings.ContainsAny(url, "?/:") {
		return url, nil
	}
	if !strings.HasPrefix(lurl, "https://books.google") {
		return "", fmt.Errorf("Not a Google Books URL")
	}

	start := strings.Index(lurl, "?id=")
	if start == -1 {
		start = strings.Index(lurl, "&id=")
	}

	if start >= 0 {
		start += 4
		if len(url[start:]) < 12 {
			return "", fmt.Errorf("Could not find book ID in URL")
		}
		return url[start:start+12], nil
	}

	return "", fmt.Errorf("Could not find book ID in URL")
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
func startGui(log log.Logger, cmd string, gbookcmd string, training string, tessdir string) error {
	myApp := app.New()
	myWindow := myApp.NewWindow("Rescribe OCR")

	myWindow.Resize(fyne.NewSize(800, 400))

	var abortbtn, gobtn *widget.Button
	var fullContent *fyne.Container

	dir := widget.NewLabel("")

	dirIcon := widget.NewIcon(theme.FolderIcon())

	folderBtn := widget.NewButtonWithIcon("Choose folder", theme.FolderOpenIcon(), func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			dir.SetText(uri.Path())
			dirIcon.SetResource(theme.FolderIcon())
			myWindow.SetContent(fullContent)
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
			myWindow.SetContent(fullContent)
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
				return fmt.Errorf("No save directory set")
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
		f2 := widget.NewFormItem("Save Directory", saveDir)
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
			myWindow.SetContent(fullContent)
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
	progressBar.TextFormatter = formatProgressBarText(progressBar)

	logarea := widget.NewMultiLineEntry()
	logarea.Disable()

	detail := widget.NewAccordion(widget.NewAccordionItem("Log", logarea))

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())

	abortbtn = widget.NewButtonWithIcon("Abort", theme.CancelIcon(), func() {
		fmt.Printf("\nAbort\n")
		cancel()
		progressBar.SetValue(0.0)
		for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
			v.Enable()
		}
		abortbtn.Disable()
		ctx, cancel = context.WithCancel(context.Background())
	})
	abortbtn.Disable()

	gobtn = widget.NewButtonWithIcon("Start OCR", theme.UploadIcon(), func() {
		if dir.Text == "" {
			return
		}

		stdout, err := copyStdoutToChan()
		if err != nil {
			msg := fmt.Sprintf("Internal error\n\nError copying stdout to chan: %v\n", err)
			dialog.ShowError(errors.New(msg), myWindow)
			fmt.Fprintf(os.Stderr, msg)
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
			msg := fmt.Sprintf("Internal error\n\nError copying stdout to chan: %v\n", err)
			dialog.ShowError(errors.New(msg), myWindow)
			fmt.Fprintf(os.Stderr, msg)
			return
		}

		// update log area with stderr in a concurrent goroutine
		go func() {
			for r := range stderr {
				logarea.SetText(logarea.Text + string(r))
				logarea.CursorRow = strings.Count(logarea.Text, "\n")
			}
		}()

		bookdir := dir.Text
		savedir := dir.Text
		bookname := strings.ReplaceAll(filepath.Base(dir.Text), " ", "_")

		f, err := os.Stat(bookdir)
		if err != nil && !strings.HasPrefix(bookdir, "Google Book: ") {
			msg := fmt.Sprintf("Error opening %s: %v", bookdir, err)
			dialog.ShowError(errors.New(msg), myWindow)
			fmt.Fprintf(os.Stderr, msg)

			progressBar.SetValue(0.0)
			for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
				v.Enable()
			}
			abortbtn.Disable()
			return
		}

		// Do this in a goroutine so the GUI remains responsive
		go func() {
			for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
				v.Disable()
			}

			abortbtn.Enable()

			progressBar.SetValue(0.1)

			if strings.HasPrefix(dir.Text, "Google Book: ") {
				progressBar.SetValue(0.11)
				start := len("Google Book: ")
				bookname = dir.Text[start:start+12]

				start = start + 12 + len(" Save to: ")
				bookdir = dir.Text[start:]
				savedir = bookdir

				fmt.Printf("Downloading Google Book\n")
				d, err := getGoogleBook(ctx, gbookcmd, bookname, bookdir)
				if err != nil {
					if !strings.HasSuffix(err.Error(), "signal: killed") {
						msg := fmt.Sprintf("Error downloading Google Book %s\n", bookname)
						dialog.ShowError(errors.New(msg), myWindow)
						fmt.Fprintf(os.Stderr, msg)
					}
					progressBar.SetValue(0.0)
					for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
						v.Enable()
					}
					abortbtn.Disable()
					return
				}
				bookdir = d
				savedir = d
				bookname = filepath.Base(d)
			}

			if strings.HasSuffix(dir.Text, ".pdf") && !f.IsDir() {
				progressBar.SetValue(0.12)
				bookdir, err = extractPdfImgs(ctx, bookdir)
				if err != nil {
					if !strings.HasSuffix(err.Error(), "context canceled") {
						msg := fmt.Sprintf("Error opening PDF %s: %v\n", bookdir, err)
						dialog.ShowError(errors.New(msg), myWindow)
						fmt.Fprintf(os.Stderr, msg)
					}

					progressBar.SetValue(0.0)
					for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
						v.Enable()
					}
					abortbtn.Disable()
					return
				}

				// happens if extractPdfImgs recovers from a PDF panic,
				// which will occur if we encounter an image we can't decode
				if bookdir == "" {
					msg := fmt.Sprintf("Error opening PDF\nThe format of this PDF is not supported, extract the images manually into a folder first.\n")
					dialog.ShowError(errors.New(msg), myWindow)
					fmt.Fprintf(os.Stderr, msg)

					progressBar.SetValue(0.0)
					for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
						v.Enable()
					}
					abortbtn.Disable()
					return
				}

				savedir = strings.TrimSuffix(savedir, ".pdf")
				bookname = strings.TrimSuffix(bookname, ".pdf")
			}

			training := trainingOpts.Selected
			if strings.Contains(training, "[") {
				start := strings.Index(training, "[") + 1
				end := strings.Index(training, "]")
				training = training[start:end]
			}

			err = startProcess(ctx, log, cmd, bookdir, bookname, training, savedir, tessdir, !wipe.Checked, bigpdf.Checked)
			if err != nil && strings.HasSuffix(err.Error(), "context canceled") {
				progressBar.SetValue(0.0)
				return
			}
			if err != nil {
				msg := fmt.Sprintf("Error during processing: %v\n", err)
				dialog.ShowError(errors.New(msg), myWindow)
				fmt.Fprintf(os.Stderr, msg)

				progressBar.SetValue(0.0)
				for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
					v.Enable()
				}
				abortbtn.Disable()
				return
			}

			progressBar.SetValue(1.0)

			for _, v := range []fyne.Disableable{folderBtn, pdfBtn, gbookBtn, wipe, bigpdf, trainingOpts, gobtn} {
				v.Enable()
			}
			abortbtn.Disable()

			msg := fmt.Sprintf("OCR process finished successfully.\n\nYour completed files have been saved in:\n%s", savedir)
			dialog.ShowInformation("OCR Complete", msg, myWindow)
		}()
	})
	gobtn.Disable()

	choices := container.New(layout.NewGridLayout(3), folderBtn, pdfBtn, gbookBtn)

	chosen := container.New(layout.NewBorderLayout(nil, nil, dirIcon, nil), dirIcon, dir)

	trainingBits := container.New(layout.NewBorderLayout(nil, nil, trainingLabel, nil), trainingLabel, trainingOpts)

	fullContent = container.NewVBox(choices, chosen, trainingBits, wipe, bigpdf, gobtn, abortbtn, progressBar, detail)
	startContent := container.NewVBox(choices, trainingBits, wipe, bigpdf, gobtn, abortbtn, progressBar, detail)

	myWindow.SetContent(startContent)

	myWindow.Show()
	myApp.Run()

	return nil
}
