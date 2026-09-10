package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/go-apppaths/paths"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/shell"
	"github.com/hollis-labs/tachyon/internal/state"
)

// TestRootsFollowXDG proves the two roots are go-apppaths', not
// os.UserConfigDir()'s. The distinction is the whole point of the move: on
// macOS os.UserConfigDir() is ~/Library/Application Support, and both roots
// used to resolve under it.
func TestRootsFollowXDG(t *testing.T) {
	t.Setenv(state.DirEnv, "")

	layout, err := paths.Resolve("tachyon", paths.WithoutMaterialize())
	if err != nil {
		t.Skipf("cannot resolve app paths on this machine: %v", err)
	}

	config, err := state.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if config != layout.ConfigDir() {
		t.Fatalf("ConfigDir() = %q; want go-apppaths' %q", config, layout.ConfigDir())
	}

	stateDir, err := state.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if stateDir != layout.StateDir() {
		t.Fatalf("StateDir() = %q; want go-apppaths' %q", stateDir, layout.StateDir())
	}
}

// TestConfigAndStateAreDistinct is the split itself. A launch profile a
// person edits and a boot directory Tachyon regenerates must not share a
// tree, or "back this up" and "delete this safely" have no answer. Checked
// with the override set, so it holds on any machine.
func TestConfigAndStateAreDistinct(t *testing.T) {
	override := t.TempDir()
	t.Setenv(state.DirEnv, override)

	config, err := state.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	stateDir, err := state.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}

	if config == stateDir {
		t.Fatalf("ConfigDir() and StateDir() are the same directory (%q); the override must redirect the roots, not collapse them", config)
	}
	if strings.HasPrefix(config, stateDir+string(filepath.Separator)) ||
		strings.HasPrefix(stateDir, config+string(filepath.Separator)) {
		t.Fatalf("one root nests inside the other: config %q, state %q", config, stateDir)
	}
}

// TestOverrideRedirectsBothRoots is the override mechanism: setting DirEnv
// moves both roots under it, without touching the real per-user
// directories.
func TestOverrideRedirectsBothRoots(t *testing.T) {
	override := filepath.Join(t.TempDir(), "somewhere-else")
	t.Setenv(state.DirEnv, override)

	for name, fn := range map[string]func() (string, error){
		"ConfigDir": state.ConfigDir,
		"StateDir":  state.StateDir,
	} {
		got, err := fn()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasPrefix(got, override+string(filepath.Separator)) {
			t.Fatalf("%s() with %s=%q = %q; want it under the override", name, state.DirEnv, override, got)
		}
	}
}

// TestBootRootIsStateDirPlusBoot pins the shape BootRoot builds on StateDir.
func TestBootRootIsStateDirPlusBoot(t *testing.T) {
	override := t.TempDir()
	t.Setenv(state.DirEnv, override)

	root, err := state.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	got, err := state.BootRoot()
	if err != nil {
		t.Fatalf("BootRoot: %v", err)
	}
	want := filepath.Join(root, "boot")
	if got != want {
		t.Fatalf("BootRoot() = %q; want %q", got, want)
	}
}

// TestLaunchDirIsConfigDirPlusLaunch pins the launch-profile store's shape,
// and that it is under CONFIG rather than state: a launch profile is a
// durable choice a person wrote, not something Tachyon regenerates.
func TestLaunchDirIsConfigDirPlusLaunch(t *testing.T) {
	override := t.TempDir()
	t.Setenv(state.DirEnv, override)

	config, err := state.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	got, err := state.LaunchDir()
	if err != nil {
		t.Fatalf("LaunchDir: %v", err)
	}
	want := filepath.Join(config, "launch")
	if got != want {
		t.Fatalf("LaunchDir() = %q; want %q", got, want)
	}

	stateDir, err := state.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if strings.HasPrefix(got, stateDir) {
		t.Fatalf("LaunchDir() = %q; falls under the state root %q, but launch profiles are config", got, stateDir)
	}
}

// TestBootRootNeverUnderDevAgentOS is this package's own half of the D9
// guard: with no override, the boot root this package computes must never
// fall under ~/dev/agent-os, the live git working tree Cairn's own default
// boot root (dev/agent-os/runtime/boot under $HOME) would plant inside.
func TestBootRootNeverUnderDevAgentOS(t *testing.T) {
	t.Setenv(state.DirEnv, "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	root, err := state.BootRoot()
	if err != nil {
		t.Skipf("cannot resolve app paths on this machine: %v", err)
	}
	devAgentOS := filepath.Join(home, "dev", "agent-os")
	if strings.HasPrefix(root, devAgentOS) {
		t.Fatalf("BootRoot() = %q; falls under %q, the live git working tree D9 exists to avoid", root, devAgentOS)
	}
}

// TestStateRootMovesEveryConsumerTogether is the property this package
// exists to prove: changing where Tachyon's files live is a one-variable
// change, and every consumer — the boot root, the launch-profile store, the
// shell's preferences path, and the bundle-root store's path — follows it,
// together, from that one change.
//
// This is a cross-package test on purpose: internal/shell and
// internal/bundle both import internal/state (never the reverse), so this
// test importing all three from internal/state's own test package is not a
// cycle — the same pattern internal/shell/bundle_root_test.go already uses
// to test shell against bundle.
func TestStateRootMovesEveryConsumerTogether(t *testing.T) {
	overrideA := filepath.Join(t.TempDir(), "state-a")
	overrideB := filepath.Join(t.TempDir(), "state-b")

	check := func(t *testing.T, override string) map[string]string {
		t.Helper()
		t.Setenv(state.DirEnv, override)

		bootRoot, err := state.BootRoot()
		if err != nil {
			t.Fatalf("BootRoot: %v", err)
		}
		launchDir, err := state.LaunchDir()
		if err != nil {
			t.Fatalf("LaunchDir: %v", err)
		}
		prefsPath, err := shell.DefaultPrefsPath()
		if err != nil {
			t.Fatalf("shell.DefaultPrefsPath: %v", err)
		}
		store, err := bundle.DefaultRootStore()
		if err != nil {
			t.Fatalf("bundle.DefaultRootStore: %v", err)
		}

		paths := map[string]string{
			"boot root":         bootRoot,
			"launch dir":        launchDir,
			"shell prefs path":  prefsPath,
			"bundle store path": store.Path,
		}

		// Every one must actually be rooted under this override -- not just
		// distinct from the other override's values, but genuinely nested
		// under the directory the override just set.
		for name, p := range paths {
			if !strings.HasPrefix(p, override) {
				t.Fatalf("%s = %q; want it under override %q", name, p, override)
			}
		}
		return paths
	}

	a := check(t, overrideA)
	b := check(t, overrideB)

	for name, pathA := range a {
		if pathA == b[name] {
			t.Errorf("%s did not move: %q under both overrides", name, pathA)
		}
	}
}
