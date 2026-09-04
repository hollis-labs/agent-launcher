package bundle_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/state"
)

// TestDefaultRootStoreUnderStateRoot proves DefaultRootStore resolves to
// state.Root()/bundle.json — the same directory internal/shell's
// preferences file (Root()/shell.json) already lives under.
//
// An earlier version of this package built its own separately-cased
// "tachyon" (lowercase) directory under state.Dir() instead, on the theory
// that it predated this package's convergence onto internal/state and
// should stay put to avoid relocating an existing user's bundle.json. That
// reasoning was found not to hold: the default macOS volume is
// case-insensitive but case-preserving, so "tachyon" and "Tachyon" name the
// same physical directory the instant either exists, and internal/shell's
// Store already creates "Tachyon" (state.Root()) on every app launch before
// a user could ever change the bundle root — so bundle.json was already
// landing inside state.Root() in practice, just under a name that read as
// though it were an isolated sibling. This test pins the corrected,
// intentional behavior: one directory, one name, [state.Root]'s.
func TestDefaultRootStoreUnderStateRoot(t *testing.T) {
	t.Setenv(state.DirEnv, "")

	root, err := state.Root()
	if err != nil {
		t.Skipf("no user config dir on this machine: %v", err)
	}
	want := filepath.Join(root, "bundle.json")

	got, err := bundle.DefaultRootStore()
	if err != nil {
		t.Fatalf("DefaultRootStore: %v", err)
	}
	if got.Path != want {
		t.Fatalf("DefaultRootStore().Path = %q; want %q (state.Root()/bundle.json)", got.Path, want)
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
	if !strings.Contains(store.Path, "Tachyon") {
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
// but changing WHAT it points at must never disturb Tachyon's own state —
// state.Root() ("Tachyon") and, within it, state.BootRoot() ("boot") and
// internal/shell's preferences file (Root()/shell.json).
//
// This reproduces the real-world ordering that actually matters: on a real
// machine, internal/shell.Store has already created state.Root() and
// written shell.json into it — on the very first app launch, before a user
// could ever reach the bundle-root UI this task adds. A version of this
// test that calls bundle.DefaultRootStore()/Save() against an empty
// state.Root() (as an earlier version of this test did) never exercises
// that collision and would pass even if bundle.json were, say, accidentally
// written into a *different* directory than shell.json — it only proves
// Save doesn't create extra top-level siblings, not that it shares the
// right one. So: create state.Root() and a shell.json inside it FIRST,
// exactly as a real launch would, then change the bundle root twice and
// assert shell.json survives untouched, bundle.json lands beside it (both
// now live under state.Root(), by design — see DefaultRootStore's doc), and
// state.BootRoot() is never created as a side effect of any of this.
//
// TACHYON_STATE_DIR redirects state.Dir() to a temp directory for the whole
// test, so nothing here can touch a real machine's
// ~/Library/Application Support.
func TestChangingBundleRootDoesNotTouchTachyonState(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(state.DirEnv, stateDir)

	wantRoot, err := state.Root()
	if err != nil {
		t.Fatalf("state.Root: %v", err)
	}
	wantBoot, err := state.BootRoot()
	if err != nil {
		t.Fatalf("state.BootRoot: %v", err)
	}

	// Simulate a real app launch: internal/shell.Store creates state.Root()
	// and writes shell.json into it before anything else happens.
	if err := os.MkdirAll(wantRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", wantRoot, err)
	}
	shellJSONPath := filepath.Join(wantRoot, "shell.json")
	const shellJSONContent = `{"hotkey":"ctrl+option+space"}` + "\n"
	if err := os.WriteFile(shellJSONPath, []byte(shellJSONContent), 0o644); err != nil {
		t.Fatalf("seeding shell.json: %v", err)
	}

	store, err := bundle.DefaultRootStore()
	if err != nil {
		t.Fatalf("DefaultRootStore: %v", err)
	}
	wantStorePath := filepath.Join(wantRoot, "bundle.json")
	if store.Path != wantStorePath {
		t.Fatalf("DefaultRootStore().Path = %q; want %q (state.Root()/bundle.json, beside shell.json)", store.Path, wantStorePath)
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
	// resolving them again just confirms nothing about the lookup itself
	// changed — the real assertions are the file-level ones below.
	if gotRoot, err := state.Root(); err != nil || gotRoot != wantRoot {
		t.Fatalf("state.Root() after two bundle-root Saves = (%q, %v); want (%q, nil)", gotRoot, err, wantRoot)
	}
	if gotBoot, err := state.BootRoot(); err != nil || gotBoot != wantBoot {
		t.Fatalf("state.BootRoot() after two bundle-root Saves = (%q, %v); want (%q, nil)", gotBoot, err, wantBoot)
	}

	// shell.json — pre-existing, unrelated content that was already in
	// state.Root() before any bundle-root Save — must survive byte for
	// byte. This is the assertion the earlier, empty-state.Root() version
	// of this test could never make, because it never put anything there
	// to potentially clobber.
	gotShellJSON, err := os.ReadFile(shellJSONPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) after two bundle-root Saves: %v", shellJSONPath, err)
	}
	if string(gotShellJSON) != shellJSONContent {
		t.Fatalf("shell.json content changed by bundle-root Save: got %q, want %q", gotShellJSON, shellJSONContent)
	}

	// state.BootRoot() ("boot") must not have been created as a side
	// effect of saving the bundle root — it stays a distinct subdirectory
	// that only internal/boot ever plants into.
	if _, err := os.Stat(wantBoot); !os.IsNotExist(err) {
		t.Fatalf("state.BootRoot() %q exists after two bundle-root Saves; it should not have been created", wantBoot)
	}

	// The real assertion: state.Root() now holds exactly shell.json (the
	// pre-existing file, untouched) and bundle.json (the one this task's
	// Save calls create) — nothing else, and in particular no second,
	// differently-cased sibling directory. Listed via os.ReadDir, whose
	// entry names are the literal bytes on disk, so this holds regardless
	// of the filesystem's case-folding behavior on lookup.
	entries, err := os.ReadDir(wantRoot)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", wantRoot, err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	sort.Strings(names)
	wantNames := []string{"bundle.json", "shell.json"}
	if len(names) != len(wantNames) || names[0] != wantNames[0] || names[1] != wantNames[1] {
		t.Fatalf("state.Root() %q has entries %v after two bundle-root Saves; want exactly %v", wantRoot, names, wantNames)
	}

	// state.Dir() itself holds exactly one entry: state.Root() ("Tachyon")
	// — no separate, differently-cased top-level sibling was created.
	dirEntries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", stateDir, err)
	}
	if len(dirEntries) != 1 || dirEntries[0].Name() != filepath.Base(wantRoot) {
		dirNames := make([]string, len(dirEntries))
		for i, e := range dirEntries {
			dirNames[i] = e.Name()
		}
		t.Fatalf("state.Dir() %q has entries %v after two bundle-root Saves; want exactly one, named %q",
			stateDir, dirNames, filepath.Base(wantRoot))
	}
}
