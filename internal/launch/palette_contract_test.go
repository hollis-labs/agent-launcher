package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPaletteCompositionContract pins the JavaScript side of the
// control-to-service wiring. This repository has no browser test harness, so
// keeping the source-level contract executable is stronger than a one-time
// manual grep. The Go mapping and real-cairn tests in launch_test.go cover
// the behavior on the other side of Launch.Composition.
func TestPaletteCompositionContract(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")

	for _, required := range []string{
		`import { Launch, LaunchProfile, Manager, Project, Shell } from "./bridge.js"`,
		`Manager.Tree()`,
		`Project.List()`,
		// The two lists the palette launches from, and the reason they are
		// separate calls: one is the bundle's, one is Tachyon's own.
		`Launch.Targets()`,
		`LaunchProfile.List()`,
		`className="compose-modal-backdrop"`,
		`role="dialog"`,
		`aria-modal="true"`,
		`className="compose-modal-footer"`,
		`className="palette-row-compose"`,
		`onDoubleClick={() => onLaunch(t.id)}`,
		`compositionDraftReducer`,
		`compositionInput(composition)`,
		`dispatchComposition({ type: "OPEN_TARGET", target: id, defaults: draftDefaults() })`,
		`dispatchComposition({ type: "OPEN_BLANK", defaults: draftDefaults() })`,
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
		`Launch only — save a launch profile from the manager.`,
		// The launch profile control, and the fact that it is a NAME.
		`id="compose-launch-profile"`,
		`const setComposeLaunchProfile = (value) => updateCompositionField("launchProfile", value);`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Palette.jsx is missing contract fragment %q", required)
		}
	}

	// THE PROVIDER CONTROL MUST NOT COME BACK.
	//
	// It was a free-text field rendering --provider. That flag is gone: no
	// profile in agent-setup declares a provider, so cairn's own default
	// resolves to nothing and it refuses to render rather than writing one
	// harness's files into another's directory. The provider is declared in
	// the launch profile and folded in by cairn's cascade, so a control here
	// would be a second source for one value and the two could disagree.
	for _, forbidden := range []string{
		`id="compose-provider-input"`,
		`Provider (--provider)`,
		`"--provider"`,
		`updateCompositionField("provider"`,
		`PROVIDER_SUGGESTIONS`,
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("Palette.jsx has a provider control again (%q); the provider belongs to the launch profile", forbidden)
		}
	}

	// The palette launches. Only the manager's composer authors.
	for _, forbidden := range []string{
		"LaunchComposer",
		"ComposerAPI",
		"Manager.Save(",
		"LaunchProfile.Save(",
		"LaunchProfile.Create(",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("Palette.jsx contains forbidden save path %q", forbidden)
		}
	}
}

// TestPaletteKeyboardIsNotGatedOnOneElementsFocus is a regression guard for
// a bug that shipped and was invisible to every test here.
//
// The arrows and Enter used to be the search input's own onKeyDown, which
// made the palette's PRIMARY ACTION depend on that one element holding
// focus. On macOS, clicking a <button> does not focus it — the platform
// convention — so a single click anywhere in the window moved focus to BODY
// and Enter reached nothing from then on. Measured in the app's own
// diagnostic log: activeElement=BODY immediately after a click, with no key
// handler firing again.
//
// Focusing the input on every summon is a convenience and does NOT fix it:
// it restores focus once, and the next click takes it away again. The
// handler has to be on the window.
//
// This is checked at the source level because there is no browser test
// harness here, and because the failure mode is a palette that renders
// perfectly and does nothing — the state was verifiably healthy the whole
// time it was broken.
func TestPaletteKeyboardIsNotGatedOnOneElementsFocus(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")

	for _, required := range []string{
		`window.addEventListener("keydown", paletteKeyDown)`,
		`window.removeEventListener("keydown", paletteKeyDown)`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Palette.jsx is missing %q; the palette's keyboard must not depend on one element holding focus", required)
		}
	}

	// The search input must NOT carry the key handler as well: with both
	// bound, Enter fires twice, and launchTarget's own re-entry guard is set
	// asynchronously so the second call can get through before it lands.
	if strings.Contains(source, "onKeyDown={paletteKeyDown}") {
		t.Error("Palette.jsx binds paletteKeyDown to an element as well as the window; Enter would fire twice")
	}
}

