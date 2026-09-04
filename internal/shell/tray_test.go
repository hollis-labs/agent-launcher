package shell

import "testing"

type recordingTrayPresentation struct {
	label   string
	tooltip string
}

func (r *recordingTrayPresentation) SetLabel(label string) {
	r.label = label
}

func (r *recordingTrayPresentation) SetTooltip(tooltip string) {
	r.tooltip = tooltip
}

func TestConfigureTrayPresentationUsesVisibleLabel(t *testing.T) {
	var tray recordingTrayPresentation

	configureTrayPresentation(&tray)

	if tray.label != "⌁" {
		t.Fatalf("label = %q, want visible Tachyon mark", tray.label)
	}
	if tray.tooltip != "Tachyon" {
		t.Fatalf("tooltip = %q, want %q", tray.tooltip, "Tachyon")
	}
}

func TestDarwinTrayContractHasStablePublicIdentities(t *testing.T) {
	if trayAccessibilityLabel != "Tachyon" {
		t.Fatalf("accessibility label = %q, want Tachyon", trayAccessibilityLabel)
	}
	if trayAutosaveName != "com.hollislabs.tachyon.main-status-item" {
		t.Fatalf("autosave name = %q, want stable bundle-scoped name", trayAutosaveName)
	}
	if trayAutosaveName == trayLabel {
		t.Fatal("autosave identity must not derive from the visible glyph")
	}
	if trayDefaultPreferredPosition != 220 {
		t.Fatalf("first-run preferred position = %v, want target-machine verified fallback 220", trayDefaultPreferredPosition)
	}
}
