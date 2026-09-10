package bundle_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// awkwardBytes is content chosen to break anything that parses and
// re-serializes: CRLF, a lone CR, a UTF-8 BOM, a NUL, tabs, trailing spaces,
// a run of blank lines, non-ASCII, and no trailing newline at the end.
//
// D5 is "bytes in, bytes out". The read path must hand these back untouched.
const awkwardBytes = "\xEF\xBB\xBF---\r\n" +
	"id: awkward\r\n" +
	"name: Awkward   \t\r\n" +
	"---\r\n" +
	"\r\n" +
	"A line with a tab\there and trailing spaces here.   \n" +
	"\n\n\n" +
	"A lone carriage return follows.\rStill the same line.\n" +
	"Non-ASCII: éü—✓ and a NUL: \x00 after it.\n" +
	"No trailing newline on the last line."

func TestReadReturnsBytesUnchanged(t *testing.T) {
	root := t.TempDir()
	files := map[bundle.Ref]string{
		{Kind: bundle.KindProfile, ID: "awkward"}:         filepath.Join("profiles", "awkward.md"),
		{Kind: bundle.KindTemplate, ID: "awkward"}:        filepath.Join("templates", "awkward.md"),
		{Kind: bundle.KindTemplate, ID: "lenses/awkward"}: filepath.Join("templates", "lenses", "awkward.md"),
		{Kind: bundle.KindPrompt, ID: "awkward"}:          filepath.Join("prompts", "awkward.md"),
		{Kind: bundle.KindSkill, ID: "awkward"}:           filepath.Join("skills", "awkward", "SKILL.md"),
		{Kind: bundle.KindHook, ID: "awkward"}:            filepath.Join("hooks", "awkward.sh"),
	}
	for _, rel := range files {
		path := filepath.Join(root, rel)
		mkdirAll(t, filepath.Dir(path))
		if err := os.WriteFile(path, []byte(awkwardBytes), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	before := hashTree(t, root)

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for ref := range files {
		got, err := b.Read(ref)
		if err != nil {
			t.Fatalf("Read(%+v): %v", ref, err)
		}
		if !bytes.Equal(got, []byte(awkwardBytes)) {
			t.Errorf("Read(%+v) returned %d bytes, want the %d bytes written\n got: %q\nwant: %q",
				ref, len(got), len(awkwardBytes), got, awkwardBytes)
		}
	}

	// Enumerating parses frontmatter for display; that must not touch a file
	// either. This is the property a later round-trip task depends on: a full
	// read of the live bundle must leave `git diff` empty.
	if _, err := b.Contents(); err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if after := hashTree(t, root); after != before {
		t.Errorf("the tree changed across a full read:\nbefore %s\nafter  %s", before, after)
	}
}

// TestHeaderScanDoesNotAlterTheBytesItReads pins the separation between the
// display projection and the file: a header is read out of the same bytes the
// editor will get, and reading it changes nothing.
func TestHeaderScanDoesNotAlterTheBytesItReads(t *testing.T) {
	original := []byte(awkwardBytes)
	data := append([]byte(nil), original...)
	h := bundle.ScanHeader(data)
	if !bytes.Equal(data, original) {
		t.Fatal("ScanHeader mutated the slice it was given")
	}
	if h.ID != "awkward" || h.Name != "Awkward" {
		t.Fatalf("ScanHeader = %+v; want id awkward, name Awkward (CRLF and trailing whitespace trimmed for display only)", h)
	}
}

// hashTree fingerprints every file's path and bytes under root.
func hashTree(t *testing.T, root string) string {
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
		if d.IsDir() {
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
