//go:build !darwin

package main

import "fmt"

func terminateApplication(_ int, _ string) error {
	return fmt.Errorf("AppKit application termination requires macOS")
}
