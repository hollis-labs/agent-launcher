package skeleton_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/skeleton"
)

// tempRoot returns a fresh, empty bundle root a test can create artifacts
// in. Its parent directories need not pre-exist — New creates them.
func tempRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestSupportedKindsExcludesOnlyHook(t *testing.T) {
	got := skeleton.SupportedKinds()
	if len(got) == 0 {
		t.Fatal("SupportedKinds() is empty")
	}
	want := map[bundle.Kind]bool{
		bundle.KindProfile:   true,
		bundle.KindRoleProse: true,
		bundle.KindTemplate:  true,
		bundle.KindPrompt:    true,
		bundle.KindSkill:     true,
		bundle.KindBinding:   true,
	}
	seen := map[bundle.Kind]bool{}
	for _, k := range got {
		seen[k] = true
		if !want[k] {
			t.Errorf("SupportedKinds() unexpectedly contains %s", k)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("SupportedKinds() is missing %s", k)
		}
	}
	if seen[bundle.KindHook] {
		t.Error("SupportedKinds() contains KindHook -- hook creation is out of this package's scope")
	}

	// Order matches bundle.Kinds()'s stable presentational order, not
	// insertion order into the registry map (which Go would randomize).
	order := map[bundle.Kind]int{}
	for i, k := range bundle.Kinds() {
		order[k] = i
	}
	for i := 1; i < len(got); i++ {
		if order[got[i-1]] >= order[got[i]] {
			t.Fatalf("SupportedKinds() = %v is not in bundle.Kinds() order", got)
		}
	}
}

func TestNewRejectsUnsupportedKindHook(t *testing.T) {
	root := tempRoot(t)
	_, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindHook, ID: "x"})
	if !errors.Is(err, skeleton.ErrKindNotSupported) {
		t.Fatalf("New(KindHook, ...) error = %v; want ErrKindNotSupported", err)
	}
}

func TestNewRejectsInvalidIDs(t *testing.T) {
	root := tempRoot(t)
	bad := []string{"", ".", "..", "a/b", "../escape", ".hidden", "with space", "with:colon", "-leading-hyphen"}
	for _, id := range bad {
		_, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: id})
		if !errors.Is(err, skeleton.ErrInvalidID) {
			t.Errorf("New(id=%q) error = %v; want ErrInvalidID", id, err)
		}
	}
}

func TestNewRejectsAPathEscapingID(t *testing.T) {
	root := tempRoot(t)
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "../../etc/passwd"}); !errors.Is(err, skeleton.ErrInvalidID) {
		t.Fatalf("New with a traversal id error = %v; want ErrInvalidID", err)
	}
	// Confirm nothing escaped the root regardless.
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(root)), "passwd.md")); err == nil {
		t.Fatal("a file landed outside the bundle root")
	}
}

