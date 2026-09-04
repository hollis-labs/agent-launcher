//go:build darwin

package shell

/*
#cgo CFLAGS: -mmacosx-version-min=10.13 -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#include <stdlib.h>

void tachyonTrayInstall(char *label, char *accessibilityLabel, char *autosaveName,
                        double defaultPreferredPosition);
int tachyonTrayRemove(void);
void tachyonTrayPositionWindow(void *nsWindow, int offset);
*/
import "C"

import (
	"sync/atomic"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// activeDarwinTray routes the AppKit target/action callbacks to Tachyon's one
// Shell. Wails itself is process-singleton, and the compare-and-swap below
// makes a second shell fail closed rather than sending one app's clicks to
// another app's windows.
var activeDarwinTray atomic.Pointer[Shell]

// wireTray deliberately owns the macOS status item rather than reaching into
// Wails' unexported macosSystemTray with reflection, unsafe layout assumptions,
// or a module-cache patch. Wails v3.0.0-beta.16 and upstream master at
// fbdc4e8627ec6a3cee598752830c4c5670c0f5aa expose neither the native status
// item nor setters for NSStatusItem.autosaveName and
// NSStatusBarButton.accessibilityLabel. The small Objective-C companion uses
// those public AppKit APIs while Wails continues to own every window, service,
// shortcut and application lifecycle operation.
func (s *Shell) wireTray() {
	if !activeDarwinTray.CompareAndSwap(nil, s) {
		s.log.Error("native tray already belongs to another shell")
		return
	}

	s.app.Event.OnApplicationEvent(events.Common.ApplicationStarted,
		func(*application.ApplicationEvent) {
			// Wails dispatches application events on goroutines. Serialize the
			// active-shell check and native installation on AppKit's main thread
			// with shutdown, so a late event cannot recreate an item after its
			// shutdown hook removed it.
			application.InvokeSync(func() {
				if activeDarwinTray.Load() != s {
					return
				}
				label := C.CString(trayLabel)
				accessibilityLabel := C.CString(trayAccessibilityLabel)
				autosaveName := C.CString(trayAutosaveName)
				defer C.free(unsafe.Pointer(label))
				defer C.free(unsafe.Pointer(accessibilityLabel))
				defer C.free(unsafe.Pointer(autosaveName))
				C.tachyonTrayInstall(label, accessibilityLabel, autosaveName,
					C.double(trayDefaultPreferredPosition))
			})
		})

	s.app.OnShutdown(func() {
		application.InvokeSync(func() {
			if activeDarwinTray.CompareAndSwap(s, nil) {
				if C.tachyonTrayRemove() == 0 {
					s.log.Error("native tray preferences did not synchronize during shutdown")
				}
			}
		})
	})
}

//export tachyonTrayTogglePalette
func tachyonTrayTogglePalette() {
	s := activeDarwinTray.Load()
	if s == nil {
		return
	}
	application.InvokeSync(func() {
		if s.palette.IsVisible() {
			s.palette.Hide()
			return
		}
		// NativeWindow is Wails' supported escape hatch for integrations that
		// need a platform window. AppKit positions it under our public status
		// item before Wails shows/focuses it, matching AttachWindow's behavior.
		if window := s.palette.NativeWindow(); window != nil {
			C.tachyonTrayPositionWindow(window, 6)
		}
		s.palette.Show()
		s.palette.Focus()
	})
}

//export tachyonTrayOpenManager
func tachyonTrayOpenManager() {
	if s := activeDarwinTray.Load(); s != nil {
		s.OpenManager()
	}
}

//export tachyonTrayOpenSettings
func tachyonTrayOpenSettings() {
	if s := activeDarwinTray.Load(); s != nil {
		s.OpenManagerSettings()
	}
}

//export tachyonTrayQuit
func tachyonTrayQuit() {
	if s := activeDarwinTray.Load(); s != nil {
		s.app.Quit()
	}
}
