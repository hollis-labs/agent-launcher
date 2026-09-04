package manager_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/manager"
)

// newSparseFixture builds a bundle with exactly one profile and nothing
// else — no templates/roles, no templates, no skills, no hooks, no
// bindings directory at all. This fixture generalizes that absence to
// every kind so a regression in any one of them is caught here rather than
// only in whichever kind happens to be empty on a given day. (It no longer
// claims to describe the live ~/dev/projects/agent-setup bundle for
// bindings specifically — that bundle's own bindings storage has changed
// shape since this comment was first written; this fixture's job was
// always the synthetic zero-member case, not a standing claim about what
// the live bundle currently looks like.)
//
// newFixture in manager_test.go always populates every kind, including
// bindings/ — which is exactly why it did not catch the bug this file
// pins: a bundle where every group has at least one member never exercises
// the zero-member path at all.
func newSparseFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "profiles", "only.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\nid: only\nname: Only\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

// TestTreeNodesAreNeverNullOnTheWire is the direct regression test for the
// bug an independent review caught by actually running the app against the
// live bundle: Tree() built Group.Nodes with a bare `var`/append, which
// leaves it Go-nil when a kind has zero members. encoding/json marshals a
// nil slice as `null`; Manager.jsx's TreeGroup calls .length and .map() on
// group.nodes with no guard at the time, and a null there threw during
// render, which — with no error boundary — unmounted the entire React tree
// and left the manager window permanently blank.
//
// This test inspects the raw JSON bytes Tree() would put on the wire, not
// just the Go value, because the Go value alone would not have caught the
// original bug: a nil []Node and a non-nil empty []Node are both perfectly
// usable from Go and both have len() == 0. The failure only exists at the
// JSON boundary, so the test has to look at the JSON boundary.
func TestTreeNodesAreNeverNullOnTheWire(t *testing.T) {
	svc := newService(t, newSparseFixture(t))
	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	// Five of six kinds have zero members in this fixture (only Profile has
	// one) — every one of them must still marshal its Nodes as [], not null.
	wire, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal(Tree): %v", err)
	}
	if strings.Contains(string(wire), `"nodes":null`) {
		t.Fatalf("Tree JSON contains a null nodes array — this is exactly what breaks Manager.jsx's TreeGroup:\n%s", wire)
	}

	emptyKinds := 0
	for _, g := range tr.Groups {
		if g.Nodes == nil {
			t.Errorf("group %q: Nodes is nil in the Go value (want a non-nil, possibly empty, slice)", g.Kind)
		}
		if g.Count == 0 {
			emptyKinds++
		}
	}
	if emptyKinds < 5 {
		t.Fatalf("fixture is supposed to leave 5 of 6 kinds empty; got only %d — the fixture itself is not exercising the bug", emptyKinds)
	}

	// And round-tripping the wire bytes back into a fresh Go value must
	// produce non-nil slices too — this is what a JSON `[]` decodes to,
	// confirming the wire actually carried an array and not an absent field.
	var decoded manager.Tree
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, g := range decoded.Groups {
		if g.Nodes == nil {
			t.Errorf("group %q: Nodes came back nil after a JSON round trip", g.Kind)
		}
	}
}

// TestTreeNodesAreNeverNullOnTheWireEvenWithNoBundleDirectoriesAtAll pins the
// most extreme version of the same shape: a bundle root that exists but has
// none of the six artifact directories at all (every kind absent, not just
// under-populated). bundle.Bundle documents this as "enumerates as empty,
// not as an error" — Tree() must carry that guarantee through to the wire
// for every group, all six at once.
func TestTreeNodesAreNeverNullOnTheWireEvenWithNoBundleDirectoriesAtAll(t *testing.T) {
	root := t.TempDir() // exists, but nothing under it
	svc := newService(t, root)

	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	wire, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal(Tree): %v", err)
	}
	if strings.Contains(string(wire), `"nodes":null`) {
		t.Fatalf("Tree JSON contains a null nodes array for a completely empty bundle:\n%s", wire)
	}
	if len(tr.Groups) != len(bundle.Kinds()) {
		t.Fatalf("Tree returned %d groups for an empty bundle; want %d (one per kind, all empty)", len(tr.Groups), len(bundle.Kinds()))
	}
	for _, g := range tr.Groups {
		if g.Count != 0 {
			t.Errorf("group %q: Count = %d in a bundle with none of its directories; want 0", g.Kind, g.Count)
		}
		if g.Nodes == nil {
			t.Errorf("group %q: Nodes is nil", g.Kind)
		}
	}
}

// TestContentBytesIsNeverNullOnTheWire is the same class of bug, checked at
// the other aggregation point this package has: a zero-byte artifact's
// Content.Bytes must marshal as "" (empty base64), never as null — the same
// nil-vs-empty distinction, at a different field, per the review's
// instruction to check every place this package aggregates rather than only
// the one field a reviewer happened to find.
func TestContentBytesIsNeverNullOnTheWire(t *testing.T) {
	root := newSparseFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles", "only.md"), []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile (truncate to zero bytes): %v", err)
	}
	svc := newService(t, root)

	content, err := svc.Open(string(bundle.KindProfile), "only")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	wire, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("json.Marshal(Content): %v", err)
	}
	if strings.Contains(string(wire), `"bytes":null`) {
		t.Fatalf("Content JSON has bytes:null for a zero-byte file; want \"bytes\":\"\":\n%s", wire)
	}
}
