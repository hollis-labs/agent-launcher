package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/shell"
	"github.com/hollis-labs/tachyon/internal/state"
)

// TestDirDefaultsToOSUserConfigDir proves that, absent an override, [state.Dir]
// returns exactly what os.UserConfigDir() returns — Dir does not change
// default behavior, only add an override on top of it.
func TestDirDefaultsToOSUserConfigDir(t *testing.T) {
	t.Setenv(state.DirEnv, "")

	want, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no user config dir on this machine: %v", err)
	}
	got, err := state.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != want {
		t.Fatalf("Dir() = %q; want os.UserConfigDir()'s %q", got, want)
	}
}

// TestDirOverride is the override mechanism itself: setting DirEnv redirects
// Dir entirely, without touching the real per-user config directory.
func TestDirOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "somewhere-else")
	t.Setenv(state.DirEnv, override)

	got, err := state.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != override {
		t.Fatalf("Dir() with %s=%q = %q; want %q", state.DirEnv, override, got, override)
	}
}

// TestRootIsDirPlusTachyon pins the shape Root builds on top of Dir, using
// the override so the test needs no real per-user config directory.
func TestRootIsDirPlusTachyon(t *testing.T) {
	override := t.TempDir()
	t.Setenv(state.DirEnv, override)

	got, err := state.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	want := filepath.Join(override, "Tachyon")
	if got != want {
		t.Fatalf("Root() = %q; want %q", got, want)
	}
}

// TestBootRootIsRootPlusBoot pins the shape BootRoot builds on top of Root.
func TestBootRootIsRootPlusBoot(t *testing.T) {
	override := t.TempDir()
	t.Setenv(state.DirEnv, override)

	root, err := state.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
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
		t.Skipf("no user config dir on this machine: %v", err)
	}
	devAgentOS := filepath.Join(home, "dev", "agent-os")
	if strings.HasPrefix(root, devAgentOS) {
		t.Fatalf("BootRoot() = %q; falls under %q, the live git working tree D9 exists to avoid", root, devAgentOS)
	}
}

// TestStateRootMovesEveryConsumerTogether is the property the task exists
// to prove: changing where Tachyon's state root points is a one-line change
// in one function ([state.Dir], via its override), and every consumer — the
// boot root this package computes directly, the shell's preferences path,
// and the bundle-root store's path — follows it, together, from that one
// change.
//
// This is a cross-package test on purpose: internal/shell and
// internal/bundle both import internal/state (never the reverse), so this
// test importing all three from internal/state's own test package is not a
// cycle — the same pattern internal/shell/bundle_root_test.go already uses
// to test shell against bundle.
func TestStateRootMovesEveryConsumerTogether(t *testing.T) {
	overrideA := filepath.Join(t.TempDir(), "state-a")
	overrideB := filepath.Join(t.TempDir(), "state-b")

	check := func(t *testing.T, override string) (bootRoot, prefsPath, bundlePath string) {
		t.Helper()
		t.Setenv(state.DirEnv, override)

		bootRoot, err := state.BootRoot()
		if err != nil {
			t.Fatalf("BootRoot: %v", err)
		}
		prefsPath, err = shell.DefaultPrefsPath()
		if err != nil {
			t.Fatalf("shell.DefaultPrefsPath: %v", err)
		}
		store, err := bundle.DefaultRootStore()
		if err != nil {
			t.Fatalf("bundle.DefaultRootStore: %v", err)
		}
		bundlePath = store.Path

		// Every one of the three must actually be rooted under this
		// override -- not just distinct from the other override's values,
		// but genuinely nested under the directory the override just set.
		for name, p := range map[string]string{
			"boot root":         bootRoot,
			"shell prefs path":  prefsPath,
			"bundle store path": bundlePath,
		} {
			if !strings.HasPrefix(p, override) {
				t.Fatalf("%s = %q; want it under override %q", name, p, override)
			}
		}
		return bootRoot, prefsPath, bundlePath
	}

	bootA, prefsA, bundleA := check(t, overrideA)
	bootB, prefsB, bundleB := check(t, overrideB)

	if bootA == bootB {
		t.Errorf("boot root did not move: %q under both overrides", bootA)
	}
	if prefsA == prefsB {
		t.Errorf("shell prefs path did not move: %q under both overrides", prefsA)
	}
	if bundleA == bundleB {
		t.Errorf("bundle store path did not move: %q under both overrides", bundleA)
	}
}
