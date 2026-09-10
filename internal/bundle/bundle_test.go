package bundle_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// fixtureRoot is the bundle every test in this package reads. Tests never
// touch ~/dev/projects/agent-setup: the live bundle is a git repo the user is
// editing, and a test suite that depends on its contents breaks whenever they
// do. See live_test.go for the opt-in census against the real thing.
const fixtureRoot = "testdata/fixture"

func openFixture(t *testing.T) *bundle.Bundle {
	t.Helper()
	b, err := bundle.Open(fixtureRoot)
	if err != nil {
		t.Fatalf("Open(%q): %v", fixtureRoot, err)
	}
	return b
}

func TestOpenRejectsBadRoots(t *testing.T) {
	if _, err := bundle.Open(""); err == nil {
		t.Fatal("Open(\"\") succeeded; want an error")
	}
	if _, err := bundle.Open(filepath.Join(fixtureRoot, "no-such-directory")); err == nil {
		t.Fatal("Open of a missing directory succeeded; want an error")
	}
	if _, err := bundle.Open(filepath.Join(fixtureRoot, "profiles", "base.md")); err == nil {
		t.Fatal("Open of a file succeeded; want an error")
	}
}

func TestOpenMakesTheRootAbsolute(t *testing.T) {
	b := openFixture(t)
	if !filepath.IsAbs(b.Root()) {
		t.Fatalf("Root() = %q; want an absolute path", b.Root())
	}
}

func TestEnumeratesAllFiveKinds(t *testing.T) {
	b := openFixture(t)
	c, err := b.Contents()
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}

	profiles := make([]string, 0, len(c.Profiles))
	for _, p := range c.Profiles {
		profiles = append(profiles, string(p.ID))
	}
	wantEqual(t, "profiles", profiles, []string{"architect", "base", "engineer", "headerless", "quoted"})

	templates := make([]string, 0, len(c.Templates))
	for _, tpl := range c.Templates {
		templates = append(templates, string(tpl.ID))
	}
	// Nested templates enumerate at any depth, and a nested id carries its
	// own path — which is what makes it addressable, since a bare basename
	// would collide the moment two directories hold the same name.
	// .hidden-note.md is a dotfile and stays out.
	wantEqual(t, "templates", templates, []string{"agents", "claude", "lenses/architect", "lenses/engineer", "projects/deep"})

	prompts := make([]string, 0, len(c.Prompts))
	for _, p := range c.Prompts {
		prompts = append(prompts, string(p.ID))
	}
	wantEqual(t, "prompts", prompts, []string{"greeting", "report"})

	skills := make([]string, 0, len(c.Skills))
	for _, s := range c.Skills {
		skills = append(skills, string(s.Name))
	}
	wantEqual(t, "skills", skills, []string{"commit", "no-skill-file", "push"})

	hooks := make([]string, 0, len(c.Hooks))
	for _, h := range c.Hooks {
		hooks = append(hooks, string(h.Name))
	}
	// hooks/README.md is not a hook.
	wantEqual(t, "hooks", hooks, []string{"session-start", "stop-disposition"})

}

// TestNestedTemplatesAreAddressableByPath is what replaced the role-prose
// collision test.
//
// templates/roles/*.md was its own kind precisely because every one of its
// files shared a basename with a profile, and an id alone could not name a
// file. That kind retired with the directory, and the same hazard came
// straight back one level down: templates/ now holds lenses/ and projects/,
// and nothing stops two of them holding the same basename. The id carrying
// its own path is what answers it, so this test pins that rather than the
// collision it used to.
func TestNestedTemplatesAreAddressableByPath(t *testing.T) {
	b := openFixture(t)

	nested := bundle.Ref{Kind: bundle.KindTemplate, ID: "lenses/architect"}
	top := bundle.Ref{Kind: bundle.KindTemplate, ID: "architect"}
	if nested == top {
		t.Fatal("a nested template ref equals the bare basename ref")
	}

	nestedPath, err := b.Resolve(nested)
	if err != nil {
		t.Fatalf("Resolve(%+v): %v", nested, err)
	}
	if got, want := filepath.ToSlash(nestedPath), "templates/lenses/architect.md"; !hasSuffix(got, want) {
		t.Fatalf("nested template resolved to %q; want a path ending %q", got, want)
	}

	// The bare basename names nothing: there is no templates/architect.md,
	// and resolution must not fall back to the nested one.
	if _, err := b.Resolve(top); !errors.Is(err, bundle.ErrNotFound) {
		t.Fatalf("Resolve(bare %q) = %v; want ErrNotFound rather than a fallback to the nested file", top.ID, err)
	}

	// And a profile of the same name is still a different artifact.
	profilePath, err := b.Resolve(bundle.Ref{Kind: bundle.KindProfile, ID: "architect"})
	if err != nil {
		t.Fatalf("Resolve(profile architect): %v", err)
	}
	if profilePath == nestedPath {
		t.Fatalf("both architect refs resolved to %s", profilePath)
	}
}

func TestProfileHeadersAreReadForDisplay(t *testing.T) {
	b := openFixture(t)
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	got := map[string]bundle.Header{}
	for _, p := range profiles {
		got[string(p.ID)] = p.Header
	}

	want := map[string]bundle.Header{
		"architect": {Present: true, ID: "architect", Name: "Architect", Extends: "base",
			Description: "Decides structure and boundaries, including what they rule out."},
		// The fixture's base declares abstract: true, as the live one does.
		"base": {Present: true, ID: "base", Name: "Fixture Base", Abstract: true},
		"engineer": {Present: true, ID: "engineer", Name: "Engineer", Extends: "base",
			Description: "Implements one task end to end."},
		"quoted": {Present: true, ID: "quoted", Name: "A name: with a colon", Extends: "base",
			Description: "plain value"},
		// No frontmatter is not an error. The file still enumerates (D8).
		"headerless": {},
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("profile %s header = %+v; want %+v", id, got[id], w)
		}
	}
}

