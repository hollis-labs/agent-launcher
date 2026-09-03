package manager_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/skeleton"
)

// TestNewArtifactKindsMatchesSkeleton pins the wiring: this service's
// NewArtifactKinds is a direct pass-through to skeleton.SupportedKinds(),
// nothing more, so the frontend's picker learns about a future binding
// scaffold (CW-20260903-0011) with no change to this package.
func TestNewArtifactKindsMatchesSkeleton(t *testing.T) {
	svc := newService(t, newFixture(t))
	got := svc.NewArtifactKinds()
	want := skeleton.SupportedKinds()
	if len(got) != len(want) {
		t.Fatalf("NewArtifactKinds() = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NewArtifactKinds() = %v; want %v", got, want)
		}
	}
	for _, k := range got {
		if k == bundle.KindBinding {
			t.Error("NewArtifactKinds() contains KindBinding -- must stay absent until CW-20260903-0011 lands")
		}
	}
}

// TestNewArtifactCreatesEachSupportedKindAndAppearsInTree exercises the
// method the frontend actually calls: create, then confirm the result shows
// up in the next Tree() read, with no restart and no cache to invalidate --
// the same "no restart" property T04/T05 already prove for reads, extended
// through creation.
func TestNewArtifactCreatesEachSupportedKindAndAppearsInTree(t *testing.T) {
	root := newFixture(t)
	svc := newService(t, root)

	cases := []struct {
		kind bundle.Kind
		id   string
	}{
		{bundle.KindProfile, "brandnew"},
		{bundle.KindRoleProse, "brandnew"},
		{bundle.KindTemplate, "brandnew"},
		{bundle.KindSkill, "brandnew"},
	}
	for _, tc := range cases {
		content, err := svc.NewArtifact(string(tc.kind), tc.id, "Brand New", "A fresh description.")
		if err != nil {
			t.Fatalf("NewArtifact(%s, %s): %v", tc.kind, tc.id, err)
		}
		if content.Kind != tc.kind || content.ID != tc.id {
			t.Fatalf("NewArtifact(%s, %s) returned %+v", tc.kind, tc.id, content)
		}
		if len(content.Bytes) == 0 {
			t.Errorf("NewArtifact(%s, %s) returned empty Bytes", tc.kind, tc.id)
		}

		tr, err := svc.Tree()
		if err != nil {
			t.Fatalf("Tree: %v", err)
		}
		found := false
		for _, g := range tr.Groups {
			if g.Kind != tc.kind {
				continue
			}
			for _, n := range g.Nodes {
				if n.ID == tc.id {
					found = true
					if n.RelPath != content.RelPath {
						t.Errorf("tree node RelPath = %q; want %q (from NewArtifact's own result)", n.RelPath, content.RelPath)
					}
				}
			}
		}
		if !found {
			t.Errorf("%s %s created by NewArtifact does not appear in the next Tree()", tc.kind, tc.id)
		}
	}
}

// TestNewArtifactRejectsBinding proves the manager layer does not quietly
// paper over skeleton's refusal -- a caller that somehow requests a binding
// (the frontend's picker should never offer it, but nothing stops a direct
// call) gets the same explicit, seam-naming error skeleton.New does, not a
// generic failure.
func TestNewArtifactRejectsBinding(t *testing.T) {
	svc := newService(t, newFixture(t))
	_, err := svc.NewArtifact(string(bundle.KindBinding), "x", "", "")
	if !errors.Is(err, skeleton.ErrKindNotSupported) {
		t.Fatalf("NewArtifact(binding, ...) error = %v; want skeleton.ErrKindNotSupported", err)
	}
}