// TestPaletteRestrictsOnlyTheTwoControlsItMust is the successor to a blanket
// "no <select> anywhere" rule, and the change is deliberate rather than a
// relaxation.
//
// That rule existed for D8: a datalist suggests, a select validates, and
// Tachyon must not keep a second, necessarily-partial copy of what cairn
// will accept. It still holds for everything whose values are a census of
// the bundle — a part, a skill, a prompt, a scope — all of which stay free
// text.
//
// Two controls are genuinely different in kind. A launch profile is a NAME
// the Go side resolves against its own store, which validates it before
// joining a path; free text there would let a value cross the Wails boundary
// and point cairn's --with at any file on the machine. A project is a record
// Tachyon itself holds. Neither is a guess about cairn's vocabulary.
func TestPaletteRestrictsOnlyTheTwoControlsItMust(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")

	for _, required := range []string{
		// Every free-text control keeps its datalist.
		`list="compose-part-suggestions"`,
		`list="compose-skill-suggestions"`,
		`list="compose-prompt-suggestions"`,
		`list="compose-project-suggestions"`,
		`list="compose-profile-suggestions"`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Palette.jsx lost the datalist %q; suggestion-backed controls must not become restricted", required)
		}
	}

	// Three selects exactly: the two axes above the list, and the launch
	// profile inside the modal. A fourth means something that should suggest
	// started validating.
	if got := strings.Count(source, "<select"); got != 3 {
		t.Errorf("Palette.jsx has %d <select> elements; want exactly 3 (launch profile and project above the list, launch profile in the modal)", got)
	}
}

// TestPaletteAdditionsRemainUserOwned guards the frontend half of the
// additive-only property. Suggestions may fill datalists, but the only setter
// paths for skills/prompts remain reducer actions dispatched by typed
// additions and direct chip removal. Backend reflection tests separately
// ensure launchprofile.Profile has no field from which either collection
// could be inherited.
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
		`skills: target.`,
		`skills: profile.`,
		`prompts: target.`,
		`prompts: profile.`,
		// The launch profile is chosen, never derived. Reading one out of
		// the target's name would render a Codex layout the first time
		// somebody named a profile after a project rather than a tool.
		`launchProfile: target.`,
		`launchProfile: t.id`,
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

// TestDirectLaunchCarriesNothingFromTheDraft: Enter on a row is a complete
// launch, because all three axes are already chosen. What it must never do
// is pick up a part, skill, prompt or set someone left open in the modal —
// a launch that silently carried a stale addition would be wrong in a way
// nothing reports.
//
// It goes through Launch.Composition now rather than a second entry point,
// which is why the fields have to be checked rather than the call: there is
// one launch path, and this is the one caller that passes empty collections.
func TestDirectLaunchCarriesNothingFromTheDraft(t *testing.T) {
	source := readRepoFile(t, "frontend", "src", "Palette.jsx")
	start := strings.Index(source, "function launchTarget(id)")
	if start < 0 {
		t.Fatal("could not find launchTarget in Palette.jsx")
	}
	relEnd := strings.Index(source[start:], "const draftDefaults")
	if relEnd <= 0 {
		t.Fatal("could not isolate launchTarget in Palette.jsx")
	}
	direct := source[start : start+relEnd]

	for _, required := range []string{
		"Launch.Composition({",
		"target: id,",
		"launchProfile,",
		"scope: projectPath,",
		"skills: [],",
		"prompts: [],",
		"sets: [],",
		"parts: [],",
	} {
		if !strings.Contains(direct, required) {
			t.Errorf("the direct launch path is missing %q", required)
		}
	}
	// Nothing from the compose draft may be READ here. Matched on the
	// actual state expressions rather than the bare word, so the comment
	// explaining why this path is separate does not trip its own guard.
	for _, forbidden := range []string{"compositionInput(", "composition.", "composeTarget", "dispatchComposition"} {
		if strings.Contains(direct, forbidden) {
			t.Errorf("the direct launch path reads compose-draft state (%q)", forbidden)
		}
	}
	if !strings.Contains(source, `launchTarget(activeTarget?.id)`) {
		t.Error("search Enter is not wired to the direct launch path")
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
