package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/config"
)

// TestDirDefaultsToDotConfig proves that, absent an override and with
// XDG_CONFIG_HOME unset, [config.Dir] is exactly ~/.config/tachyon — never
// internal/state's os.UserConfigDir()-based tree (Application Support on
// macOS), and never adrg/xdg's own platform-native substitute for an unset
// XDG_CONFIG_HOME (~/Library/Application Support on macOS), which
// go-apppaths deliberately bypasses under the hood. See [Dir]'s own doc for
// why that distinction is the point of this package.
func TestDirDefaultsToDotConfig(t *testing.T) {
	t.Setenv(config.DirEnv, "")
	t.Setenv("XDG_CONFIG_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	got, err := config.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(home, ".config", "tachyon")
	if got != want {
		t.Fatalf("Dir() = %q; want %q", got, want)
	}
}

// TestDirHonorsXDGConfigHome proves XDG_CONFIG_HOME, when set, takes
// priority over the ~/.config default -- the actual point of aligning with
// the Base Directory spec rather than hardcoding ~/.config.
func TestDirHonorsXDGConfigHome(t *testing.T) {
	t.Setenv(config.DirEnv, "")
	xdgHome := filepath.Join(t.TempDir(), "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", xdgHome)

	got, err := config.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(xdgHome, "tachyon")
	if got != want {
		t.Fatalf("Dir() = %q; want %q", got, want)
	}
}

// TestDirOverride is the override mechanism: setting DirEnv redirects Dir
// entirely, without touching the real XDG config directory.
func TestDirOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "somewhere-else")
	t.Setenv(config.DirEnv, override)

	got, err := config.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != override {
		t.Fatalf("Dir() with %s=%q = %q; want %q", config.DirEnv, override, got, override)
	}
}

// TestPathIsDirPlusConfigJSON pins the shape Path builds on top of Dir.
func TestPathIsDirPlusConfigJSON(t *testing.T) {
	override := t.TempDir()
	t.Setenv(config.DirEnv, override)

	got, err := config.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(override, "config.json")
	if got != want {
		t.Fatalf("Path() = %q; want %q", got, want)
	}
}

// TestLoadAbsentFileReturnsZeroValue proves an absent config file means "use
// every default," not an error — the same contract internal/shell.NewStore
// already has for shell.json.
func TestLoadAbsentFileReturnsZeroValue(t *testing.T) {
	t.Setenv(config.DirEnv, filepath.Join(t.TempDir(), "does-not-exist"))

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != (config.Config{}) {
		t.Fatalf("Load() = %+v; want the zero value", got)
	}
}

// TestLoadParsesCairnPath proves a real config file's cairnPath round-trips
// through Load.
func TestLoadParsesCairnPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	writeConfig(t, dir, `{"cairnPath": "/opt/example/cairn"}`)

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.CairnPath != "/opt/example/cairn" {
		t.Fatalf("Load().CairnPath = %q; want %q", got.CairnPath, "/opt/example/cairn")
	}
}

// TestLoadMalformedJSONIsError proves a config file that exists but cannot be
// parsed is reported, not silently treated as absent — a typo in a
// hand-edited file should not look like "nothing configured."
func TestLoadMalformedJSONIsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	writeConfig(t, dir, `{not valid json`)

	if _, err := config.Load(); err == nil {
		t.Fatal("Load: want an error for malformed JSON, got nil")
	}
}

// TestResolveCairnPathUsesConfiguredPath proves an explicit, existing
// cairnPath wins outright — it is never second-guessed against PATH.
func TestResolveCairnPathUsesConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	cairn := writeFakeExecutable(t, dir, "cairn-elsewhere")
	writeConfig(t, dir, `{"cairnPath": "`+jsonEscape(cairn)+`"}`)

	// A PATH with no cairn on it at all — proves the configured path did
	// not merely happen to agree with a PATH lookup.
	t.Setenv("PATH", t.TempDir())

	got, err := config.ResolveCairnPath()
	if err != nil {
		t.Fatalf("ResolveCairnPath: %v", err)
	}
	if got != cairn {
		t.Fatalf("ResolveCairnPath() = %q; want the configured %q", got, cairn)
	}
}

// TestResolveCairnPathConfiguredPathMissingIsError proves a configured
// cairnPath that does not exist is reported immediately, by name, rather
// than silently falling back to PATH — a stale or mistyped path should not
// resolve to some other cairn a person did not intend.
func TestResolveCairnPathConfiguredPathMissingIsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	missing := filepath.Join(dir, "no-such-cairn")
	writeConfig(t, dir, `{"cairnPath": "`+jsonEscape(missing)+`"}`)

	_, err := config.ResolveCairnPath()
	if err == nil {
		t.Fatal("ResolveCairnPath: want an error for a missing configured path, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("ResolveCairnPath error = %q; want it to name the missing path %q", err, missing)
	}
}

// TestResolveCairnPathFallsBackToPATH proves that with no config file at
// all, ResolveCairnPath behaves exactly like the exec.LookPath("cairn") it
// replaces — a terminal-launched Tachyon with cairn already on PATH needs
// no config file.
func TestResolveCairnPathFallsBackToPATH(t *testing.T) {
	t.Setenv(config.DirEnv, filepath.Join(t.TempDir(), "does-not-exist"))

	binDir := t.TempDir()
	cairn := writeFakeExecutable(t, binDir, "cairn")
	t.Setenv("PATH", binDir)

	got, err := config.ResolveCairnPath()
	if err != nil {
		t.Fatalf("ResolveCairnPath: %v", err)
	}
	if got != cairn {
		t.Fatalf("ResolveCairnPath() = %q; want the PATH-resolved %q", got, cairn)
	}
}

// TestResolveCairnPathNotFoundNamesConfigFile proves the "found nowhere"
// error tells a person exactly which file to create or edit — the whole
// point of this package is that the fix is a file, not a shell export that
// (per the friction this package exists to remove) never reaches a
// launchd-started Tachyon anyway.
func TestResolveCairnPathNotFoundNamesConfigFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv(config.DirEnv, dir)
	t.Setenv("PATH", t.TempDir())

	_, err := config.ResolveCairnPath()
	if err == nil {
		t.Fatal("ResolveCairnPath: want an error when cairn is nowhere to be found, got nil")
	}
	wantPath := filepath.Join(dir, "config.json")
	if !strings.Contains(err.Error(), wantPath) {
		t.Fatalf("ResolveCairnPath error = %q; want it to name %q", err, wantPath)
	}
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing config.json: %v", err)
	}
}

// writeFakeExecutable creates an executable file that exec.LookPath (and a
// plain os.Stat, for the configured-path case) will find, without needing a
// real cairn binary on this machine.
func writeFakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing fake executable %s: %v", path, err)
	}
	return path
}

func jsonEscape(s string) string {
	return strings.ReplaceAll(s, `\`, `\\`)
}
