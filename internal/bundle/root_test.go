package bundle_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

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
