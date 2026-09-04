package testbundle_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/testbundle"
)

// TestResolve_AbsentBundleRootSkips is the "no bundle on this machine"
// half of the contract: a path that was never created at all must skip,
// not fail.
func TestResolve_AbsentBundleRootSkips(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")

	bindings, skip, err := testbundle.Resolve(root)
	if !skip {
		t.Errorf("skip = false; want true for a bundle root that was never created")
	}
	if err != nil {
		t.Errorf("err = %v; want nil for an absent bundle root", err)
	}
	if bindings != nil {
		t.Errorf("bindings = %+v; want nil", bindings)
	}
}

// TestResolve_PresentBundleRootWithNoBindingsDirFails is this task's own
// positive control: a bundle directory that exists but has no bindings/
// subdirectory at all — exactly what a bundle root pointed at the wrong
// place, or never set up by Cairn, looks like — must FAIL, not skip.
func TestResolve_PresentBundleRootWithNoBindingsDirFails(t *testing.T) {
	root := t.TempDir() // exists; nothing has ever been written into it

	bindings, skip, err := testbundle.Resolve(root)
	if skip {
		t.Errorf("skip = true; want false — the bundle root exists")
	}
	if err == nil {
		t.Fatal("err = nil; want a non-nil error for a bundle present but with no bindings/ directory")
	}
	if bindings != nil {
		t.Errorf("bindings = %+v; want nil", bindings)
	}
	t.Logf("got the expected error: %v", err)
}

// TestResolve_BindingsDirIsActuallyAFileFails covers one of T24's own
// adversarial shapes, re-tested here against the new bindings/-directory
// format: something occupies "bindings" but it is a plain file, not a
// directory. internal/binding must treat this as unreadable, and Resolve
// must propagate that as a failure, not a skip.
func TestResolve_BindingsDirIsActuallyAFileFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bindings"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("writing a file where bindings/ should be a directory: %v", err)
	}

	bindings, skip, err := testbundle.Resolve(root)
	if skip {
		t.Errorf("skip = true; want false — the bundle root exists")
	}
	if err == nil {
		t.Fatal("err = nil; want a non-nil error when bindings/ is actually a plain file")
	}
	if bindings != nil {
		t.Errorf("bindings = %+v; want nil", bindings)
	}
	t.Logf("got the expected error: %v", err)
}

// TestResolve_PresentBundleRootWithUnparseableBindingFileFails covers the
// task's other worked example directly: bindings/ exists, but one of its
// *.yaml files is not in a shape internal/binding's narrow scanner
// recognizes at all. internal/binding fails the whole List() call rather
// than silently omitting the bad file, and Resolve must propagate that.
func TestResolve_PresentBundleRootWithUnparseableBindingFileFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bindings"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	garbage := "this is not the expected shape at all: {{{\nneither is this line\n"
	if err := os.WriteFile(filepath.Join(root, "bindings", "broken.yaml"), []byte(garbage), 0o644); err != nil {
		t.Fatalf("writing an unparseable binding file: %v", err)
	}

	bindings, skip, err := testbundle.Resolve(root)
	if skip {
		t.Errorf("skip = true; want false — the bundle root exists")
	}
	if err == nil {
		t.Fatal("err = nil; want a non-nil error for an unparseable binding file")
	}
	if bindings != nil {
		t.Errorf("bindings = %+v; want nil", bindings)
	}
	t.Logf("got the expected error: %v", err)
}

// TestResolve_PresentBundleRootWithEmptyBindingsDirSucceeds is the
// negative control for T23's own disambiguation work: a bindings/
// directory that exists and is genuinely empty is neither a skip nor a
// failure — it is the one state where "no bindings yet" is actually true.
func TestResolve_PresentBundleRootWithEmptyBindingsDirSucceeds(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bindings"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	bindings, skip, err := testbundle.Resolve(root)
	if skip {
		t.Errorf("skip = true; want false")
	}
	if err != nil {
		t.Fatalf("err = %v; want nil for a present, empty bindings/ directory", err)
	}
	if len(bindings) != 0 {
		t.Errorf("bindings = %+v; want empty", bindings)
	}
}

// TestResolve_PresentBundleRootWithReadableBindingsSucceeds is the
// negative control: a bundle whose bindings/ directory exists and parses
// cleanly must neither skip nor fail, and must hand back what it read.
func TestResolve_PresentBundleRootWithReadableBindingsSucceeds(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bindings"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "profile: engineer\nscope: /tmp/demo\n"
	if err := os.WriteFile(filepath.Join(root, "bindings", "demo.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing a readable binding file: %v", err)
	}

	bindings, skip, err := testbundle.Resolve(root)
	if skip {
		t.Errorf("skip = true; want false")
	}
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if len(bindings) != 1 || bindings[0].Name != "demo" {
		t.Errorf("bindings = %+v; want exactly one binding named %q", bindings, "demo")
	}
}