// TestNewArtifactRejectsADuplicateID confirms the manager surfaces
// skeleton's create-only guarantee: NewArtifact never overwrites.
func TestNewArtifactRejectsADuplicateID(t *testing.T) {
	svc := newService(t, newFixture(t))
	if _, err := svc.NewArtifact(string(bundle.KindTemplate), "onlyonce", "", ""); err != nil {
		t.Fatalf("first NewArtifact: %v", err)
	}
	if _, err := svc.NewArtifact(string(bundle.KindTemplate), "onlyonce", "", ""); !errors.Is(err, skeleton.ErrAlreadyExists) {
		t.Fatalf("second NewArtifact error = %v; want skeleton.ErrAlreadyExists", err)
	}
}

// TestFreshlyCreatedArtifactRoundTripsThroughRealOpenSave is
// CW-20260903-0010's last acceptance criterion, proved through the actual
// T05 editor code path rather than through skeleton's own writer: create an
// artifact, Open it (the same call the frontend's tree click makes), Save
// its bytes back completely unedited (the same call the frontend's Save
// button makes), and confirm the file on disk did not change by one byte.
//
// This exercises every kind NewArtifact supports, because the four
// scaffolds are different enough (YAML frontmatter with a spec block, bare
// prose, an HTML-comment-only template, a skill's two-field frontmatter)
// that a byte-preservation bug in one shape would not necessarily show up in
// another.
func TestFreshlyCreatedArtifactRoundTripsThroughRealOpenSave(t *testing.T) {
	root := newFixture(t)
	svc := newService(t, root)

	for _, kind := range skeleton.SupportedKinds() {
		id := "roundtrip"
		created, err := svc.NewArtifact(string(kind), id, "Round Trip", "Exercises the byte-preservation guarantee.")
		if err != nil {
			t.Fatalf("NewArtifact(%s, %s): %v", kind, id, err)
		}

		onDiskBefore, err := os.ReadFile(created.Path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", created.Path, err)
		}
		if !bytes.Equal(created.Bytes, onDiskBefore) {
			t.Fatalf("%s: NewArtifact's returned Bytes do not match what landed on disk", kind)
		}

		// Open: the real editor-open path, not a re-read of the file.
		opened, err := svc.Open(string(kind), id)
		if err != nil {
			t.Fatalf("Open(%s, %s) after creation: %v", kind, id, err)
		}
		if !bytes.Equal(opened.Bytes, onDiskBefore) {
			t.Fatalf("%s: Open after creation returned different bytes than NewArtifact wrote", kind)
		}

		// Save without editing: draftText === originalText in the frontend's
		// terms, so this is exactly opened.Bytes going back in unmodified --
		// the acceptance criterion's "re-saving a freshly created file
		// changes no bytes", proved through Save, not through skeleton.New
		// being idempotent with itself.
		saved, err := svc.Save(string(kind), id, opened.Bytes)
		if err != nil {
			t.Fatalf("Save(%s, %s) unedited: %v", kind, id, err)
		}
		if !bytes.Equal(saved.Bytes, onDiskBefore) {
			t.Fatalf("%s: Save result bytes differ from the original\n before: %q\n after:  %q", kind, onDiskBefore, saved.Bytes)
		}

		onDiskAfter, err := os.ReadFile(created.Path)
		if err != nil {
			t.Fatalf("ReadFile(%s) after save: %v", created.Path, err)
		}
		if !bytes.Equal(onDiskAfter, onDiskBefore) {
			t.Fatalf("%s: file on disk changed after an unedited create+open+save\n before: %q\n after:  %q",
				kind, onDiskBefore, onDiskAfter)
		}
	}
}

// TestNewArtifactRejectsAnInvalidID confirms invalid ids are refused at the
// manager boundary too, not only when calling skeleton directly, and that
// nothing escapes the bundle root in the attempt.
func TestNewArtifactRejectsAnInvalidID(t *testing.T) {
	root := newFixture(t)
	svc := newService(t, root)
	if _, err := svc.NewArtifact(string(bundle.KindProfile), "../escape", "", ""); !errors.Is(err, skeleton.ErrInvalidID) {
		t.Fatalf("NewArtifact with a traversal id error = %v; want skeleton.ErrInvalidID", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.md")); err == nil {
		t.Fatal("a file landed outside the bundle root")
	}
}
