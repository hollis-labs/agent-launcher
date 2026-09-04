//go:build darwin

package shell

/*
#cgo CFLAGS: -mmacosx-version-min=10.13 -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation

#import <Foundation/Foundation.h>

static void tachyonRegisterTrayPlacementDefault(void) {
	@autoreleasepool {
		// AppKit assigns Item-0 to the first status item when the application
		// does not provide an explicit autosave name. Wails creates exactly one.
		// A registered default loses to a position the user has saved by
		// Command-dragging, so it avoids the target Mac's measured first-run
		// overflow without pinning the item against later user customization.
		[[NSUserDefaults standardUserDefaults] registerDefaults:@{
			@"NSStatusItem Preferred Position Item-0": @220
		}];
	}
}
*/
import "C"

func registerTrayPlacementDefault() {
	C.tachyonRegisterTrayPlacementDefault()
}
