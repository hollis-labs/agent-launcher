package skeleton_test

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
	"github.com/hollis-labs/tachyon/internal/skeleton"
)

// TestLiveBundleCreateThenCleanUp is CW-20260903-0010's live-bundle proof,
// in the same shape internal/bundle's and internal/manager's own live tests
// use: skipped unless TACHYON_LIVE_BUNDLE is set, fingerprints the tree
// before and after, and fails loudly if a single byte outside its own
// created-then-removed files moved.
//
// It creates one profile and one skill against the REAL bundle, confirms
// each is picked up by internal/bundle's enumeration with no restart and no
// cache to invalidate (the exact "skill picked up without a restart"
// acceptance criterion), and then removes both before returning — every
// path, success or failure, via t.Cleanup, so a panic or a t.Fatal partway
// through still leaves the live bundle exactly as it found it.
//
// It deliberately does NOT invoke `cairn boot`: the plan's hard fence is
// "zero Cairn dependency for anything except the one explicit acceptance
// check that requires it," and that check is a manual, one-off terminal
// command a person runs once against a artifact this test creates by hand,
// not something wired into Tachyon or its test suite. See the task's commit
// message / report for that command and its output.
//
//	TACHYON_LIVE_BUNDLE=1 go test ./internal/skeleton/ -run TestLiveBundleCreateThenCleanUp -v
func TestLiveBundleCreateThenCleanUp(t *testing.T) {
	if os.Getenv("TACHYON_LIVE_BUNDLE") == "" {
		t.Skip("set TACHYON_LIVE_BUNDLE=1 to create-and-remove against the real bundle; the suite reads a temp dir by default")
	}
	root := os.Getenv("TACHYON_BUNDLE_ROOT")
	if root == "" {
		var err error
		if root, err = bundle.DefaultRoot(); err != nil {
			t.Fatalf("DefaultRoot: %v", err)
		}
	}
	t.Logf("bundle root: %s", root)

	before := hashTree(t, root)

	const profileID = "tachyon-t06-livetest"
	const skillID = "tachyon-t06-livetest"

	profilePath := filepath.Join(root, "profiles", profileID+".md")
	skillDir := filepath.Join(root, "skills", skillID)

	// Cleanup runs regardless of how the test ends -- t.Cleanup, not a plain
	// defer conditioned on success, is what keeps a t.Fatalf partway through
	// from leaving a stray artifact in Chrispian's real bundle.
	t.Cleanup(func() {
		os.Remove(profilePath)
		os.RemoveAll(skillDir)
		if after := hashTree(t, root); after != before {
			t.Errorf(
				"the live bundle did not return to its exact starting fingerprint after cleanup:\n"+
					"before %s\nafter  %s\n"+
					"cd %s && git status --porcelain && git diff -- this must be investigated and fixed before "+
					"anything else touches this bundle.",
				before, after, root)
		} else {
			t.Logf("tree fingerprint restored: %s", before)
		}
	})

	profileRef, err := skeleton.New(root, skeleton.Spec{
		Kind:        bundle.KindProfile,
		ID:          profileID,
		Description: "CW-20260903-0010 live-bundle proof. Removed by the test that created it.",
	})
	if err != nil {
		t.Fatalf("New(profile): %v", err)
	}
	skillRef, err := skeleton.New(root, skeleton.Spec{
		Kind:        bundle.KindSkill,
		ID:          skillID,
		Description: "CW-20260903-0010 live-bundle proof. Removed by the test that created it.",
	})
	if err != nil {
		t.Fatalf("New(skill): %v", err)
	}

	// "Picked up without a restart": bundle.Bundle caches nothing (see its
	// package doc), so simply opening the bundle again -- the same Open call
	// every other operation makes, with no special-cased reload -- and
	// enumerating is the whole proof.
	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	foundProfile := false
	for _, p := range profiles {
		if p.ID == bundle.ProfileID(profileRef.ID) {
			foundProfile = true
		}
	}
	if !foundProfile {
		t.Error("the profile just created does not appear in Profiles() against the live bundle")
	}

	skills, err := b.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	foundSkill := false
	for _, s := range skills {
		if s.Name == bundle.SkillID(skillRef.ID) {
			foundSkill = true
			if !s.HasSkillFile {
				t.Error("the skill just created has HasSkillFile false")
			}
		}
	}
	if !foundSkill {
		t.Error("the skill just created does not appear in Skills() against the live bundle")
	}

	t.Logf("created and verified profiles/%s.md and skills/%s/SKILL.md against the live bundle; cleanup will remove both", profileID, skillID)
}

// hashTree mirrors internal/bundle's and internal/manager's own
// hashBundleTree/hashLiveTree helpers exactly, duplicated here rather than
// exported from either package (both are unexported test helpers in
// _test.go files, which nothing outside their own package can import).
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
