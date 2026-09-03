package shell

// Service is the shell's surface to the frontend. It is bound through
// application.NewService, so its exported methods are callable from JavaScript
// as "<package path>.Service.<Method>".
//
// It is deliberately thin: everything here is window and hotkey management.
// The bundle, composition and launch surfaces are separate services in later
// tasks.
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

func (s *Shell) settingsView() SettingsView {
	p := s.prefs.Get()
	return SettingsView{
		Hotkey:        p.Hotkey,
		DefaultHotkey: DefaultHotkey,
		Registered:    s.hotkey.current() == p.Hotkey,
		Path:          s.prefs.Path(),
	}
}
