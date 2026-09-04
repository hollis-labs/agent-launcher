package apply_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/apply"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCompare_DarkLitDark is the literal acceptance criterion: stage a
// scratch bundle's {templates,skills,prompts} into a scratch AGENTS_HOME so
// they match (dark), edit a file in the bundle (lit), revert the edit to
// its original content (dark again). A dirty flag would stay lit after the
// revert; a real tree comparison must not.
func TestCompare_DarkLitDark(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	const original = "# hello\noriginal content\n"
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), original)
	mustWrite(t, filepath.Join(agentsHome, "prompts", "hello.md"), original)

	// --- dark: bundle and staged layer match ---
	sum, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare (dark): %v", err)
	}
	if sum.Differs {
		t.Fatalf("Compare (dark) = %+v; want Differs=false when bundle and AGENTS_HOME match", sum)
	}
	if sum.Description != "" {
		t.Errorf("Description (dark) = %q; want empty", sum.Description)
	}

	// --- lit: edit the bundle's copy ---
	const edited = "# hello\nEDITED content\n"
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), edited)

	sum, err = apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare (lit): %v", err)
	}
	if !sum.Differs {
		t.Fatalf("Compare (lit) = %+v; want Differs=true after editing the bundle's copy", sum)
	}
	if sum.Description != "1 prompt" {
		t.Errorf("Description (lit) = %q; want %q", sum.Description, "1 prompt")
	}

	// --- dark again: revert the edit to its original content ---
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), original)

	sum, err = apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare (dark again): %v", err)
	}
	if sum.Differs {
		t.Fatalf("Compare (dark again) = %+v; want Differs=false once the edit is reverted -- "+
			"a dirty flag would still read true here, which is exactly the bug this comparison exists to avoid", sum)
	}
}

// TestCompare_DSStoreExcludedBothSides proves a .DS_Store present on one
// side and not the other never lights up Apply -- matching
// install-system's own --exclude='.DS_Store' (reinforced by
// --delete-excluded), which never transfers or deletes it either.
func TestCompare_DSStoreExcludedBothSides(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	const content = "skill body\n"
	mustWrite(t, filepath.Join(bundleRoot, "skills", "demo", "SKILL.md"), content)
	mustWrite(t, filepath.Join(agentsHome, "skills", "demo", "SKILL.md"), content)

	// A .DS_Store in the bundle only.
	mustWrite(t, filepath.Join(bundleRoot, "skills", ".DS_Store"), "junk")
	// A *different* .DS_Store in the staged layer only, and a third one
	// nested deeper, to prove exclusion applies at any depth on both sides.
	mustWrite(t, filepath.Join(agentsHome, "skills", "demo", ".DS_Store"), "other junk")
	mustWrite(t, filepath.Join(bundleRoot, "templates", "roles", ".DS_Store"), "yet more junk")

	sum, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if sum.Differs {
		t.Fatalf("Compare = %+v; want Differs=false -- .DS_Store on either side must never count as a difference", sum)
	}
}

// TestCompare_MissingStagedDirectoryDiffersNotError is the "a completely
// missing staged AGENTS_HOME directory counts as differs, not an error"
// acceptance bullet: agentsHome does not exist at all (not even created by
// t.TempDir), and the bundle has real content in it.
func TestCompare_MissingStagedDirectoryDiffersNotError(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := filepath.Join(t.TempDir(), "does-not-exist-yet")

	mustWrite(t, filepath.Join(bundleRoot, "templates", "agents.md"), "template body\n")

	sum, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare with a wholly missing AGENTS_HOME returned an error, want none: %v", err)
	}
	if !sum.Differs {
		t.Fatalf("Compare = %+v; want Differs=true when AGENTS_HOME does not exist and the bundle has content", sum)
	}

	var templates apply.KindDiff
	for _, k := range sum.Kinds {
		if k.Kind == "templates" {
			templates = k
		}
	}
	if len(templates.Added) != 1 || templates.Added[0] != "agents.md" {
		t.Errorf("templates.Added = %v; want [\"agents.md\"] (nothing is staged yet, so the whole bundle side is new)", templates.Added)
	}
	if len(templates.Removed) != 0 {
		t.Errorf("templates.Removed = %v; want none -- there is nothing at the missing destination to delete", templates.Removed)
	}
}

