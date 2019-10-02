package preproc

// TODO: add minimum size variable (default ~30%?)
// TODO: switch to an interface rather than integralimg.I

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"

	"rescribe.xyz/go.git/integralimg"
)

// returns the proportion of the given window that is black pixels
func proportion(i integralimg.I, x int, size int) float64 {
	w := i.GetVerticalWindow(x, size)
	return w.Proportion()
}

// findbestedge goes through every vertical line from x to x+w to
// find the one with the lowest proportion of black pixels.
func findbestedge(img integralimg.I, x int, w int) int {
	var bestx int
	var best float64

	if w == 1 {
		return x
	}

	right := x + w
	for ; x < right; x++ {
		prop := proportion(img, x, 1)
		if prop > best {
			best = prop
			bestx = x
		}
	}

	return bestx
}

// findedges finds the edges of the main content, by moving a window of wsize
// from near the middle of the image to the left and right, stopping when it reaches
// a point at which there is a lower proportion of black pixels than thresh.
func findedges(img integralimg.I, wsize int, thresh float64) (int, int) {
	maxx := len(img[0]) - 1
	var lowedge, highedge int = 0, maxx

	// don't start at the middle, as this will fail for 2 column layouts,
	// start 10% left or right of the middle
	notcentre := maxx / 10

	for x := maxx/2 + notcentre; x < maxx-wsize; x++ {
		if proportion(img, x, wsize) <= thresh {
			highedge = findbestedge(img, x, wsize)
			break
		}
	}

	for x := maxx/2 - notcentre; x > 0; x-- {
		if proportion(img, x, wsize) <= thresh {
			lowedge = findbestedge(img, x, wsize)
			break
		}
	}

	return lowedge, highedge
}

// wipesides fills the sections of image not within the boundaries
// of lowedge and highedge with white
func wipesides(img *image.Gray, lowedge int, highedge int) *image.Gray {
	b := img.Bounds()
	new := image.NewGray(b)

	// set left edge white
	for x := b.Min.X; x < lowedge; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			new.SetGray(x, y, color.Gray{255})
		}
	}
	// copy middle
	for x := lowedge; x < highedge; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			new.SetGray(x, y, img.GrayAt(x, y))
		}
	}
	// set right edge white
	for x := highedge; x < b.Max.X; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			new.SetGray(x, y, color.Gray{255})
		}
	}

	return new
}

// toonarrow checks whether the area between lowedge and highedge is
// less than min % of the total image width
func toonarrow(img *image.Gray, lowedge int, highedge int, min int) bool {
	b := img.Bounds()
	imgw := b.Max.X - b.Min.X
	wipew := highedge - lowedge
	if float64(wipew)/float64(imgw)*100 < float64(min) {
		return true
	}
	return false
}

// Wipe fills the sections of image which fall outside the content
// area with white, providing the content area is above min %
func Wipe(img *image.Gray, wsize int, thresh float64, min int) *image.Gray {
	integral := integralimg.ToIntegralImg(img)
	lowedge, highedge := findedges(integral, wsize, thresh)
	if toonarrow(img, lowedge, highedge, min) {
		return img
	}
	return wipesides(img, lowedge, highedge)
}

// WipeFile wipes an image file, filling the sections of the image
// which fall outside the content area with white, providing the
// content area is above min %.
// inPath: path of the input image.
// outPath: path to save the output image.
// wsize: window size for wipe algorithm.
// thresh: threshold for wipe algorithm.
// min: minimum % of content area width to consider valid.
func WipeFile(inPath string, outPath string, wsize int, thresh float64, min int) error {
	f, err := os.Open(inPath)
	defer f.Close()
	if err != nil {
		return errors.New(fmt.Sprintf("Could not open file %s: %v", inPath, err))
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not decode image: %v", err))
	}
	b := img.Bounds()
	gray := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(gray, b, img, b.Min, draw.Src)

	clean := Wipe(gray, wsize, thresh, min)

	f, err = os.Create(outPath)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not create file %s: %v", outPath, err))
	}
	defer f.Close()
	err = png.Encode(f, clean)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not encode image: %v", err))
	}
	return nil
}
