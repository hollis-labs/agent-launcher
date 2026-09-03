package shell

import (
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// TestBundleRootStoreOverride confirms an explicit override is used verbatim
// — the case a test of the whole shell would take, if constructing one did
// not require a real windowing system (see the package doc's note on why
// there is no shell.New test).
func TestBundleRootStoreOverride(t *testing.T) {
	const path = "/tmp/some-tachyon-test/bundle.json"
	store, err := bundleRootStore(path)
	if err != nil {
		t.Fatalf("bundleRootStore(%q): %v", path, err)
	}
	if store.Path != path {
		t.Fatalf("bundleRootStore(%q).Path = %q; want %q", path, store.Path, path)
	}
}

// TestBundleRootStoreDefault confirms an empty override falls back to
// bundle.DefaultRootStore rather than some ad hoc path of its own — one
// definition of "where the active bundle root lives" is the point.
func TestBundleRootStoreDefault(t *testing.T) {
	store, err := bundleRootStore("")
	if err != nil {
		t.Fatalf("bundleRootStore(\"\"): %v", err)
	}
	want, err := bundle.DefaultRootStore()
	if err != nil {
		t.Fatalf("bundle.DefaultRootStore: %v", err)
	}
	if store.Path != want.Path {
		t.Fatalf("bundleRootStore(\"\").Path = %q; want %q", store.Path, want.Path)
	}
}
