package state_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/state"
)

// legacyFixture builds a fake pre-XDG tree and points os.UserConfigDir()'s
// lookup at its parent, so Adopt sees <parent>/Tachyon as the legacy root
// without any test touching a real machine's Application Support.
//
// XDG_CONFIG_HOME is redirected in the same breath, because Adopt's target
// is state.ConfigDir() and the two must land in the same scratch tree for
// the move to be observable. DirEnv is deliberately NOT used: it disables
// adoption by design (see state.LegacyRoot), which is the behavior
// TestAdoptIsDisabledUnderTheOverride pins.
func legacyFixture(t *testing.T, seed map[string]string) (legacyRoot, configDir string) {
	t.Helper()
	scratch := t.TempDir()

	t.Setenv(state.DirEnv, "")
	// os.UserConfigDir reads XDG_CONFIG_HOME on Linux and ignores it on
	// macOS, where it is always ~/Library/Application Support. HOME is what
	// moves it on both.
	t.Setenv("HOME", scratch)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(scratch, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(scratch, ".local", "state"))

	legacyRoot = state.LegacyRoot()
	if legacyRoot == "" {
		t.Skip("no user config dir on this machine")
	}
	if !strings.HasPrefix(legacyRoot, scratch) {
		t.Skipf("legacy root %q did not follow HOME into the scratch tree %q; this platform resolves it elsewhere", legacyRoot, scratch)
	}
	if err := os.MkdirAll(legacyRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", legacyRoot, err)
	}
	for name, content := range seed {
		if err := os.WriteFile(filepath.Join(legacyRoot, name), []byte(content), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}

	configDir, err := state.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	return legacyRoot, configDir
}

// TestAdoptMovesLegacyFilesOnce is the other half of
// internal/shell.TestDefaultPrefsPathIsUnderConfigDir: the relocation is
// only safe because the old files follow. It checks the content survives
// byte for byte -- a rebound hotkey that arrives corrupted is no better than
// one that does not arrive -- and that a second run is a no-op rather than a
// second move.
func TestAdoptMovesLegacyFilesOnce(t *testing.T) {
	const hotkey = `{"hotkey":"ctrl+option+space"}` + "\n"
	const projects = `{"projects":[{"name":"Cairn","path":"~/dev/projects/cairn"}]}` + "\n"
	const bundleRoot = `{"root":"/Users/somebody/dev/projects/agent-setup"}` + "\n"

	legacy, config := legacyFixture(t, map[string]string{
		"shell.json":    hotkey,
		"projects.json": projects,
		"bundle.json":   bundleRoot,
	})

	if err := state.Adopt(io.Discard); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	for name, want := range map[string]string{
		"shell.json":    hotkey,
		"projects.json": projects,
		"bundle.json":   bundleRoot,
	} {
		got, err := os.ReadFile(filepath.Join(config, name))
		if err != nil {
			t.Fatalf("after Adopt, reading %s from the config dir: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q after adoption; want the legacy content %q", name, got, want)
		}
		if _, err := os.Stat(filepath.Join(legacy, name)); !os.IsNotExist(err) {
			t.Errorf("%s is still in the legacy root; Adopt moves rather than copies", name)
		}
	}

	// Idempotent: a second run has nothing to move and must not error.
	if err := state.Adopt(io.Discard); err != nil {
		t.Fatalf("second Adopt: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(config, "shell.json")); err != nil || string(got) != hotkey {
		t.Fatalf("shell.json after a second Adopt = (%q, %v); want it untouched", got, err)
	}
}

// TestAdoptNeverClobbers is the third case in Adopt's own rule, and the one
// that must never lose data: both files present means leave both. Picking a
// winner would discard whatever the person did since the move.
func TestAdoptNeverClobbers(t *testing.T) {
	const legacyContent = `{"hotkey":"the-old-one"}` + "\n"
	const currentContent = `{"hotkey":"the-one-in-use"}` + "\n"

	legacy, config := legacyFixture(t, map[string]string{"shell.json": legacyContent})

	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", config, err)
	}
	current := filepath.Join(config, "shell.json")
	if err := os.WriteFile(current, []byte(currentContent), 0o644); err != nil {
		t.Fatalf("seeding the current shell.json: %v", err)
	}

	var log strings.Builder
	if err := state.Adopt(&log); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if got, err := os.ReadFile(current); err != nil || string(got) != currentContent {
		t.Fatalf("the file in use = (%q, %v); Adopt must not overwrite it with the legacy one", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(legacy, "shell.json")); err != nil || string(got) != legacyContent {
		t.Fatalf("the legacy file = (%q, %v); Adopt must leave it where it is so the two can be merged by hand", got, err)
	}
	if !strings.Contains(log.String(), "shell.json") {
		t.Errorf("Adopt said nothing about the collision; the report is what makes leaving both an answer rather than a silence. Got: %q", log.String())
	}
}

// TestAdoptIsDisabledUnderTheOverride keeps a test that redirects Tachyon's
// footprint from ever being handed a path into the real machine's
// Application Support.
func TestAdoptIsDisabledUnderTheOverride(t *testing.T) {
	t.Setenv(state.DirEnv, t.TempDir())

	if got := state.LegacyRoot(); got != "" {
		t.Fatalf("LegacyRoot() with %s set = %q; want \"\"", state.DirEnv, got)
	}
	if err := state.Adopt(io.Discard); err != nil {
		t.Fatalf("Adopt under the override: %v", err)
	}
}

// TestAdoptDoesNotMoveBootDirectories pins the deliberate omission. Boot
// directories are disposable, and the harness trust entry that would make
// them worth keeping keys on the path -- which is the thing that changed.
func TestAdoptDoesNotMoveBootDirectories(t *testing.T) {
	legacy, config := legacyFixture(t, map[string]string{"shell.json": "{}\n"})

	bootDir := filepath.Join(legacy, "boot", "engineer", "current")
	if err := os.MkdirAll(bootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", bootDir, err)
	}

	if err := state.Adopt(io.Discard); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if _, err := os.Stat(bootDir); err != nil {
		t.Errorf("the legacy boot directory is gone (%v); Adopt must leave it rather than move or delete it", err)
	}
	if _, err := os.Stat(filepath.Join(config, "boot")); !os.IsNotExist(err) {
		t.Errorf("a boot/ directory appeared under the config root; boot directories belong under state.StateDir()")
	}
}
