package main

import (
	"image"
	"image/color"
	"testing"
)

func TestContrastingPixelsRejectsBlankFrame(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 48))
	fill(img, color.RGBA{R: 48, G: 50, B: 53, A: 255})

	count, _ := contrastingPixels(img, minimumLumaDifference)
	if count != 0 {
		t.Fatalf("contrasting pixels = %d, want 0 for blank frame", count)
	}
}

func TestContrastingPixelsFindsRenderedMark(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 48))
	fill(img, color.RGBA{R: 48, G: 50, B: 53, A: 255})
	for y := 20; y < 23; y++ {
		for x := 25; x < 29; x++ {
			img.Set(x, y, color.White)
		}
	}

	count, _ := contrastingPixels(img, minimumLumaDifference)
	if count != 12 {
		t.Fatalf("contrasting pixels = %d, want 12 rendered-mark pixels", count)
	}
}

func fill(img *image.RGBA, c color.Color) {
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			img.Set(x, y, c)
		}
	}
}
