package bundle_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// The census of ~/dev/projects/agent-setup, recounted 2026-09-10 after the
// bundle's own reshaping: bindings/ retired entirely, templates/roles/ with
// it, and templates/ grew lenses/ and projects/ in their place.
//
// liveTemplates is the number that moved most and says the most:
// Templates() reads at any depth now, so it counts all 13 files under
// templates/ rather than the 3 at its top level. The previous 4 was
// measured when roles/ was a separate kind and templates/agents.md still
// existed.
//
// Counts include each directory's own README where it has one — every
// enumeration filters by extension, not by name.
const (
	liveProfiles  = 21
	liveTemplates = 13
	livePrompts   = 2
	liveSkills    = 29
	liveHooks     = 7
)

// TestLiveBundleCensus runs the enumeration against the real bundle.
//
// It is skipped unless TACHYON_LIVE_BUNDLE is set, because the suite must not
// depend on a tree the user is editing — every other test in this package
// reads testdata/. It exists so the acceptance numbers can be reproduced on
// demand, and it is strictly read-only: it also fingerprints the tree before
// and after and fails if a single byte moved.
//
//	TACHYON_LIVE_BUNDLE=1 go test ./internal/bundle/ -run TestLiveBundleCensus -v
func TestLiveBundleCensus(t *testing.T) {
	if os.Getenv("TACHYON_LIVE_BUNDLE") == "" {
		t.Skip("set TACHYON_LIVE_BUNDLE=1 to enumerate the real bundle; the suite reads testdata/ by default")
	}
	root := os.Getenv("TACHYON_BUNDLE_ROOT")
	if root == "" {
		var err error
		if root, err = bundle.DefaultRoot(); err != nil {
			t.Fatalf("DefaultRoot: %v", err)
		}
	}
	t.Logf("bundle root: %s", root)

	before := hashBundleTree(t, root)

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open(%s): %v", root, err)
	}
	c, err := b.Contents()
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}

	t.Logf("profiles   %2d  %s", len(c.Profiles), joinProfiles(c.Profiles))
	t.Logf("templates  %2d  %s", len(c.Templates), joinTemplates(c.Templates))
	t.Logf("prompts    %2d  %s", len(c.Prompts), joinPrompts(c.Prompts))
	t.Logf("skills     %2d  %s", len(c.Skills), joinSkills(c.Skills))
	t.Logf("hooks      %2d  %s", len(c.Hooks), joinHooks(c.Hooks))

	const note = "; if the bundle has genuinely changed, that is not a defect in this package — update the census constants"
	for _, tc := range []struct {
		kind string
		got  int
		want int
	}{
		{"profiles", len(c.Profiles), liveProfiles},
		{"templates", len(c.Templates), liveTemplates},
		{"prompts", len(c.Prompts), livePrompts},
		{"skills", len(c.Skills), liveSkills},
		{"hooks", len(c.Hooks), liveHooks},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d; want %d%s", tc.kind, tc.got, tc.want, note)
		}
	}

	// Every skill directory carries its SKILL.md, and every profile parses far
	// enough to label a tree row.
	for _, s := range c.Skills {
		if !s.HasSkillFile {
			t.Errorf("skill %s has no %s", s.Name, "SKILL.md")
		}
	}
	for _, p := range c.Profiles {
		if !p.Header.Present || p.Header.ID == "" || p.Header.Name == "" {
			t.Errorf("profile %s: header = %+v; want at least id and name", p.ID, p.Header)
		}
	}

	// Every nested template's id carries its own path, which is what makes
	// it addressable at all: templates/ holds lenses/ and projects/ now, and
	// a bare basename would collide the moment two of them agree.
	nested := 0
	for _, tpl := range c.Templates {
		if strings.Contains(string(tpl.ID), "/") {
			nested++
		}
	}
	t.Logf("templates below the top level: %d of %d", nested, len(c.Templates))
	if nested == 0 {
		t.Error("no nested template found; this census exists partly to prove Templates() descends, so a flat result means either the bundle or the walk changed")
	}

	// Read every artifact's bytes, then check nothing moved.
	read := 0
	for _, p := range c.Profiles {
		readLive(t, b, p.Ref())
		read++
	}
	for _, tpl := range c.Templates {
		readLive(t, b, tpl.Ref())
		read++
	}
	for _, p := range c.Prompts {
		readLive(t, b, p.Ref())
		read++
	}
	for _, s := range c.Skills {
		readLive(t, b, s.Ref())
		read++
	}
	for _, h := range c.Hooks {
		readLive(t, b, h.Ref())
		read++
	}
	t.Logf("read %d artifacts", read)

	if after := hashBundleTree(t, root); after != before {
		t.Fatalf("the live bundle changed across a full read:\nbefore %s\nafter  %s", before, after)
	}
	t.Logf("tree fingerprint unchanged: %s", before)
}

func readLive(t *testing.T, b *bundle.Bundle, ref bundle.Ref) {
	t.Helper()
	got, err := b.Read(ref)
	if err != nil {
		t.Fatalf("Read(%+v): %v", ref, err)
	}
	path, err := b.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve(%+v): %v", ref, err)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("Read(%+v) does not match the file on disk", ref)
	}
}

// hashBundleTree fingerprints the bundle's files, skipping dot-directories so
// that .git is not walked.
func hashBundleTree(t *testing.T, root string) string {
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

func joinProfiles(in []bundle.Profile) string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v.ID)
	}
	return strings.Join(out, " ")
}

func joinTemplates(in []bundle.Template) string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v.ID)
	}
	return strings.Join(out, " ")
}

func joinPrompts(in []bundle.Prompt) string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v.ID)
	}
	return strings.Join(out, " ")
}

func joinSkills(in []bundle.Skill) string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v.Name)
	}
	return strings.Join(out, " ")
}

func joinHooks(in []bundle.Hook) string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v.Name)
	}
	return strings.Join(out, " ")
}
