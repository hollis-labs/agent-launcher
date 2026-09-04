package shell

import (
	"context"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// Service is the shell's surface to the frontend. It is bound through
// application.NewService, so its exported methods are callable from JavaScript
// as "<package path>.Service.<Method>".
//
// It is deliberately thin: everything here is window and hotkey management,
// plus (CW-20260903-0019) the one manual action the manager's Settings
// pane offers to clean up old boot directories. The bundle, composition
// and launch surfaces are separate services in other tasks.
type Service struct {
	shell *Shell
}

// SettingsView is what the settings pane renders.
type SettingsView struct {
	// Hotkey is the accelerator recorded in preferences.
	Hotkey string `json:"hotkey"`
	// DefaultHotkey lets the pane offer a reset without hardcoding it.
	DefaultHotkey string `json:"defaultHotkey"`
	// Registered reports whether the OS accepted the current accelerator.
	//
	// True does NOT mean the hotkey fires. macOS accepts a registration that
	// collides with another application and never delivers the key. The pane
	// says so; do not present this as a health check.
	Registered bool `json:"registered"`
	// Path is the preferences file, shown so the user can find it.
	Path string `json:"path"`
}

// RebindResult reports the outcome of a hotkey change.
type RebindResult struct {
	Settings SettingsView `json:"settings"`
	// Advisory is non-empty when the new accelerator is known to be claimed by
	// something else. The rebind still happened.
	Advisory string `json:"advisory"`
}

// Settings returns the current shell settings.
func (s *Service) Settings() SettingsView {
	return s.shell.settingsView()
}

// ValidateHotkey reports why an accelerator would be rejected, or "" if it
// would be accepted. It does not change anything.
func (s *Service) ValidateHotkey(accelerator string) string {
	if err := ValidateAccelerator(accelerator); err != nil {
		return err.Error()
	}
	return ""
}

// SetHotkey rebinds the global hotkey and persists it.
func (s *Service) SetHotkey(accelerator string) (RebindResult, error) {
	if err := s.shell.Rebind(accelerator); err != nil {
		return RebindResult{}, err
	}
	return RebindResult{
		Settings: s.shell.settingsView(),
		Advisory: AdvisoryFor(accelerator),
	}, nil
}

// OpenManager shows the manager window.
func (s *Service) OpenManager() { s.shell.OpenManager() }

// HidePalette hides the palette window.
func (s *Service) HidePalette() { s.shell.HidePalette() }

// SweepBootDirectories is the manual action the manager's Settings pane
// offers for CW-20260903-0019's .prev-* sweep: it calls the exact same
// [Shell.SweepBootDirectories] method the app already calls once, in the
// background, right after startup ([Shell.wireBootSweep]) — never a
// second implementation of the sweep, never a different boot root or a
// different lsof resolution. A person can use this to clean up
// immediately after relaunching a binding rather than waiting for the
// next full app restart, or simply to see the report (what was removed,
// what was kept, and why) on demand.
//
// This is bound only to this button. There is no timer anywhere in this
// package that calls it, and there must never be one — see
// [Shell.wireBootSweep]'s doc.
func (s *Service) SweepBootDirectories() (boot.Report, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bootSweepTimeout)
	defer cancel()
	return s.shell.SweepBootDirectories(ctx)
}

func (s *Shell) settingsView() SettingsView {
	p := s.prefs.Get()
	return SettingsView{
		Hotkey:        p.Hotkey,
		DefaultHotkey: DefaultHotkey,
		Registered:    s.hotkey.current() == p.Hotkey,
		Path:          s.prefs.Path(),
	}
}
