//go:build darwin

package main

/*
#cgo LDFLAGS: -framework CoreGraphics

#include <CoreGraphics/CoreGraphics.h>
#include <unistd.h>

static int tachyonPostRightClick(double x, double y) {
	CGPoint point = CGPointMake(x, y);
	CGEventRef down = CGEventCreateMouseEvent(NULL, kCGEventRightMouseDown, point, kCGMouseButtonRight);
	CGEventRef up = CGEventCreateMouseEvent(NULL, kCGEventRightMouseUp, point, kCGMouseButtonRight);
	if (down == NULL || up == NULL) {
		if (down != NULL) CFRelease(down);
		if (up != NULL) CFRelease(up);
		return 0;
	}
	CGEventPost(kCGHIDEventTap, down);
	usleep(50000);
	CGEventPost(kCGHIDEventTap, up);
	CFRelease(down);
	CFRelease(up);
	return 1;
}
*/
import "C"

import "fmt"

func postRightClick(x, y float64) error {
	if C.tachyonPostRightClick(C.double(x), C.double(y)) == 0 {
		return fmt.Errorf("CoreGraphics could not create mouse events")
	}
	return nil
}
