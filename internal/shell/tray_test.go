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
