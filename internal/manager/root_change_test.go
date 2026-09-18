package manager_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// TestSetRootChangesWhatTreeAndOpenRead is CW-20260904-0019's core
// acceptance test: SetRoot is [bundle.RootStore.Save] bound to the
// frontend, and the whole point of it is that nothing else needs to
// change for the next Tree()/Open() call to read from the new bundle —
// no restart, no cache to invalidate, because [Service.open] resolves the
// root fresh on every call (see the package doc's "nothing here caches").
// This proves that claim empirically rather than by reading the doc
// comment and trusting it.
func TestSetRootChangesWhatTreeAndOpenRead(t *testing.T) {
	svc := newService(t, newFixture(t)) // starts pointed at a fixture with "architect" etc.

	before, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree (before SetRoot): %v", err)
	}
	if countKind(before, bundle.KindProfile) == 0 {
		t.Fatal("fixture bundle has no profiles; test setup is broken")
	}

	// A second, completely different bundle: one profile, a different id.
	second := t.TempDir()
	secondProfile := filepath.Join(second, "profiles", "second-only.md")
	if err := os.MkdirAll(filepath.Dir(secondProfile), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(secondProfile, []byte("---\nid: second-only\nname: Second Only\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := svc.SetRoot(second); err != nil {
		t.Fatalf("SetRoot(%s): %v", second, err)
	}

	// Root() must report the new root immediately.
	gotRoot, err := svc.Root()
	if err != nil {
		t.Fatalf("Root after SetRoot: %v", err)
	}
	if gotRoot != second {
		t.Fatalf("Root() after SetRoot = %q; want %q", gotRoot, second)
	}

	// Tree(), called again with no other change, must read the new bundle:
	// no "second-only" profile before, exactly one now.
	after, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree (after SetRoot): %v", err)
	}
	if after.Root != second {
		t.Fatalf("Tree().Root after SetRoot = %q; want %q", after.Root, second)
	}
	if countKind(after, bundle.KindProfile) != 1 {
		t.Fatalf("Tree() after SetRoot has %d profiles; want exactly 1 (the new bundle's only profile)", countKind(after, bundle.KindProfile))
	}
	foundNew, foundOld := false, false
	for _, g := range after.Groups {
		if g.Kind != bundle.KindProfile {
			continue
		}
		for _, n := range g.Nodes {
			if n.ID == "second-only" {
				foundNew = true
			}
			if n.ID == "architect" {
				foundOld = true
			}
		}
	}
	if !foundNew {
		t.Error("Tree() after SetRoot does not contain the new bundle's profile")
	}
	if foundOld {
		t.Error("Tree() after SetRoot still contains the old bundle's profile — root did not actually change")
	}

	// Open() must resolve against the new bundle too, not just Tree().
	if _, err := svc.Open(string(bundle.KindProfile), "second-only"); err != nil {
		t.Fatalf("Open(second-only) after SetRoot: %v", err)
	}
	if _, err := svc.Open(string(bundle.KindProfile), "architect"); err == nil {
		t.Fatal("Open(architect) after SetRoot succeeded; want an error -- that profile belongs to the old bundle")
	}
}

// TestDefaultRootMatchesBundleDefaultRoot pins Service.DefaultRoot to the
// one Go source of truth for the default, bundle.DefaultRoot, so the
// manager's "reset to default" affordance can never hardcode
// bundle.DefaultRootPath a second time in JavaScript.
func TestDefaultRootMatchesBundleDefaultRoot(t *testing.T) {
	svc := newService(t, newFixture(t))
	want, err := bundle.DefaultRoot()
	if err != nil {
		t.Fatalf("bundle.DefaultRoot: %v", err)
	}
	got, err := svc.DefaultRoot()
	if err != nil {
		t.Fatalf("Service.DefaultRoot: %v", err)
	}
	if got != want {
		t.Fatalf("Service.DefaultRoot() = %q; want %q", got, want)
	}
}