func TestNewCreatesProfile(t *testing.T) {
	root := tempRoot(t)
	ref, err := skeleton.New(root, skeleton.Spec{
		Kind:        bundle.KindProfile,
		ID:          "newrole",
		Description: "Handles error: recovery, and says \"sorry\" a lot.",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ref.Kind != bundle.KindProfile || ref.ID != "newrole" {
		t.Fatalf("New returned ref %+v; want {profile newrole}", ref)
	}

	target := filepath.Join(root, "profiles", "newrole.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", target, err)
	}
	content := string(data)

	for _, want := range []string{
		"id: newrole",
		"extends: base",
		"provider: claude",
		"spec: {}",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("profile scaffold is missing %q\n---\n%s", want, content)
		}
	}

	// The description contains a colon-space and an embedded double quote --
	// both of which would corrupt an unquoted plain YAML scalar -- so it must
	// have been quoted and escaped.
	if !strings.Contains(content, `description: "Handles error: recovery, and says \"sorry\" a lot."`) {
		t.Errorf("profile scaffold's description was not safely quoted:\n%s", content)
	}

	// Round-trip through internal/bundle's own tree machinery: this is what
	// "picked up without a restart" actually means -- bundle.Bundle caches
	// nothing, so enumerating again after New, with no reopen ceremony,
	// must show the new profile.
	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	found := false
	for _, p := range profiles {
		if p.ID == "newrole" {
			found = true
			if !p.Header.Present {
				t.Error("newrole's Header.Present is false; frontmatter should have scanned")
			}
			if p.Header.ID != "newrole" {
				t.Errorf("newrole's Header.ID = %q; want %q", p.Header.ID, "newrole")
			}
			if p.Header.Extends != "base" {
				t.Errorf("newrole's Header.Extends = %q; want %q", p.Header.Extends, "base")
			}
			if p.Header.Name != "Newrole" {
				t.Errorf("newrole's Header.Name = %q; want the default title-cased name %q", p.Header.Name, "Newrole")
			}
			if p.Header.Description != "Handles error: recovery, and says \"sorry\" a lot." {
				t.Errorf("newrole's Header.Description = %q; want the original text back, unmangled", p.Header.Description)
			}
		}
	}
	if !found {
		t.Fatal("the profile just created by New does not appear in Profiles() -- 'picked up without a restart' failed")
	}
}

func TestNewProfileDefaultNameIsTitleCased(t *testing.T) {
	root := tempRoot(t)
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "search-first"}); err != nil {
		t.Fatalf("New: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "profiles", "search-first.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "name: Search First\n") {
		t.Errorf("default name was not title-cased from the id:\n%s", data)
	}
}

func TestNewCreatesRoleProse(t *testing.T) {
	root := tempRoot(t)
	ref, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindRoleProse, ID: "newrole", Name: "New Role"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ref.Kind != bundle.KindRoleProse {
		t.Fatalf("ref.Kind = %s; want role-prose", ref.Kind)
	}
	target := filepath.Join(root, "templates", "roles", "newrole.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", target, err)
	}
	if !strings.HasPrefix(string(data), "# New Role\n") {
		t.Errorf("role prose scaffold does not open with the heading:\n%s", data)
	}
	// Bare prose: no frontmatter delimiter at all.
	if strings.HasPrefix(string(data), "---") {
		t.Errorf("role prose scaffold has frontmatter; it must be bare prose:\n%s", data)
	}

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rp, err := b.RoleProse()
	if err != nil {
		t.Fatalf("RoleProse: %v", err)
	}
	found := false
	for _, r := range rp {
		if r.Role == "newrole" {
			found = true
		}
	}
	if !found {
		t.Fatal("the role prose file just created does not appear in RoleProse()")
	}
}

// TestNewCreatesPrompt is CW-20260904-0006's own acceptance criterion:
// prompts/<id>.md, scaffolded from a title heading and an explanatory HTML
// comment (roleProseScaffold's own shape) rather than any invented slot or
// value content -- see promptScaffold's own doc for why. Writes into a
// t.TempDir() bundle, exactly like every other kind's creation test in this
// file; the real ~/dev/projects/agent-setup is never touched here.
func TestNewCreatesPrompt(t *testing.T) {
	root := tempRoot(t)
	ref, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindPrompt, ID: "newprompt", Name: "New Prompt"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ref.Kind != bundle.KindPrompt {
		t.Fatalf("ref.Kind = %s; want prompt", ref.Kind)
	}
	target := filepath.Join(root, "prompts", "newprompt.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", target, err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "# New Prompt\n") {
		t.Errorf("prompt scaffold does not open with the heading:\n%s", content)
	}
	// Bare prose: no frontmatter delimiter at all, same as role prose.
	if strings.HasPrefix(content, "---") {
		t.Errorf("prompt scaffold has frontmatter; it must be bare prose:\n%s", content)
	}
	// D8, and this task's own "keep the scaffold minimal" instruction: the
	// comment quotes prompts/README.md's own description of the marker
	// shape (<!-- cairn:slot ... --> / <!-- cairn:value ... -->) but must
	// not invent a real, specific marker of its own -- there is no
	// general-purpose slot or value name to derive one from, only
	// report.md's own task-specific ones (binding/profile/scope/session),
	// which are particular to that one prompt, not to prompts in general.
	if strings.Contains(content, "cairn:value scope") || strings.Contains(content, "cairn:value binding") {
		t.Errorf("prompt scaffold invented a specific marker rather than staying minimal:\n%s", content)
	}

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	prompts, err := b.Prompts()
	if err != nil {
		t.Fatalf("Prompts: %v", err)
	}
	found := false
	for _, p := range prompts {
		if p.ID == "newprompt" {
			found = true
		}
	}
	if !found {
		t.Fatal("the prompt just created does not appear in Prompts()")
	}
}

