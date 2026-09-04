package bundle_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/state"
)

// TestDefaultRootStoreUnchanged proves internal/state's convergence did not
// relocate the bundle-root store: DefaultRootStore must still resolve to
// exactly the path this package hardcoded before internal/state existed --
// <os.UserConfigDir()>/tachyon/bundle.json (lowercase "tachyon", predating
// and preserved through this change) -- computed here the old way, by hand,
// and compared against what DefaultRootStore returns today. A silent
// relocation here would point an existing user's editor at nothing, having
// lost their chosen bundle root.
func TestDefaultRootStoreUnchanged(t *testing.T) {
	t.Setenv(state.DirEnv, "")

	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no user config dir on this machine: %v", err)
	}
	want := filepath.Join(dir, "tachyon", "bundle.json")

	got, err := bundle.DefaultRootStore()
	if err != nil {
		t.Fatalf("DefaultRootStore: %v", err)
	}
	if got.Path != want {
		t.Fatalf("DefaultRootStore().Path = %q; want the pre-internal/state path %q", got.Path, want)
	}
}

func TestRootStoreRoundTrip(t *testing.T) {
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "settings", "bundle.json")}

	// First run: nothing saved is not an error.
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load on a fresh store: %v", err)
	}
	if got != "" {
		t.Fatalf("Load on a fresh store = %q; want empty", got)
	}

	// With nothing saved, the caller gets the default bundle.
	def, err := bundle.DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot: %v", err)
	}
	resolved, err := store.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != def {
		t.Fatalf("Resolve with nothing saved = %q; want the default %q", resolved, def)
	}

	// A chosen bundle persists, and comes back absolute.
	chosen := t.TempDir()
	if err := store.Save(chosen); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got != chosen {
		t.Fatalf("Load after Save = %q; want %q", got, chosen)
	}
	resolved, err = store.Resolve()
	if err != nil {
		t.Fatalf("Resolve after Save: %v", err)
	}
	if resolved != chosen {
		t.Fatalf("Resolve after Save = %q; want %q", resolved, chosen)
	}

	// Saving again replaces rather than appends.
	second := t.TempDir()
	if err := store.Save(second); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatalf("Load after the second Save: %v", err)
	}
	if got != second {
		t.Fatalf("Load = %q; want %q", got, second)
	}
}

func TestRootStoreSaveExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save("~/some/bundle"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(home, "some", "bundle"); got != want {
		t.Fatalf("Load = %q; want %q", got, want)
	}
}

// A settings file we cannot read must not silently become "use the default".
// Pointing the editor at a different tree than the user last chose is worse
// than saying so.
func TestRootStoreRejectsMalformedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := bundle.RootStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load of a malformed settings file succeeded; want an error")
	}
	if _, err := store.Resolve(); err == nil {
		t.Fatal("Resolve over a malformed settings file succeeded; want an error")
	}
}

func TestRootStoreNeedsAPath(t *testing.T) {
	var store bundle.RootStore
	if _, err := store.Load(); err == nil {
		t.Error("Load with no path succeeded; want an error")
	}
	if err := store.Save("/tmp"); err == nil {
		t.Error("Save with no path succeeded; want an error")
	}
}

func TestDefaultRootStoreIsUnderTheUserConfigDir(t *testing.T) {
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no user config dir: %v", err)
	}
	store, err := bundle.DefaultRootStore()
	if err != nil {
		t.Fatalf("DefaultRootStore: %v", err)
	}
	if !strings.HasPrefix(store.Path, dir) {
		t.Errorf("DefaultRootStore path %q is not under %q", store.Path, dir)
	}
	if !strings.Contains(store.Path, "tachyon") {
		t.Errorf("DefaultRootStore path %q does not name the app", store.Path)
	}
}

// The default is a default, not a constant the package depends on: it is one
// exported string a caller can ignore entirely.
func TestDefaultRootPathIsTheLiveBundle(t *testing.T) {
	if bundle.DefaultRootPath != "~/dev/projects/agent-setup" {
		t.Fatalf("DefaultRootPath = %q", bundle.DefaultRootPath)
	}
}