// TestRevertToDefaultRoundTrips is the acceptance criterion's explicit
// "reverting to the default works, including when bundle.json already
// exists" scenario, exercised through the same three calls the frontend
// makes: SetRoot to something else, then SetRoot back to
// Service.DefaultRoot()'s own answer, then a fresh Root() read.
func TestRevertToDefaultRoundTrips(t *testing.T) {
	svc := newService(t, newFixture(t)) // newService already calls Save once

	other := t.TempDir()
	if err := svc.SetRoot(other); err != nil {
		t.Fatalf("SetRoot(other): %v", err)
	}
	if got, err := svc.Root(); err != nil || got != other {
		t.Fatalf("Root() after SetRoot(other) = (%q, %v); want (%q, nil)", got, err, other)
	}

	def, err := svc.DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot: %v", err)
	}
	if err := svc.SetRoot(def); err != nil {
		t.Fatalf("SetRoot(default): %v", err)
	}
	if got, err := svc.Root(); err != nil || got != def {
		t.Fatalf("Root() after reverting to default = (%q, %v); want (%q, nil)", got, err, def)
	}
}

// TestTreeStateOkForARealBundle pins the ordinary case: a bundle with real
// content is "ok", never "unrecognized".
func TestTreeStateOkForARealBundle(t *testing.T) {
	svc := newService(t, newFixture(t))
	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tr.State != "ok" {
		t.Fatalf("Tree().State = %q; want %q", tr.State, "ok")
	}
}

// TestTreeStateOkForAGenuinelyEmptyButRecognizedBundle is the other half of
// the three-state distinction this task adds: a bundle root that has one
// of the five known artifact directories, even empty, is recognized as a
// real (if empty) bundle -- "ok", not "unrecognized" -- because
// bundle.Bundle.HasKnownShape only needs one of them to exist.
func TestTreeStateOkForAGenuinelyEmptyButRecognizedBundle(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	svc := newService(t, root)
	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tr.State != "ok" {
		t.Fatalf("Tree().State for an empty-but-recognized bundle = %q; want %q", tr.State, "ok")
	}
	for _, g := range tr.Groups {
		if g.Count != 0 {
			t.Errorf("group %q: Count = %d; want 0 (nothing was ever added)", g.Kind, g.Count)
		}
	}
}

// TestTreeStateUnrecognizedForANonBundleDirectory is the failure mode this
// task's own record says must not be created: pointing the manager at a
// directory that exists, is readable, and has none of the five artifact
// directories -- a home directory, a Desktop, a typo -- must not read the
// same as a real, empty bundle. nil_slices_test.go's
// TestTreeNodesAreNeverNullOnTheWireEvenWithNoBundleDirectoriesAtAll
// already pins that this exact shape of root must not error and must
// never marshal a null Nodes array; this test adds the State assertion on
// top of that, without changing any of those existing guarantees.
func TestTreeStateUnrecognizedForANonBundleDirectory(t *testing.T) {
	root := t.TempDir() // exists, readable, nothing recognizable under it
	svc := newService(t, root)
	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tr.State != "unrecognized" {
		t.Fatalf("Tree().State for a directory with none of the known bundle directories = %q; want %q", tr.State, "unrecognized")
	}
	// Still well-formed on the wire: no null slices, one group per kind --
	// State is an addition, not a replacement for those guarantees.
	if len(tr.Groups) != len(bundle.Kinds()) {
		t.Fatalf("Tree() with State=unrecognized still returned %d groups; want %d", len(tr.Groups), len(bundle.Kinds()))
	}
	for _, g := range tr.Groups {
		if g.Nodes == nil {
			t.Errorf("group %q: Nodes is nil even in the unrecognized state", g.Kind)
		}
	}
}

// TestTreeStateUnrecognizedClearsOnceARealBundleIsChosen proves State is
// read fresh every call, same as everything else this service reads: a
// bundle root pointed at first a non-bundle directory, then a real
// bundle, reports "unrecognized" and then "ok" with no restart -- the
// same per-call-resolution property TestSetRootChangesWhatTreeAndOpenRead
// proves for Tree's actual content.
func TestTreeStateUnrecognizedClearsOnceARealBundleIsChosen(t *testing.T) {
	svc := newService(t, t.TempDir()) // starts pointed at nothing recognizable

	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree (before): %v", err)
	}
	if tr.State != "unrecognized" {
		t.Fatalf("Tree().State before choosing a real bundle = %q; want %q", tr.State, "unrecognized")
	}

	real := newFixture(t)
	if err := svc.SetRoot(real); err != nil {
		t.Fatalf("SetRoot: %v", err)
	}

	tr, err = svc.Tree()
	if err != nil {
		t.Fatalf("Tree (after): %v", err)
	}
	if tr.State != "ok" {
		t.Fatalf("Tree().State after SetRoot to a real bundle = %q; want %q", tr.State, "ok")
	}
}