func TestNewCreatesTemplate(t *testing.T) {
	root := tempRoot(t)
	ref, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindTemplate, ID: "newtemplate"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ref.Kind != bundle.KindTemplate {
		t.Fatalf("ref.Kind = %s; want template", ref.Kind)
	}
	target := filepath.Join(root, "templates", "newtemplate.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", target, err)
	}
	content := string(data)
	for _, want := range []string{
		"binding, model, profile, provider, scope, session",
		"<!-- cairn:slot example -->",
		"- scope: <!-- cairn:value scope -->",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("template scaffold is missing %q\n---\n%s", want, content)
		}
	}

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	templates, err := b.Templates()
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	found := false
	for _, tpl := range templates {
		if tpl.ID == "newtemplate" {
			found = true
		}
	}
	if !found {
		t.Fatal("the template just created does not appear in Templates()")
	}
	// A new template must not be filed under templates/roles/ -- that is a
	// different kind (role prose) with a different id space.
	if _, err := os.Stat(filepath.Join(root, "templates", "roles", "newtemplate.md")); err == nil {
		t.Fatal("the template landed under templates/roles/ instead of templates/")
	}
}

func TestNewCreatesSkill(t *testing.T) {
	root := tempRoot(t)
	ref, err := skeleton.New(root, skeleton.Spec{
		Kind:        bundle.KindSkill,
		ID:          "new-skill",
		Description: "Does the thing. Use when you need the thing done.",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ref.Kind != bundle.KindSkill {
		t.Fatalf("ref.Kind = %s; want skill", ref.Kind)
	}
	target := filepath.Join(root, "skills", "new-skill", "SKILL.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", target, err)
	}
	content := string(data)
	if !strings.Contains(content, "name: new-skill\n") {
		t.Errorf("skill scaffold's name does not match its directory name:\n%s", content)
	}
	if !strings.Contains(content, "description: Does the thing. Use when you need the thing done.\n") {
		t.Errorf("skill scaffold's description missing or altered:\n%s", content)
	}

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	skills, err := b.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	found := false
	for _, s := range skills {
		if s.Name == "new-skill" {
			found = true
			if !s.HasSkillFile {
				t.Error("new-skill's HasSkillFile is false")
			}
		}
	}
	if !found {
		t.Fatal("the skill just created does not appear in Skills() -- 'picked up without a restart' failed")
	}
}

// TestNewCreatesBinding is CW-20260904-0002's (T23) proof that the binding
// seam this package's doc used to describe as deferred is real: New writes
// bindings/<id>.yaml directly (not through internal/binding.Store.Create,
// which would reject the scaffold's empty placeholder values — see
// scaffolds.go's bindingScaffold doc), and internal/binding's own reader
// can immediately List and Get it back without erroring the whole
// directory, because a present-but-empty profile/scope key is valid, not
// corrupt.
func TestNewCreatesBinding(t *testing.T) {
	root := tempRoot(t)
	ref, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindBinding, ID: "fresh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Unlike every other kind, a binding's bundle.Ref.ID carries its file
	// extension (bundle.BindingID's own documented convention).
	if ref.Kind != bundle.KindBinding || ref.ID != "fresh.yaml" {
		t.Fatalf("New returned ref %+v; want {binding fresh.yaml}", ref)
	}

	target := filepath.Join(root, "bindings", "fresh.yaml")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", target, err)
	}
	content := string(data)
	if !strings.Contains(content, "profile:\n") || !strings.Contains(content, "scope:\n") {
		t.Errorf("binding scaffold does not carry both blank profile: and scope: keys:\n%s", content)
	}

	// Round-trip through internal/bundle's own tree machinery, same as
	// every other kind: picked up with no restart.
	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	bindings, err := b.Bindings()
	if err != nil {
		t.Fatalf("Bindings: %v", err)
	}
	found := false
	for _, bd := range bindings {
		if string(bd.Name) == "fresh.yaml" {
			found = true
		}
	}
	if !found {
		t.Fatal("the binding just created does not appear in Bindings() -- 'picked up without a restart' failed")
	}

	// internal/binding's own reader must accept the freshly scaffolded file
	// immediately: a present, empty profile:/scope: pair is valid, not a
	// parse failure that would take the rest of bindings/ down with it.
	got, err := binding.Open(root).Get("fresh")
	if err != nil {
		t.Fatalf("binding.Open(root).Get(fresh): %v", err)
	}
	if got.Name != "fresh" || got.Profile != "" || got.Scope != "" {
		t.Errorf("binding.Get(fresh) = %+v; want {fresh \"\" \"\"}", got)
	}
	list, err := binding.Open(root).List()
	if err != nil {
		t.Fatalf("binding.Open(root).List() with a freshly scaffolded, still-blank binding present: %v", err)
	}
	if len(list) != 1 || list[0].Name != "fresh" {
		t.Fatalf("binding.Open(root).List() = %+v; want exactly one binding named %q", list, "fresh")
	}
}

