// tachyon-menubarcheck verifies that a captured status-item frame contains a
// rendered mark rather than only menu-bar background. It is invoked by
// scripts/check-macos-tray.sh after that script resolves the package's exact
// Accessibility frame and captures it with screencapture.
package main

import (
	"fmt"
	"image"
	_ "image/png"
	"os"
	"sort"
)

const (
	minimumLumaDifference = 60
	minimumContrasting    = 8
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tachyon-menubarcheck STATUS-ITEM.png")
		os.Exit(2)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open status-item capture: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	img, format, err := image.Decode(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode status-item capture: %v\n", err)
		os.Exit(1)
	}
	if format != "png" {
		fmt.Fprintf(os.Stderr, "status-item capture format = %q, want png\n", format)
		os.Exit(1)
	}

	count, median := contrastingPixels(img, minimumLumaDifference)
	if count < minimumContrasting {
		fmt.Fprintf(os.Stderr,
			"status-item frame is visually blank: %d pixels differ from median luminance %d by at least %d (need %d)\n",
			count, median, minimumLumaDifference, minimumContrasting)
		os.Exit(1)
	}
	fmt.Printf("verified rendered status mark: %d contrasting pixels (median luminance %d)\n", count, median)
}

func contrastingPixels(img image.Image, minimumDifference int) (int, int) {
	bounds := img.Bounds()
	luminances := make([]int, 0, bounds.Dx()*bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			luminances = append(luminances,
				(299*int(r>>8)+587*int(g>>8)+114*int(b>>8))/1000)
		}
	}
	if len(luminances) == 0 {
		return 0, 0
	}

	sort.Ints(luminances)
	median := luminances[len(luminances)/2]
	count := 0
	for _, luminance := range luminances {
		difference := luminance - median
		if difference < 0 {
			difference = -difference
		}
		if difference >= minimumDifference {
			count++
		}
	}
	return count, median
}
