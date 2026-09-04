// tachyon-menubarcheck provides the native operations needed by the packaged
// tray acceptance check: pixel inspection, a physical right-click, and
// PID/executable-checked Cocoa termination for failure cleanup.
package main

import (
	"fmt"
	"image"
	_ "image/png"
	"os"
	"sort"
	"strconv"
)

const (
	minimumLumaDifference = 60
	minimumContrasting    = 8
)

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--terminate" {
		pid, err := strconv.Atoi(os.Args[2])
		if err != nil || pid <= 0 || os.Args[3] == "" {
			fmt.Fprintln(os.Stderr, "terminate requires a positive PID and exact executable path")
			os.Exit(2)
		}
		if err := terminateApplication(pid, os.Args[3]); err != nil {
			fmt.Fprintf(os.Stderr, "terminate exact packaged app: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 6 && os.Args[1] == "--right-click" {
		x, xerr := strconv.ParseFloat(os.Args[2], 64)
		y, yerr := strconv.ParseFloat(os.Args[3], 64)
		width, werr := strconv.ParseFloat(os.Args[4], 64)
		height, herr := strconv.ParseFloat(os.Args[5], 64)
		if xerr != nil || yerr != nil || werr != nil || herr != nil || width <= 0 || height <= 0 {
			fmt.Fprintln(os.Stderr, "right-click requires numeric X Y WIDTH HEIGHT with positive dimensions")
			os.Exit(2)
		}
		if err := postRightClick(x+width/2, y+height/2); err != nil {
			fmt.Fprintf(os.Stderr, "right-click status item: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tachyon-menubarcheck STATUS-ITEM.png | --right-click X Y WIDTH HEIGHT | --terminate PID EXECUTABLE")
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
