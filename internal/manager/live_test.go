package manager_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/manager"
)

// TestLiveBundleOpenSaveRoundTrip is the acceptance test for CW-20260903-0009:
// open every artifact kind in the live bundle, save without editing, and
// leave the tree byte-for-byte unchanged — the same property T04's
// TestLiveBundleCensus proves for Read, extended through Save.
//
// It goes through the exact path the frontend does: Open returns Content
// whose Bytes field is []byte; that is round-tripped through encoding/json
// (the same encoder Wails uses for a call result and for a call argument,
// confirmed in TestOpenSaveRoundTripThroughJSON) before being handed back to
// Save. So this is not merely "Read then Write", it is "what a save-without
// editing click actually does, including the JSON/base64 hop".
//
// Skipped unless TACHYON_LIVE_BUNDLE is set — the suite must not depend on,
// or ever touch by default, ~/dev/projects/agent-setup, which is a git repo
// Chrispian edits for real. See plan CW-20260518-0061 task CW-20260903-0009's
// explicit safety protocol: this is meant to be run rarely, once confidence
// is already established against the fixture in manager_test.go, and its
// result is meant to be checked immediately afterward with `git status` /
// `git diff` in that repo by the person running it — this test's own
// before/after fingerprint is a second, independent check, not a
// replacement for that.
//
//	TACHYON_LIVE_BUNDLE=1 go test ./internal/manager/ -run TestLiveBundleOpenSaveRoundTrip -v
func TestLiveBundleOpenSaveRoundTrip(t *testing.T) {
	if os.Getenv("TACHYON_LIVE_BUNDLE") == "" {
		t.Skip("set TACHYON_LIVE_BUNDLE=1 to round-trip the real bundle; the suite reads a fixture by default")
	}
	root := os.Getenv("TACHYON_BUNDLE_ROOT")
	if root == "" {
		var err error
		if root, err = bundle.DefaultRoot(); err != nil {
			t.Fatalf("DefaultRoot: %v", err)
		}
	}
	t.Logf("bundle root: %s", root)

	before := hashLiveTree(t, root)

	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save(root); err != nil {
		t.Fatalf("RootStore.Save: %v", err)
	}
	svc := manager.New(store)

	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tr.Root != mustAbs(t, root) {
		t.Fatalf("Tree().Root = %q; want %q", tr.Root, mustAbs(t, root))
	}

	total := 0
	for _, g := range tr.Groups {
		t.Logf("%-10s %2d", g.Kind, g.Count)
		for _, n := range g.Nodes {
			total++
			roundTripOne(t, svc, n)
		}
	}
	t.Logf("opened and saved %d artifacts across %d kinds", total, len(tr.Groups))

	if after := hashLiveTree(t, root); after != before {
		t.Fatalf("the live bundle changed across an open+save-unedited pass:\nbefore %s\nafter  %s\n"+
			"If this fails, DO NOT leave it: cd %s && git status --porcelain && git diff, then "+
			"`git checkout -- <file>` to restore anything that moved, and treat this as a real bug.",
			before, after, root)
	}
	t.Logf("tree fingerprint unchanged: %s", before)
}