func TestSkillsReportExistenceRatherThanEnforcingIt(t *testing.T) {
	b := openFixture(t)
	skills, err := b.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	byName := map[bundle.SkillID]bundle.Skill{}
	for _, s := range skills {
		byName[s.Name] = s
	}

	commit, ok := byName["commit"]
	if !ok {
		t.Fatal("skill commit missing")
	}
	if !commit.HasSkillFile {
		t.Error("skill commit: HasSkillFile = false; want true")
	}
	if commit.Header.Name != "commit" || commit.Header.Description == "" {
		t.Errorf("skill commit header = %+v; want name and description read from SKILL.md", commit.Header)
	}

	orphan, ok := byName["no-skill-file"]
	if !ok {
		t.Fatal("a skill directory without SKILL.md was dropped from the enumeration; existence is reported, not enforced")
	}
	if orphan.HasSkillFile {
		t.Error("skill no-skill-file: HasSkillFile = true; want false")
	}
	if orphan.Path == "" {
		t.Error("skill no-skill-file: Path is empty; the intended SKILL.md path should still be reported")
	}
	if _, err := b.Resolve(orphan.Ref()); !errors.Is(err, bundle.ErrNotFound) {
		t.Errorf("Resolve of a skill without SKILL.md: err = %v; want ErrNotFound", err)
	}
}

// TestMissingDirectoriesEnumerateEmpty: an absent artifact directory reads
// as "nothing here", not as a fault. Only the root's own absence is an
// error — see ErrRootMissing.
func TestMissingDirectoriesEnumerateEmpty(t *testing.T) {
	b, err := bundle.Open("testdata/minimal")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	c, err := b.Contents()
	if err != nil {
		t.Fatalf("Contents on a bundle with only profiles/: %v", err)
	}
	if len(c.Profiles) != 1 {
		t.Fatalf("profiles = %d; want 1", len(c.Profiles))
	}
	for name, n := range map[string]int{
		"templates": len(c.Templates),
		"prompts":   len(c.Prompts),
		"skills":    len(c.Skills),
		"hooks":     len(c.Hooks),
	} {
		if n != 0 {
			t.Errorf("%s = %d; want 0", name, n)
		}
	}
	// Non-nil, so a caller can range without a nil check.
	if c.Templates == nil {
		t.Error("Templates is nil; want an empty slice")
	}
}

func TestReadRejectsUnknownAndEscapingRefs(t *testing.T) {
	b := openFixture(t)
	cases := []bundle.Ref{
		{Kind: bundle.KindProfile, ID: "nope"},
		{Kind: bundle.KindProfile, ID: "../../go"},
		{Kind: bundle.KindTemplate, ID: "../agents"},
		{Kind: bundle.KindTemplate, ID: "lenses/../../go"},
		{Kind: bundle.KindTemplate, ID: "nope/architect"},
		{Kind: bundle.KindPrompt, ID: "../report"},
		{Kind: bundle.KindHook, ID: "README"},
		{Kind: bundle.KindSkill, ID: ".."},
	}
	for _, ref := range cases {
		if _, err := b.Read(ref); !errors.Is(err, bundle.ErrNotFound) {
			t.Errorf("Read(%+v): err = %v; want ErrNotFound", ref, err)
		}
	}
	if _, err := b.Read(bundle.Ref{Kind: "invented", ID: "x"}); err == nil {
		t.Error("Read with an unknown kind succeeded; want an error")
	}
}

func TestDotfilesAreSkipped(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "templates"))
	writeFile(t, filepath.Join(root, "templates", "agents.md"), "real\n")
	// The case from the live bundle, which cannot be committed as a fixture
	// because .DS_Store is globally gitignored on this machine.
	writeFile(t, filepath.Join(root, "templates", ".DS_Store"), "\x00\x00binary junk\n")

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	templates, err := b.Templates()
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	if len(templates) != 1 || templates[0].ID != "agents" {
		t.Fatalf("templates = %+v; want just agents", templates)
	}
}

func TestExpandRootHandlesTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	got, err := bundle.ExpandRoot("~/dev/projects/agent-setup")
	if err != nil {
		t.Fatalf("ExpandRoot: %v", err)
	}
	if want := filepath.Join(home, "dev", "projects", "agent-setup"); got != want {
		t.Fatalf("ExpandRoot = %q; want %q", got, want)
	}
}

func TestKindsCoversEveryKind(t *testing.T) {
	kinds := bundle.Kinds()
	if len(kinds) != 5 {
		t.Fatalf("Kinds() has %d entries; the bundle has five artifact kinds", len(kinds))
	}
	seen := map[bundle.Kind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("Kinds() repeats %q", k)
		}
		seen[k] = true
	}
	for _, k := range []bundle.Kind{
		bundle.KindProfile, bundle.KindTemplate, bundle.KindPrompt,
		bundle.KindSkill, bundle.KindHook,
	} {
		if !seen[k] {
			t.Errorf("Kinds() omits %q", k)
		}
	}
}

func wantEqual(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v (%d); want %v (%d)", what, got, len(got), want, len(want))
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v; want %v", what, got, want)
			return
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
