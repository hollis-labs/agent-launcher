//go:build !darwin

package shell

import "github.com/wailsapp/wails/v3/pkg/application"

// wireTray keeps Wails' portable tray implementation on non-macOS platforms.
// macOS uses tray_darwin.go so it can set the AppKit-only autosave and
// accessibility properties that Wails does not currently expose.
func (s *Shell) wireTray() {
	menu := application.NewMenu()
	menu.Add("Open Manager").OnClick(func(*application.Context) { s.OpenManager() })
	menu.Add("Settings…").OnClick(func(*application.Context) { s.OpenManagerSettings() })
	menu.AddSeparator()
	menu.Add("Quit Tachyon").OnClick(func(*application.Context) { s.app.Quit() })

	tray := s.app.SystemTray.New()
	configureTrayPresentation(tray)
	tray.AttachWindow(s.palette).WindowOffset(6)
	tray.SetMenu(menu)
}