// roundTripOne opens one artifact, sends its bytes through encoding/json (the
// same marshaling Wails' call bridge uses) and saves the result back
// unedited, then confirms both Open and Save reported the file's actual
// on-disk bytes at every step.
func roundTripOne(t *testing.T, svc *manager.Service, n manager.Node) {
	t.Helper()

	opened, err := svc.Open(string(n.Kind), n.ID)
	if err != nil {
		t.Fatalf("Open(%s %s): %v", n.Kind, n.ID, err)
	}
	onDiskBefore, err := os.ReadFile(opened.Path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", opened.Path, err)
	}
	if string(opened.Bytes) != string(onDiskBefore) {
		t.Fatalf("Open(%s %s) bytes do not match the file on disk", n.Kind, n.ID)
	}

	wire, err := json.Marshal(opened)
	if err != nil {
		t.Fatalf("json.Marshal(Content for %s %s): %v", n.Kind, n.ID, err)
	}
	var overWire manager.Content
	if err := json.Unmarshal(wire, &overWire); err != nil {
		t.Fatalf("json.Unmarshal(Content for %s %s): %v", n.Kind, n.ID, err)
	}
	if string(overWire.Bytes) != string(onDiskBefore) {
		t.Fatalf("%s %s: content changed crossing JSON", n.Kind, n.ID)
	}

	saved, err := svc.Save(string(n.Kind), n.ID, overWire.Bytes)
	if err != nil {
		t.Fatalf("Save(%s %s): %v", n.Kind, n.ID, err)
	}
	onDiskAfter, err := os.ReadFile(saved.Path)
	if err != nil {
		t.Fatalf("ReadFile(%s) after Save: %v", saved.Path, err)
	}
	if string(onDiskAfter) != string(onDiskBefore) {
		t.Fatalf("%s %s: file changed after an unedited open+save\nbefore: %q\nafter:  %q",
			n.Kind, n.ID, onDiskBefore, onDiskAfter)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := bundle.ExpandRoot(p)
	if err != nil {
		t.Fatalf("ExpandRoot(%s): %v", p, err)
	}
	return abs
}

// TestLiveBundleTreeNeverMarshalsNullNodes is the direct check, against the
// actual live bundle, for the bug an independent review caught by running
// the built app: Tree() used to leave Group.Nodes Go-nil for a kind with
// zero members, which encoding/json marshals as `null`, which
// Manager.jsx's TreeGroup then crashed on (.length / .map() with no
// guard), unmounting the whole window with no error boundary to catch it.
//
// It is read-only — Tree() never writes — so unlike
// TestLiveBundleOpenSaveRoundTrip this is safe to run directly against
// ~/dev/projects/agent-setup with no snapshot and no restore step.
//
// KindBinding's group was the zero-member case the bug needed, on the real
// data, when this test was written — the live bundle's own bindings
// storage has since changed shape, so this may no longer be the kind that
// actually exercises that path today. Nothing here depends on which kind
// supplies it: this test only needs Tree() to never marshal a Go-nil Nodes
// slice as null, for whichever kind is empty against the bundle it runs
// against.
//
//	TACHYON_LIVE_BUNDLE=1 go test ./internal/manager/ -run TestLiveBundleTreeNeverMarshalsNullNodes -v
func TestLiveBundleTreeNeverMarshalsNullNodes(t *testing.T) {
	if os.Getenv("TACHYON_LIVE_BUNDLE") == "" {
		t.Skip("set TACHYON_LIVE_BUNDLE=1 to read the real bundle; the suite reads a fixture by default")
	}
	root := os.Getenv("TACHYON_BUNDLE_ROOT")
	if root == "" {
		var err error
		if root, err = bundle.DefaultRoot(); err != nil {
			t.Fatalf("DefaultRoot: %v", err)
		}
	}
	t.Logf("bundle root: %s", root)

	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save(root); err != nil {
		t.Fatalf("RootStore.Save: %v", err)
	}
	svc := manager.New(store)

	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	wire, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal(Tree): %v", err)
	}
	if strings.Contains(string(wire), `"nodes":null`) {
		t.Fatalf("Tree JSON against the live bundle contains a null nodes array — this is the exact bug the review reported:\n%s", wire)
	}

	sawEmptyGroup := false
	for _, g := range tr.Groups {
		t.Logf("%-10s count=%d nodes-is-nil=%v", g.Kind, g.Count, g.Nodes == nil)
		if g.Nodes == nil {
			t.Errorf("group %q: Nodes is nil in the Go value", g.Kind)
		}
		if g.Count == 0 {
			sawEmptyGroup = true
		}
	}
	if !sawEmptyGroup {
		t.Log("note: every kind had at least one member in this run — the zero-member case (bindings, as of this writing) was not exercised; that does not invalidate the fix, but this run did not reproduce the exact shape the review found")
	}
}

// hashLiveTree fingerprints every file's path and bytes under root, skipping
// dot-directories so .git is not walked. Mirrors bundle's live_test.go.
func hashLiveTree(t *testing.T, root string) string {
	t.Helper()
	type entry struct {
		rel string
		sum [32]byte
	}
	var entries []entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{rel: filepath.ToSlash(rel), sum: sha256.Sum256(data)})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.rel))
		h.Write([]byte{0})
		h.Write(e.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}