// TestCompare_MissingBundleDirectoryStillDiffersAgainstRealStagedContent
// covers the mirror case: nothing in the bundle at all for a kind that IS
// staged -- every staged file reads as Removed (would be deleted), which is
// exactly what `make install-system`'s --delete would do against an empty
// source directory.
func TestCompare_MissingBundleDirectoryStillDiffersAgainstRealStagedContent(t *testing.T) {
	bundleRoot := t.TempDir() // no skills/ at all
	agentsHome := t.TempDir()
	mustWrite(t, filepath.Join(agentsHome, "skills", "orphan", "SKILL.md"), "leftover\n")

	sum, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !sum.Differs {
		t.Fatalf("Compare = %+v; want Differs=true", sum)
	}
	var skills apply.KindDiff
	for _, k := range sum.Kinds {
		if k.Kind == "skills" {
			skills = k
		}
	}
	if len(skills.Removed) != 1 || skills.Removed[0] != "orphan/SKILL.md" {
		t.Errorf("skills.Removed = %v; want [\"orphan/SKILL.md\"]", skills.Removed)
	}
}

// TestCompare_ContentNotJustFilename proves a same-named file with
// different content counts as a difference -- the acceptance criterion's
// own explicit requirement, "content-based, not just filename-based".
func TestCompare_ContentNotJustFilename(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	mustWrite(t, filepath.Join(bundleRoot, "prompts", "report.md"), "version A\n")
	mustWrite(t, filepath.Join(agentsHome, "prompts", "report.md"), "version B (different bytes, same name)\n")

	sum, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !sum.Differs {
		t.Fatal("Compare says nothing differs for two files with the same name but different content")
	}
	var prompts apply.KindDiff
	for _, k := range sum.Kinds {
		if k.Kind == "prompts" {
			prompts = k
		}
	}
	if len(prompts.Changed) != 1 || prompts.Changed[0] != "report.md" {
		t.Errorf("prompts.Changed = %v; want [\"report.md\"]", prompts.Changed)
	}
	if len(prompts.Added) != 0 || len(prompts.Removed) != 0 {
		t.Errorf("prompts.Added/Removed = %v/%v; want both empty -- the file exists on both sides, only its content differs", prompts.Added, prompts.Removed)
	}
}

// TestCompare_DescriptionNamesWhatDiffers is the acceptance bullet
// "report what differs, not just that something does" -- the task's own
// example shape, "3 prompts, 1 template".
func TestCompare_DescriptionNamesWhatDiffers(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	mustWrite(t, filepath.Join(bundleRoot, "templates", "agents.md"), "t1\n")
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "a.md"), "p1\n")
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "b.md"), "p2\n")
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "c.md"), "p3\n")

	sum, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	want := "1 template, 3 prompts"
	if sum.Description != want {
		t.Errorf("Description = %q; want %q", sum.Description, want)
	}
}

// TestCompare_AlwaysAllThreeKinds pins the "always all groups, some empty"
// discipline this package borrows from internal/manager.Tree: Summary.Kinds
// is always exactly len(apply.Kinds) entries, in that order, even when the
// bundle is entirely empty.
func TestCompare_AlwaysAllThreeKinds(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	sum, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(sum.Kinds) != len(apply.Kinds) {
		t.Fatalf("len(Kinds) = %d; want %d", len(sum.Kinds), len(apply.Kinds))
	}
	for i, want := range apply.Kinds {
		if sum.Kinds[i].Kind != want {
			t.Errorf("Kinds[%d].Kind = %q; want %q", i, sum.Kinds[i].Kind, want)
		}
	}
}
