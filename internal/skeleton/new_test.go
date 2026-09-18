package skeleton_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		bundle.KindProfile:  true,
		bundle.KindTemplate: true,
		bundle.KindPrompt:   true,
		bundle.KindSkill:    true,
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
		"spec: {}",
		// The body is a template now, and a scaffold that did not say so
		// would leave someone writing a profile that renders no prose.
		"{{ section charter }}",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("profile scaffold is missing %q\n---\n%s", want, content)
		}
	}

	// A profile must NOT declare a provider. No profile in agent-setup has
	// since 2026-09-10 -- a runtime is a launch's to choose -- and a
	// scaffold that wrote one would put it back where it just left, one new
	// profile at a time.
	if strings.Contains(content, "provider: claude") || strings.Contains(content, "provider: codex") {
		t.Errorf("profile scaffold declares a provider; that belongs to the launch profile:\n%s", content)
	}
	// Nor the retired marker engine, nor templates/roles/.
	for _, gone := range []string{"cairn:slot", "cairn:value", "templates/roles"} {
		if strings.Contains(content, gone) {
			t.Errorf("profile scaffold mentions the retired %q:\n%s", gone, content)
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

// TestGeneratedScaffoldsUseOnlyTheBundleRoot guards every artifact scaffold
// against reintroducing the retired installed-content layer. Every path a
// scaffold names must resolve inside the active Cairn bundle, and no
// generated artifact (including a part) may point at ~/.config/agents.
func TestGeneratedScaffoldsUseOnlyTheBundleRoot(t *testing.T) {
	for _, kind := range skeleton.SupportedKinds() {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			root := tempRoot(t)
			if _, err := skeleton.New(root, skeleton.Spec{Kind: kind, ID: "bundle-root-audit"}); err != nil {
				t.Fatalf("New(%s): %v", kind, err)
			}
			assertGeneratedTreeUsesBundleRoot(t, root)
		})
	}

	t.Run("part", func(t *testing.T) {
		root := tempRoot(t)
		if _, err := skeleton.NewPart(root, "bundle-root-audit"); err != nil {
			t.Fatalf("NewPart: %v", err)
		}
		assertGeneratedTreeUsesBundleRoot(t, root)
	})
}

func assertGeneratedTreeUsesBundleRoot(t *testing.T, root string) {
	t.Helper()
	var all strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		all.Write(data)
		return nil
	})
	if err != nil {
		t.Fatalf("reading generated scaffold tree: %v", err)
	}

	content := all.String()
	if strings.Contains(content, "~/.config/agents") {
		t.Fatalf("generated scaffold points at the retired installed-content root:\n%s", content)
	}
	// Every path a scaffold names must be bundle-relative through the
	// variable, so a bundle that moves needs no edit. This used to check one
	// hardcoded role-slot example; the scaffolds now show `{{ file: ... }}`
	// paths instead, and the rule is the same for all of them.
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, "{{ file:") && !strings.Contains(line, "static_file") {
			continue
		}
		if !strings.Contains(line, "$CAIRN_PROFILE_ROOT") {
			t.Errorf("a scaffold names a templates/ path that is not rooted at $CAIRN_PROFILE_ROOT: %q", strings.TrimSpace(line))
		}
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

// TestNewCreatesPrompt is CW-20260904-0006's own acceptance criterion:
// prompts/<id>.md, scaffolded from a title heading and an explanatory HTML
// comment rather than any invented content -- see promptScaffold's own doc
// for why. Writes into a
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
	// Bare prose: no frontmatter delimiter at all.
	if strings.HasPrefix(content, "---") {
		t.Errorf("prompt scaffold has frontmatter; it must be bare prose:\n%s", content)
	}
	// The scaffold must not teach the retired marker engine. agent-setup
	// moved off it on 2026-09-10 and cairn's own docs put every directive in
	// the profile now, so a marker here would hand someone a document the
	// live bundle has no reader for.
	if strings.Contains(content, "cairn:value") || strings.Contains(content, "cairn:slot") {
		t.Errorf("prompt scaffold teaches the retired marker engine:\n%s", content)
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
	// The scaffold points at the engine that is actually running: the
	// directives live in the PROFILE, and a template named by
	// `{{ file: ... }}` is substituted whole.
	for _, want := range []string{
		"{{ file:",
		"{{ section",
		"profiles/base.md",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("template scaffold is missing %q\n---\n%s", want, content)
		}
	}
	// And must not teach the retired one.
	for _, gone := range []string{"cairn:slot", "cairn:value", "templates/agents.md"} {
		if strings.Contains(content, gone) {
			t.Errorf("template scaffold still teaches the retired marker engine (%q):\n%s", gone, content)
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
