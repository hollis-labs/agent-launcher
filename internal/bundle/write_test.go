package bundle_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// TestWriteRoundTripsAwkwardBytesUnchanged is the mirror of
// TestReadReturnsBytesUnchanged in bytes_test.go: every hazard byte sequence
// that must survive a Read must also survive a Write of the exact same bytes
// — an "open and save without editing" cycle must be a no-op on disk.
func TestWriteRoundTripsAwkwardBytesUnchanged(t *testing.T) {
	root := t.TempDir()
	files := map[bundle.Ref]string{
		{Kind: bundle.KindProfile, ID: "awkward"}:      filepath.Join("profiles", "awkward.md"),
		{Kind: bundle.KindRoleProse, ID: "awkward"}:    filepath.Join("templates", "roles", "awkward.md"),
		{Kind: bundle.KindTemplate, ID: "awkward"}:     filepath.Join("templates", "awkward.md"),
		{Kind: bundle.KindSkill, ID: "awkward"}:        filepath.Join("skills", "awkward", "SKILL.md"),
		{Kind: bundle.KindHook, ID: "awkward"}:         filepath.Join("hooks", "awkward.sh"),
		{Kind: bundle.KindBinding, ID: "awkward.yaml"}: filepath.Join("bindings", "awkward.yaml"),
	}
	for _, rel := range files {
		path := filepath.Join(root, rel)
		mkdirAll(t, filepath.Dir(path))
		if err := os.WriteFile(path, []byte(awkwardBytes), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for ref := range files {
		// Open (Read), save without editing (Write the same bytes back), and
		// confirm the file on disk is byte-identical to what was there before
		// — this is the acceptance test's "save without editing" cycle.
		got, err := b.Read(ref)
		if err != nil {
			t.Fatalf("Read(%+v): %v", ref, err)
		}
		if err := b.Write(ref, got); err != nil {
			t.Fatalf("Write(%+v): %v", ref, err)
		}
		path, err := b.Resolve(ref)
		if err != nil {
			t.Fatalf("Resolve(%+v): %v", ref, err)
		}
		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		if !bytes.Equal(onDisk, []byte(awkwardBytes)) {
			t.Errorf("after Write(%+v) the file on disk changed\n got: %q\nwant: %q", ref, onDisk, awkwardBytes)
		}
		// And Read again returns the same bytes it was handed.
		reread, err := b.Read(ref)
		if err != nil {
			t.Fatalf("re-Read(%+v): %v", ref, err)
		}
		if !bytes.Equal(reread, []byte(awkwardBytes)) {
			t.Errorf("re-Read(%+v) = %q; want %q", ref, reread, awkwardBytes)
		}
	}
}

// TestWriteReplacesContent is the ordinary case: the user typed something
// different, and it lands on disk verbatim.
func TestWriteReplacesContent(t *testing.T) {
	root, b := tempBundle(t)
	ref := bundle.Ref{Kind: bundle.KindProfile, ID: "base"}

	newContent := "---\nid: base\nname: Renamed\n---\n\nNew body.\n"
	if err := b.Write(ref, []byte(newContent)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := b.Read(ref)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != newContent {
		t.Fatalf("Read after Write = %q; want %q", got, newContent)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "profiles", "base.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(onDisk) != newContent {
		t.Fatalf("file on disk = %q; want %q", onDisk, newContent)
	}
}

// TestWriteRejectsAnUnknownRef mirrors Resolve: Write edits an artifact that
// already exists, it does not invent one by name.
func TestWriteRejectsAnUnknownRef(t *testing.T) {
	_, b := tempBundle(t)
	ref := bundle.Ref{Kind: bundle.KindProfile, ID: "does-not-exist"}
	err := b.Write(ref, []byte("anything"))
	if err == nil {
		t.Fatal("Write of an unknown ref succeeded; want an error")
	}
	if !errors.Is(err, bundle.ErrNotFound) {
		t.Fatalf("Write of an unknown ref error = %v; want ErrNotFound", err)
	}
}

// TestWriteLeavesNoTempFileBehind guards the atomic-write implementation: a
// successful Write must not leave a .tachyon-*.tmp sibling in the artifact's
// directory.
func TestWriteLeavesNoTempFileBehind(t *testing.T) {
	root, b := tempBundle(t)
	ref := bundle.Ref{Kind: bundle.KindProfile, ID: "base"}
	if err := b.Write(ref, []byte("---\nid: base\n---\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "base.md" {
			t.Errorf("unexpected leftover entry in profiles/: %s", e.Name())
		}
	}
}

// TestWritePreservesFileMode confirms Write only ever touches content: an
// unusual permission on the original file survives a Write.
func TestWritePreservesFileMode(t *testing.T) {
	root, b := tempBundle(t)
	path := filepath.Join(root, "profiles", "base.md")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	ref := bundle.Ref{Kind: bundle.KindProfile, ID: "base"}
	if err := b.Write(ref, []byte("---\nid: base\n---\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode after Write = %v; want 0640", info.Mode().Perm())
	}
}

// TestWriteIsAtomicAgainstAConcurrentReader confirms a reader that opened the
// file before a Write started still sees the old content through its own
// handle — the rename replaces the directory entry, it does not truncate the
// inode a concurrent reader holds open.
func TestWriteIsAtomicAgainstAConcurrentReader(t *testing.T) {
	root, b := tempBundle(t)
	path := filepath.Join(root, "profiles", "base.md")

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	ref := bundle.Ref{Kind: bundle.KindProfile, ID: "base"}
	if err := b.Write(ref, []byte("---\nid: base\nname: Changed\n---\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	held, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll on the pre-opened handle: %v", err)
	}
	if !bytes.Equal(held, before) {
		t.Fatalf("the pre-opened handle's content changed under Write: got %q, want the original %q", held, before)
	}
}
