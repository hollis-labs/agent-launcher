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

// TestResolve_PresentBundleRootWithNoBindingsFileFails is this task's own
// positive control: a bundle directory that exists, in a shape the reader
// does not understand (no bindings file at all — exactly what
// ~/dev/projects/agent-setup looks like today, now that its bindings live
// under a bindings/ directory this build of Tachyon never reads), must
// FAIL, not skip. This is the empirical proof the task asked for — it does
// not require sabotaging the real agent-setup bundle to demonstrate it.
func TestResolve_PresentBundleRootWithNoBindingsFileFails(t *testing.T) {
	root := t.TempDir() // exists; nothing has ever been written into it

	bindings, skip, err := testbundle.Resolve(root)
	if skip {
		t.Errorf("skip = true; want false — the bundle root exists")
	}
	if err == nil {
		t.Fatal("err = nil; want a non-nil error for a bundle present but unreadable")
	}
	if bindings != nil {
		t.Errorf("bindings = %+v; want nil", bindings)
	}
	t.Logf("got the expected error: %v", err)
}

// TestResolve_PresentBundleRootWithUnparseableBindingsFileFails covers the
// task's other worked example directly: a bindings file that exists but is
// not in a shape internal/binding's narrow scanner recognizes at all.
// internal/binding treats every line it cannot parse as "leave it alone,"
// so a wholly unrecognizable file still yields zero entries — the same
// failure mode as an absent file, and Resolve must catch both.
func TestResolve_PresentBundleRootWithUnparseableBindingsFileFails(t *testing.T) {
	root := t.TempDir()
	garbage := "this is not the expected shape at all: {{{\nneither is this line\n"
	if err := os.WriteFile(filepath.Join(root, "bindings.yaml"), []byte(garbage), 0o644); err != nil {
		t.Fatalf("writing an unparseable bindings file: %v", err)
	}

	bindings, skip, err := testbundle.Resolve(root)
	if skip {
		t.Errorf("skip = true; want false — the bundle root exists")
	}
	if err == nil {
		t.Fatal("err = nil; want a non-nil error for an unparseable bindings file")
	}
	if bindings != nil {
		t.Errorf("bindings = %+v; want nil", bindings)
	}
	t.Logf("got the expected error: %v", err)
}

// TestResolve_PresentBundleRootWithReadableBindingsSucceeds is the negative
// control: a bundle whose bindings file exists and parses cleanly must
// neither skip nor fail, and must hand back what it read.
func TestResolve_PresentBundleRootWithReadableBindingsSucceeds(t *testing.T) {
	root := t.TempDir()
	content := "bindings:\n  demo: { profile: engineer, scope: /tmp/demo }\n"
	if err := os.WriteFile(filepath.Join(root, "bindings.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing a readable bindings file: %v", err)
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
