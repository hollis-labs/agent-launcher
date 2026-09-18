//go:build !darwin

package main

import "fmt"

func postRightClick(_, _ float64) error {
	return fmt.Errorf("CoreGraphics right-click injection requires macOS")
}