func TestNewRefusesToOverwriteAnExistingFile(t *testing.T) {
	root := tempRoot(t)
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "dup"}); err != nil {
		t.Fatalf("first New: %v", err)
	}
	// Prove it really would have overwritten by editing the file first.
	target := filepath.Join(root, "profiles", "dup.md")
	if err := os.WriteFile(target, []byte("hand-edited content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "dup"}); !errors.Is(err, skeleton.ErrAlreadyExists) {
		t.Fatalf("second New error = %v; want ErrAlreadyExists", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hand-edited content\n" {
		t.Fatalf("New overwrote an existing file; on disk: %q", data)
	}
}

func TestNewRefusesToOverwriteAnExistingSkillFile(t *testing.T) {
	root := tempRoot(t)
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindSkill, ID: "dup-skill"}); err != nil {
		t.Fatalf("first New: %v", err)
	}
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindSkill, ID: "dup-skill"}); !errors.Is(err, skeleton.ErrAlreadyExists) {
		t.Fatalf("second New error = %v; want ErrAlreadyExists", err)
	}
}

func TestNewReportsRootMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "x"})
	if !errors.Is(err, bundle.ErrRootMissing) {
		t.Fatalf("New against a missing root error = %v; want bundle.ErrRootMissing", err)
	}
}

func TestNewLeavesNoPartialFileOnAConcurrentRaceLoser(t *testing.T) {
	// Not a true concurrency test (that would be flaky by construction);
	// this instead confirms the O_EXCL contract directly: a second New for
	// the same id, run after the first completed, must find the file
	// untouched and must not leave any temp/partial artifact behind.
	root := tempRoot(t)
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindTemplate, ID: "race"}); err != nil {
		t.Fatalf("first New: %v", err)
	}
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindTemplate, ID: "race"}); !errors.Is(err, skeleton.ErrAlreadyExists) {
		t.Fatalf("second New error = %v; want ErrAlreadyExists", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "templates"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "race.md" {
			t.Errorf("unexpected leftover entry in templates/: %s", e.Name())
		}
	}
}
