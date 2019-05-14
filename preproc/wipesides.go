package preproc

// TODO: add minimum size variable (default ~30%?)
// TODO: switch to an interface rather than integralimg.I

import (
	"image"
	"image/color"

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
// from the middle of the image to the left and right, stopping when it reaches
// a point at which there is a lower proportion of black pixels than thresh.
func findedges(img integralimg.I, wsize int, thresh float64) (int, int) {
	maxx := len(img[0]) - 1
	var lowedge, highedge int = 0, maxx

	for x := maxx / 2; x < maxx-wsize; x++ {
		if proportion(img, x, wsize) <= thresh {
			highedge = findbestedge(img, x, wsize)
			break
		}
	}

	for x := maxx / 2; x > 0; x-- {
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

// wipe fills the sections of image which fall outside the content
// area with white
func Wipe(img *image.Gray, wsize int, thresh float64) *image.Gray {
	integral := integralimg.ToIntegralImg(img)
	lowedge, highedge := findedges(integral, wsize, thresh)
	return wipesides(img, lowedge, highedge)
}
