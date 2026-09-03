package shell

import "testing"

func TestValidateAcceleratorRejectsBareKey(t *testing.T) {
	// The load-bearing case. Wails' parseAccelerator accepts a single
	// component as a key with no modifiers, and Register("K") returns nil
	// while binding the unmodified K system-wide. Nothing downstream catches
	// it, so this validator has to.
	for _, s := range []string{"K", "Space", "F1", "escape"} {
		if err := ValidateAccelerator(s); err == nil {
			t.Errorf("ValidateAccelerator(%q) = nil, want an error: a bare key binds system-wide", s)
		}
	}
}

func TestValidateAcceleratorRejectsMalformed(t *testing.T) {
	for _, tc := range []struct{ in, why string }{
		{"", "empty"},
		{"   ", "blank"},
		{"Ctrl+", "no key"},
		{"Meta+K", "Meta is not a Wails modifier name"},
		{"Ctrl+Shft+K", "misspelled modifier"},
	} {
		if err := ValidateAccelerator(tc.in); err == nil {
			t.Errorf("ValidateAccelerator(%q) = nil, want an error (%s)", tc.in, tc.why)
		}
	}
}

func TestValidateAcceleratorAcceptsModifiedKeys(t *testing.T) {
	for _, s := range []string{
		DefaultHotkey,
		"Ctrl+Option+Space",
		"CmdOrCtrl+Shift+P",
		"cmd+k",
		"Alt+Shift+F1",
		"Super+Space",
		"Ctrl+plus",
	} {
		if err := ValidateAccelerator(s); err != nil {
			t.Errorf("ValidateAccelerator(%q) = %v, want nil", s, err)
		}
	}
}

func TestDefaultHotkeyIsValid(t *testing.T) {
	if err := ValidateAccelerator(DefaultHotkey); err != nil {
		t.Fatalf("DefaultHotkey %q does not validate: %v", DefaultHotkey, err)
	}
	if a := AdvisoryFor(DefaultHotkey); a != "" {
		t.Errorf("DefaultHotkey %q carries an advisory: %s", DefaultHotkey, a)
	}
}

func TestAdvisoryFor(t *testing.T) {
	// Advisory lookup has to survive the spellings a user actually types:
	// modifier order, casing, and the Cmd/Command/CmdOrCtrl aliases all name
	// the same combination.
	for _, s := range []string{
		"Cmd+Shift+Space",
		"Shift+Cmd+Space",
		"cmdorctrl+shift+space",
		"Command+Shift+Space",
	} {
		if AdvisoryFor(s) == "" {
			t.Errorf("AdvisoryFor(%q) = \"\", want the 1Password advisory", s)
		}
	}
	if got := AdvisoryFor("Ctrl+Option+J"); got != "" {
		t.Errorf("AdvisoryFor(unclaimed) = %q, want \"\"", got)
	}
}
