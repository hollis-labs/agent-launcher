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
		`const [composeBinding, setComposeBinding] = useState("")`,
		`setComposeBinding(name)`,
		`const target = bindingless ? baseProfile.trim() : composeOpen ? composeBinding : activeTarget?.name`,
		`targetName={bindingless ? baseProfile.trim() : composeBinding}`,
		`const [baseProfile, setBaseProfile] = useState("")`,
		`list="compose-profile-suggestions"`,
		`list="compose-part-suggestions"`,
		`list="compose-skill-suggestions"`,
		`list="compose-prompt-suggestions"`,
		`list="compose-project-suggestions"`,
		`target,`,
		`Launch.Composition({`,
		`return Shell.HidePalette()`,
		`window.addEventListener("blur", resetComposeDraft)`,
		`if (e.target === e.currentTarget) closeComposition()`,
		`acceptTopSuggestion(e, scope, suggestions, setScope)`,
		`Launch only — this palette never writes a binding.`,
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
// paths for skills/prompts remain reset, append from typed additions, and
// direct chip removal. Backend reflection tests separately ensure Binding has
// no field from which either collection could be inherited.
func TestPaletteAdditionsRemainUserOwned(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")
	for fragment, want := range map[string]int{
		"setSkills(":  3,
		"setPrompts(": 3,
	} {
		if got := strings.Count(source, fragment); got != want {
			t.Errorf("Palette.jsx contains %d occurrences of %q; want %d user-owned setter paths", got, fragment, want)
		}
	}
	for _, required := range []string{
		`const [skills, setSkills] = useState([])`,
		`const [prompts, setPrompts] = useState([])`,
		`setSkills((cur) => [...cur, ...additions])`,
		`setPrompts((cur) => [...cur, ...additions])`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Palette.jsx is missing additive-only fragment %q", required)
		}
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
