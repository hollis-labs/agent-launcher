//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>
#include <stdlib.h>

static int tachyonTerminateApplication(int pid, const char *expectedExecutableBytes) {
	@autoreleasepool {
		NSRunningApplication *application =
			[NSRunningApplication runningApplicationWithProcessIdentifier:(pid_t)pid];
		if (application == nil) {
			return 0;
		}
		NSString *expectedExecutable = [NSString stringWithUTF8String:expectedExecutableBytes];
		if (expectedExecutable == nil ||
			![application.executableURL.path isEqualToString:expectedExecutable]) {
			return -1;
		}
		return [application terminate] ? 1 : 0;
	}
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func terminateApplication(pid int, expectedExecutable string) error {
	executable := C.CString(expectedExecutable)
	defer C.free(unsafe.Pointer(executable))

	switch result := int(C.tachyonTerminateApplication(C.int(pid), executable)); result {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("PID %d does not run %q", pid, expectedExecutable)
	default:
		return fmt.Errorf("AppKit could not request termination of PID %d", pid)
	}
}
