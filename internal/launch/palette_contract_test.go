package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPaletteBindinglessCompositionContract pins the JavaScript side of
// T33's control-to-service wiring. This repository has no browser test
// harness; the task explicitly requires a no-save grep, so keeping that
// source-level contract executable is stronger than a one-time manual grep.
// The Go mapping and real-Cairn tests in launch_test.go cover the behavior on
// the other side of Launch.Composition.
func TestPaletteBindinglessCompositionContract(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")

	for _, required := range []string{
		`import { Binding, Launch, Manager, Project, Shell } from "./bridge.js"`,
		`Manager.Tree()`,
		`Project.List()`,
		`New one-time composition`,
		`className="compose-modal-backdrop"`,
		`role="dialog"`,
		`aria-modal="true"`,
		`className="compose-modal-footer"`,
		`className="palette-row-compose"`,
		`onDoubleClick={() => onLaunch(b.name)}`,
		`Launch.Binding(name)`,
		`compositionDraftReducer`,
		`compositionInput(composition)`,
		`dispatchComposition({ type: "OPEN_BINDING", target: name })`,
		`dispatchComposition({ type: "OPEN_PROFILE" })`,
		`targetName={composeTarget}`,
		`targetDraft: baseProfile`,
		`list="compose-profile-suggestions"`,
		`list="compose-part-suggestions"`,
		`list="compose-skill-suggestions"`,
		`list="compose-prompt-suggestions"`,
		`list="compose-project-suggestions"`,
		`Launch.Composition(input)`,
		`return Shell.HidePalette()`,
		`window.addEventListener("blur", retainOpenDraft)`,
		`dispatchComposition({ type: "HIDE" })`,
		`acceptTopSuggestion(e, scope, suggestions, setScope)`,
		`Launch only — this palette never writes a binding.`,
		// CW-20260906-0001: the provider control. Datalist-backed like every
		// other control here, for the reason the guard below states.
		`id="compose-provider-input"`,
		`list="compose-provider-suggestions"`,
		`Provider (--provider)`,
		`acceptTopSuggestion(e, provider, providerSuggestions, setProvider)`,
		`const setProvider = (value) => updateCompositionField("provider", value);`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Palette.jsx is missing T33 contract fragment %q", required)
		}
	}

	// Datalist-backed inputs are suggestions, not validation. A select would
	// restrict values to the bundle/project census and violate D8.
	if strings.Contains(source, "<select") {
		t.Error("Palette.jsx contains <select; T33 controls must accept free text")
	}

	// The palette launches. Only the manager's BindingComposer authors.
	for _, forbidden := range []string{
		"BindingComposer",
		"ComposerAPI",
		"Manager.Save(",
		"Binding.Create(",
		"Binding.Update(",
		"Binding.Delete(",
		".Save(",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("Palette.jsx contains forbidden save path %q", forbidden)
		}
	}
}

// TestPaletteAdditionsRemainUserOwned guards the frontend half of the
// additive-only property. Suggestions may fill datalists, but the only setter
// paths for skills/prompts remain reducer actions dispatched by typed
// additions and direct chip removal. Backend reflection tests separately
// ensure Binding has no field from which either collection could be inherited.
func TestPaletteAdditionsRemainUserOwned(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")
	for _, required := range []string{
		`dispatchComposition({ type: "ADD_SKILLS", values: additions })`,
		`dispatchComposition({ type: "REMOVE_SKILL", value: name })`,
		`dispatchComposition({ type: "ADD_PROMPTS", values: additions })`,
		`dispatchComposition({ type: "REMOVE_PROMPT", value: name })`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Palette.jsx is missing additive-only fragment %q", required)
		}
	}
	for _, forbidden := range []string{
		`skills: binding.`,
		`skills: profile.`,
		`prompts: binding.`,
		`prompts: profile.`,
		// The provider is chosen, never derived. A control seeded from the
		// target's own name would render a Codex layout the first time
		// somebody named a binding after a project rather than a tool.
		`provider: binding.`,
		`provider: profile.`,
		`startsWith("codex`,
		`startsWith('codex`,
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("Palette.jsx contains inherited-addition path %q", forbidden)
		}
	}
}

// TestPaletteDraftLifecycleContract covers the DOM/native wiring around the
// reducer's executable lifecycle tests. Passive backdrop clicks do nothing;
// only the two plainly labelled actions clear state, and the retained marker
// is rendered when native dismissal has hidden an open draft.
func TestPaletteDraftLifecycleContract(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")
	for _, required := range []string{
		`Draft retained while the palette was hidden`,
		`Discard &amp; close`,
		`dispatchComposition({ type: "DISCARD" })`,
		`dispatchComposition({ type: "CLEAR" })`,
		`if (activeCompositionLaunchRef.current !== draftId) return undefined`,
		`dispatchComposition({ type: "LAUNCH_SUCCESS", draftId })`,
		`type: "LAUNCH_FAILURE"`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Palette.jsx is missing lifecycle fragment %q", required)
		}
	}

	backdropStart := strings.Index(source, `className="compose-modal-backdrop"`)
	if backdropStart < 0 {
		t.Fatal("composition backdrop is missing")
	}
	backdropEnd := strings.Index(source[backdropStart:], `<section`)
	if backdropEnd < 0 {
		t.Fatal("could not isolate composition backdrop element")
	}
	if strings.Contains(source[backdropStart:backdropStart+backdropEnd], "onMouse") ||
		strings.Contains(source[backdropStart:backdropStart+backdropEnd], "onClick") {
		t.Error("composition backdrop still has a passive-click action")
	}
	if strings.Contains(source, `if (e.target === e.currentTarget)`) {
		t.Error("composition backdrop still has a passive-click close path")
	}
}

func TestDirectBindingLaunchStaysCompositionFree(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")
	start := strings.Index(source, "function launchBinding(name)")
	if start < 0 {
		t.Fatal("could not find launchBinding in Palette.jsx")
	}
	relEnd := strings.Index(source[start:], "function openBindingComposition")
	if relEnd <= 0 {
		t.Fatal("could not isolate launchBinding in Palette.jsx")
	}
	end := start + relEnd
	direct := source[start:end]
	if !strings.Contains(direct, "Launch.Binding(name)") {
		t.Error("direct launch does not call Launch.Binding")
	}
	if strings.Contains(direct, "Composition") {
		t.Error("direct launch reaches composition state or service")
	}
	if !strings.Contains(source, `launchBinding(activeTarget?.name)`) {
		t.Error("search Enter is not wired to the direct saved-binding path")
	}
}

// TestPaletteNativeDismissalPostureRemainsEnabled covers the native half of
// dismissal. Palette.jsx's blur listener and success hide are asserted above;
// both Wails window options must remain enabled for Escape and click-away.
func TestPaletteNativeDismissalPostureRemainsEnabled(t *testing.T) {
	source := readRepoFile(t, "internal", "shell", "shell.go")
	start := strings.Index(source, "func paletteOptions()")
	end := strings.Index(source, "func managerOptions(")
	if start < 0 || end <= start {
		t.Fatal("could not isolate paletteOptions in internal/shell/shell.go")
	}
	options := source[start:end]
	for _, required := range []string{
		"HideOnEscape:    true",
		"HideOnFocusLost: true",
	} {
		if !strings.Contains(options, required) {
			t.Errorf("paletteOptions is missing %q", required)
		}
	}
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	pathParts := append([]string{"..", ".."}, parts...)
	path := filepath.Join(pathParts...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