// TestChangingBundleRootDoesNotTouchTachyonState is CW-20260904-0019's own
// explicit acceptance criterion: the bundle root is a pointer to content,
// persisted under state.Dir()'s own "tachyon" (lowercase) subdirectory —
// but changing WHAT it points at must never change WHERE Tachyon's own
// settings live, or move state.Root() ("Tachyon", capitalized) or
// state.BootRoot() at all.
//
// TACHYON_STATE_DIR redirects state.Dir() to a temp directory for the
// whole test, so nothing here can touch a real machine's
// ~/Library/Application Support — the same isolation
// TestDefaultRootStoreIsUnderTheUserConfigDir relies on state.Dir() for,
// just pinned rather than left to the real OS lookup.
func TestChangingBundleRootDoesNotTouchTachyonState(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(state.DirEnv, stateDir)

	wantDir, err := state.Dir()
	if err != nil {
		t.Fatalf("state.Dir: %v", err)
	}
	wantRoot, err := state.Root()
	if err != nil {
		t.Fatalf("state.Root: %v", err)
	}
	wantBoot, err := state.BootRoot()
	if err != nil {
		t.Fatalf("state.BootRoot: %v", err)
	}
	if _, err := os.Stat(wantRoot); !os.IsNotExist(err) {
		t.Fatalf("state.Root() %q already exists before the test touched anything; test is not isolated: %v", wantRoot, err)
	}

	store, err := bundle.DefaultRootStore()
	if err != nil {
		t.Fatalf("DefaultRootStore: %v", err)
	}
	wantStorePath := filepath.Join(stateDir, "tachyon", "bundle.json")
	if store.Path != wantStorePath {
		t.Fatalf("DefaultRootStore().Path = %q; want %q", store.Path, wantStorePath)
	}

	// Change the active bundle root — twice, to a couple of different
	// scratch directories, the way a real "pick a folder" / "reset to
	// default" sequence would.
	first := t.TempDir()
	if err := store.Save(first); err != nil {
		t.Fatalf("Save(%s): %v", first, err)
	}
	second := t.TempDir()
	if err := store.Save(second); err != nil {
		t.Fatalf("Save(%s): %v", second, err)
	}
	if got, err := store.Load(); err != nil || got != second {
		t.Fatalf("Load after two Saves = (%q, %v); want (%q, nil)", got, err, second)
	}

	// state.Dir/Root/BootRoot are pure functions of TACHYON_STATE_DIR, so
	// resolving them again is a sanity check, not the real assertion — the
	// real assertion is that Save never created anything on disk under
	// state.Root() (the capitalized "Tachyon" directory shell.json and
	// boot/ live under), only under state.Dir()'s own lowercase "tachyon".
	if gotDir, err := state.Dir(); err != nil || gotDir != wantDir {
		t.Fatalf("state.Dir() after two bundle-root Saves = (%q, %v); want (%q, nil)", gotDir, err, wantDir)
	}
	if gotRoot, err := state.Root(); err != nil || gotRoot != wantRoot {
		t.Fatalf("state.Root() after two bundle-root Saves = (%q, %v); want (%q, nil)", gotRoot, err, wantRoot)
	}
	if gotBoot, err := state.BootRoot(); err != nil || gotBoot != wantBoot {
		t.Fatalf("state.BootRoot() after two bundle-root Saves = (%q, %v); want (%q, nil)", gotBoot, err, wantBoot)
	}

	// The real assertion: exactly one entry was created directly under
	// state.Dir(), and it is named "tachyon" — case-preserved, listed via
	// os.ReadDir rather than checked with os.Stat.
	//
	// os.Stat cannot tell "tachyon" and "Tachyon" (state.Root()'s own
	// name) apart on this machine's filesystem: APFS's default mode is
	// case-insensitive but case-preserving, so once
	// bundle.RootStore.Save has created "tachyon", os.Stat(".../Tachyon")
	// resolves to the very same directory and reports it as existing —
	// which would make a Stat-based "state.Root() must not exist" check
	// fail on every macOS machine with default settings regardless of
	// what Save actually wrote, proving nothing about which name was
	// really created. os.ReadDir's entry names are the literal bytes on
	// disk, unaffected by that lookup-time folding, so listing state.Dir()
	// and checking the one entry's Name() is what actually distinguishes
	// "Save wrote its own lowercase tachyon/" from "Save (somehow) wrote
	// into state.Root()'s Tachyon/" on a case-insensitive volume.
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", stateDir, err)
	}
	if len(entries) != 1 || entries[0].Name() != "tachyon" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("state.Dir() %q has entries %v after two bundle-root Saves; want exactly one, named %q (bundle.RootStore's own directory) — not %q (state.Root()'s own name) or anything else",
			stateDir, names, "tachyon", filepath.Base(wantRoot))
	}

	tachyonEntries, err := os.ReadDir(filepath.Join(stateDir, "tachyon"))
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", filepath.Join(stateDir, "tachyon"), err)
	}
	if len(tachyonEntries) != 1 || tachyonEntries[0].Name() != "bundle.json" {
		names := make([]string, len(tachyonEntries))
		for i, e := range tachyonEntries {
			names[i] = e.Name()
		}
		t.Fatalf("state.Dir()/tachyon has entries %v after two bundle-root Saves; want exactly one, %q — nothing named %q (state.BootRoot()'s own \"boot\" name) or anything else belonging to Tachyon's own state",
			names, "bundle.json", filepath.Base(wantBoot))
	}
}
