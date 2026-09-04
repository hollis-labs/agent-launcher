package binding_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
)

// copyFixtureBundle copies testdata/<name> (a whole bundle-root-shaped
// subtree: a bindings/ directory plus a scopes.yaml beside it) into a fresh
// temp directory and returns that directory — testdata/ is never mutated by
// a test run. The returned path is the bundle root, so a test wanting a
// [binding.FileStore] joins "bindings" onto it itself, matching how
// [binding.Open] does the same join in production.
func copyFixtureBundle(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", name)
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copying testdata/%s: %v", name, err)
	}
	return dst
}

func newFixtureStore(t *testing.T, name string) (*binding.FileStore, string) {
	t.Helper()
	root := copyFixtureBundle(t, name)
	return binding.NewFileStore(filepath.Join(root, "bindings")), root
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return data
}

func TestListResolvesAliasesAndLiteralsAndQuoting(t *testing.T) {
	s, _ := newFixtureStore(t, "fixture")

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []binding.Binding{
		{Name: "aliased", Profile: "planner", Scope: "~/dev/home"},
		{Name: "aliastool", Profile: "architect", Scope: "~/dev/projects/tool"},
		{Name: "literal", Profile: "engineer", Scope: "~/dev/projects/literal"},
		{Name: "quoted", Profile: "odd name", Scope: "~/dev/has space"},
	}
	if len(got) != len(want) {
		t.Fatalf("List returned %d bindings; want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List()[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}

	// README.md sitting alongside the *.yaml files must never be treated as
	// a binding.
	for _, b := range got {
		if b.Name == "README" {
			t.Errorf("README.md was surfaced as a binding: %+v", b)
		}
	}

	// The whole point of read-time resolution: nothing in what List hands
	// back may equal a raw scopes.yaml alias key.
	aliasKeys := map[string]bool{"home": true, "tool": true}
	for _, b := range got {
		if aliasKeys[b.Scope] {
			t.Errorf("binding %q surfaced an alias key as its scope: %q", b.Name, b.Scope)
		}
		if !strings.HasPrefix(b.Scope, "~/") {
			t.Errorf("binding %q scope %q does not look like a path", b.Name, b.Scope)
		}
	}
}

func TestGet(t *testing.T) {
	s, _ := newFixtureStore(t, "fixture")

	got, err := s.Get("aliased")
	if err != nil {
		t.Fatalf("Get(aliased): %v", err)
	}
	want := binding.Binding{Name: "aliased", Profile: "planner", Scope: "~/dev/home"}
	if got != want {
		t.Errorf("Get(aliased) = %+v; want %+v", got, want)
	}

	if _, err := s.Get("nope"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Get(nope) error = %v; want ErrNotFound", err)
	}
}

func TestGetIsUnaffectedByAnUnrelatedCorruptFile(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	if err := os.WriteFile(filepath.Join(root, "bindings", "broken.yaml"), []byte("not a binding at all\n"), 0o644); err != nil {
		t.Fatalf("writing a corrupt sibling file: %v", err)
	}

	// The corrupt sibling must fail List (see TestListFailsOnAnyUnparseableFile)
	// but must NOT stop Get from returning a perfectly good, unrelated entry.
	got, err := s.Get("aliased")
	if err != nil {
		t.Fatalf("Get(aliased) with an unrelated corrupt sibling present: %v", err)
	}
	want := binding.Binding{Name: "aliased", Profile: "planner", Scope: "~/dev/home"}
	if got != want {
		t.Errorf("Get(aliased) = %+v; want %+v", got, want)
	}
}

func TestListFailsOnAnyUnparseableFile(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	if err := os.WriteFile(filepath.Join(root, "bindings", "broken.yaml"), []byte("not a binding at all\n"), 0o644); err != nil {
		t.Fatalf("writing a corrupt sibling file: %v", err)
	}

	if _, err := s.List(); err == nil {
		t.Fatal("List succeeded with an unparseable *.yaml file present under bindings/; want an error")
	}
}

func TestCreateWritesAWholeNewFile(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")

	nb := binding.Binding{Name: "new-one", Profile: "engineer", Scope: "~/dev/projects/new"}
	if err := s.Create(nb); err != nil {
		t.Fatalf("Create: %v", err)
	}

	path := filepath.Join(root, "bindings", "new-one.yaml")
	data := mustRead(t, path)
	if string(data) != "profile: engineer\nscope: ~/dev/projects/new\n" {
		t.Errorf("new file content = %q", data)
	}

	got, err := s.Get("new-one")
	if err != nil {
		t.Fatalf("Get(new-one) after Create: %v", err)
	}
	if got != nb {
		t.Errorf("Get(new-one) = %+v; want %+v", got, nb)
	}

	// Every other file in the directory is untouched.
	if string(mustRead(t, filepath.Join(root, "bindings", "aliased.yaml"))) !=
		"# A leading comment describing this particular binding -- must survive an\n# edit through this package.\nprofile: planner\nscope: home\n" {
		t.Errorf("an unrelated file changed as a side effect of Create")
	}
}

func TestCreateDuplicateNameRejectedFileUnchanged(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	path := filepath.Join(root, "bindings", "aliased.yaml")
	before := mustRead(t, path)

	err := s.Create(binding.Binding{Name: "aliased", Profile: "x", Scope: "~/dev/x"})
	if !errors.Is(err, binding.ErrExists) {
		t.Fatalf("Create(duplicate) error = %v; want ErrExists", err)
	}
	if after := mustRead(t, path); string(after) != string(before) {
		t.Errorf("file changed despite a rejected Create")
	}
}

func TestCreateWithAliasAsScopeRejected(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")

	// "home" is a scopes.yaml key in the fixture, not a path. The interface
	// must refuse to let a new binding be saved carrying an alias.
	err := s.Create(binding.Binding{Name: "bad", Profile: "engineer", Scope: "home"})
	if err == nil {
		t.Fatal("Create with an alias-shaped scope succeeded; want an error")
	}
	if _, statErr := os.Stat(filepath.Join(root, "bindings", "bad.yaml")); statErr == nil {
		t.Errorf("a file was written despite a rejected Create")
	}
}

func TestUpdateNoOpTouchesNothing(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	path := filepath.Join(root, "bindings", "aliased.yaml")
	before := mustRead(t, path)

	current, err := s.Get("aliased")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := s.Update(current); err != nil {
		t.Fatalf("Update(no-op): %v", err)
	}
	after := mustRead(t, path)
	if string(after) != string(before) {
		t.Fatalf("a no-op Update changed the file\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestUpdateProfileOnlyPreservesLeadingCommentAndScopeToken(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	path := filepath.Join(root, "bindings", "aliased.yaml")

	err := s.Update(binding.Binding{Name: "aliased", Profile: "architect", Scope: "~/dev/home"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	after := string(mustRead(t, path))
	// The leading comment block must survive byte-for-byte.
	if !strings.HasPrefix(after, "# A leading comment describing this particular binding -- must survive an\n# edit through this package.\n") {
		t.Errorf("leading comment did not survive the update:\n%s", after)
	}
	// The raw alias token "home" must survive: only the profile value
	// changed, and this package never rewrites an untouched scope even when
	// it is an alias.
	if !strings.Contains(after, "scope: home") {
		t.Errorf("profile-only update did not preserve the raw scope token:\n%s", after)
	}
	if strings.Contains(after, "profile: planner") {
		t.Errorf("old profile value still present after update:\n%s", after)
	}
	if !strings.Contains(after, "profile: architect") {
		t.Errorf("new profile value not written:\n%s", after)
	}

	got, err := s.Get("aliased")
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	want := binding.Binding{Name: "aliased", Profile: "architect", Scope: "~/dev/home"}
	if got != want {
		t.Errorf("Get(aliased) after Update = %+v; want %+v", got, want)
	}
}

func TestUpdateScopeWritesLiteralPathNotAlias(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	path := filepath.Join(root, "bindings", "aliased.yaml")

	err := s.Update(binding.Binding{Name: "aliased", Profile: "planner", Scope: "~/dev/elsewhere"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	after := string(mustRead(t, path))
	if !strings.Contains(after, "scope: ~/dev/elsewhere") {
		t.Errorf("new scope path not written:\n%s", after)
	}
	if strings.Contains(after, "scope: home") {
		t.Errorf("old alias token still present after scope was explicitly changed:\n%s", after)
	}
	if !strings.Contains(after, "profile: planner") {
		t.Errorf("unrelated profile value was rewritten:\n%s", after)
	}
}

func TestUpdateScopeToAliasKeyRejected(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	path := filepath.Join(root, "bindings", "literal.yaml")
	before := mustRead(t, path)

	err := s.Update(binding.Binding{Name: "literal", Profile: "engineer", Scope: "tool"})
	if err == nil {
		t.Fatal("Update with an alias-shaped scope succeeded; want an error")
	}
	if after := mustRead(t, path); string(after) != string(before) {
		t.Errorf("file changed despite a rejected Update")
	}
}

func TestUpdateUnknownNotFound(t *testing.T) {
	s, _ := newFixtureStore(t, "fixture")
	err := s.Update(binding.Binding{Name: "nope", Profile: "x", Scope: "~/dev/x"})
	if !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Update(nope) error = %v; want ErrNotFound", err)
	}
}

func TestDeleteRemovesOnlyThatFile(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")

	if err := s.Delete("literal"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "bindings", "literal.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("literal.yaml still exists after Delete: %v", err)
	}
	if _, err := s.Get("literal"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Get(literal) after Delete = %v; want ErrNotFound", err)
	}

	// Every other file is untouched.
	for _, name := range []string{"aliased.yaml", "quoted.yaml", "aliastool.yaml"} {
		if _, err := os.Stat(filepath.Join(root, "bindings", name)); err != nil {
			t.Errorf("Delete(literal) affected an unrelated file %s: %v", name, err)
		}
	}
}

func TestDeleteUnknownNotFound(t *testing.T) {
	s, _ := newFixtureStore(t, "fixture")
	if err := s.Delete("nope"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Delete(nope) error = %v; want ErrNotFound", err)
	}
}

// TestCreateThenDeleteLeavesNoTrace is the same property the live-bundle
// acceptance check depends on: creating one new binding and deleting it
// right back through this package's own Delete must leave no file behind
// and every existing file exactly as it was. This is what makes it safe to
// prove CRUD against the real bundle by creating a throwaway entry and
// cleaning it up through the same interface, rather than trusting eyeballs.
func TestCreateThenDeleteLeavesNoTrace(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	before := treeFingerprint(t, filepath.Join(root, "bindings"))

	nb := binding.Binding{Name: "roundtrip-probe", Profile: "engineer", Scope: "~/dev/probe"}
	if err := s.Create(nb); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(nb.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	after := treeFingerprint(t, filepath.Join(root, "bindings"))
	if before != after {
		t.Fatalf("create+delete did not restore the original directory contents\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestCreateOnMissingDirBringsItIntoExistence(t *testing.T) {
	root := t.TempDir()
	s := binding.NewFileStore(filepath.Join(root, "bindings"))

	nb := binding.Binding{Name: "first", Profile: "engineer", Scope: "~/dev/first"}
	if err := s.Create(nb); err != nil {
		t.Fatalf("Create on a missing bindings/ dir: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0] != nb {
		t.Fatalf("List = %+v; want exactly [%+v]", got, nb)
	}
}

func TestListOnMissingDirReturnsErrBindingsDirMissing(t *testing.T) {
	s := binding.NewFileStore(filepath.Join(t.TempDir(), "bindings"))
	_, err := s.List()
	if !errors.Is(err, binding.ErrBindingsDirMissing) {
		t.Fatalf("List on a missing bindings/ dir error = %v; want ErrBindingsDirMissing", err)
	}
}

func TestGetOnMissingDirReturnsErrBindingsDirMissing(t *testing.T) {
	s := binding.NewFileStore(filepath.Join(t.TempDir(), "bindings"))
	_, err := s.Get("anything")
	if !errors.Is(err, binding.ErrBindingsDirMissing) {
		t.Fatalf("Get on a missing bindings/ dir error = %v; want ErrBindingsDirMissing", err)
	}
}

func TestListOnPresentButEmptyDirIsEmptyNotError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bindings"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s := binding.NewFileStore(filepath.Join(root, "bindings"))
	got, err := s.List()
	if err != nil {
		t.Fatalf("List on a present, empty bindings/ dir: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %+v; want empty", got)
	}
}

func TestListOnBindingsDirThatIsActuallyAFileFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bindings"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := binding.NewFileStore(filepath.Join(root, "bindings"))
	_, err := s.List()
	if err == nil {
		t.Fatal("List succeeded when bindings/ is actually a plain file; want an error")
	}
	if errors.Is(err, binding.ErrBindingsDirMissing) {
		t.Errorf("List error = %v; want a distinct 'unreadable' error, not ErrBindingsDirMissing", err)
	}
}

func TestUpdateReplacesQuotedValueSpanCorrectly(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	path := filepath.Join(root, "bindings", "quoted.yaml")

	// "quoted" starts as profile: 'odd name' / scope: "~/dev/has space".
	// Changing only Profile must replace the whole quoted token — including
	// its quotes — with the new value, and must not disturb the
	// double-quoted scope on the line below.
	err := s.Update(binding.Binding{Name: "quoted", Profile: "plain", Scope: "~/dev/has space"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	after := string(mustRead(t, path))
	if !strings.Contains(after, "profile: plain\n") {
		t.Errorf("quoted profile value was not cleanly replaced:\n%s", after)
	}
	if strings.Contains(after, "odd name") {
		t.Errorf("old quoted profile value still present:\n%s", after)
	}
	if !strings.Contains(after, `scope: "~/dev/has space"`) {
		t.Errorf("unrelated quoted scope value was disturbed:\n%s", after)
	}

	got, err := s.Get("quoted")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := binding.Binding{Name: "quoted", Profile: "plain", Scope: "~/dev/has space"}
	if got != want {
		t.Errorf("Get(quoted) = %+v; want %+v", got, want)
	}
}

func TestUpdateToValueNeedingQuotesIsQuoted(t *testing.T) {
	s, root := newFixtureStore(t, "fixture")
	path := filepath.Join(root, "bindings", "literal.yaml")

	// A profile value containing a comma is not safe as a plain scalar; it
	// must be single-quoted on write, or a later read would decode it
	// wrong.
	err := s.Update(binding.Binding{Name: "literal", Profile: "a, b", Scope: "~/dev/projects/literal"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	after := string(mustRead(t, path))
	if !strings.Contains(after, "profile: 'a, b'\n") {
		t.Errorf("value needing quotes was not quoted on write:\n%s", after)
	}

	got, err := s.Get("literal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Profile != "a, b" {
		t.Errorf("Get(literal).Profile = %q; want %q", got.Profile, "a, b")
	}
}

func TestValidateRejectsBadNames(t *testing.T) {
	bad := []binding.Binding{
		{Name: "", Profile: "p", Scope: "~/s"},
		{Name: "has space", Profile: "p", Scope: "~/s"},
		{Name: "colon:name", Profile: "p", Scope: "~/s"},
		{Name: "ok", Profile: "", Scope: "~/s"},
		{Name: "ok", Profile: "p", Scope: ""},
	}
	for _, b := range bad {
		if err := b.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil; want an error", b)
		}
	}
}

func TestGetRejectsAPathEscapingName(t *testing.T) {
	s, _ := newFixtureStore(t, "fixture")
	for _, name := range []string{"../escape", "a/b", "..", "."} {
		if _, err := s.Get(name); !errors.Is(err, binding.ErrNotFound) {
			t.Errorf("Get(%q) error = %v; want ErrNotFound (unsafe names must never touch the filesystem)", name, err)
		}
	}
}

// TestLiveShapeFixtureNeverLeaksAnAlias runs List against a frozen,
// byte-for-byte copy of the real ~/dev/projects/agent-setup bundle's
// bindings/ directory and scopes.yaml (testdata/live-shape/ — copied once,
// not read from the live bundle at test time) so this exact regression is
// caught on every `go test`, not only when TACHYON_LIVE_BUNDLE=1 is set.
// Every one of the eight live entries has an alias for its scope today;
// this is the test that would fail if that alias ever leaked through
// unresolved.
func TestLiveShapeFixtureNeverLeaksAnAlias(t *testing.T) {
	s := binding.NewFileStore(filepath.Join("testdata", "live-shape", "bindings"))
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("List returned %d bindings; want 8: %+v", len(got), got)
	}

	wantScope := map[string]string{
		"chrispian":   "~/dev/chrispian",
		"nanite":      "~/dev/hollis-labs/apps/nanite",
		"conductor":   "~/dev/chrispian",
		"eng-cairn":   "~/dev/projects/cairn",
		"eng-setup":   "~/dev/projects/agent-setup",
		"eng-nanite":  "~/dev/hollis-labs/apps/nanite",
		"orch-nanite": "~/dev/hollis-labs/apps/nanite",
		"dir-nanite":  "~/dev/hollis-labs/apps/nanite",
	}
	rawAliasKeys := map[string]bool{
		"chrispian": true, "cairn": true, "agent-setup": true,
		"nanite": true, "tesseract": true, "torque": true,
	}
	seen := map[string]bool{}
	for _, b := range got {
		seen[b.Name] = true
		want, ok := wantScope[b.Name]
		if !ok {
			t.Errorf("unexpected binding %q in the live-shape fixture", b.Name)
			continue
		}
		if b.Scope != want {
			t.Errorf("binding %q scope = %q; want %q", b.Name, b.Scope, want)
		}
		if rawAliasKeys[b.Scope] && b.Scope != want {
			t.Errorf("binding %q surfaced a raw alias key as its scope: %q", b.Name, b.Scope)
		}
	}
	for name := range wantScope {
		if !seen[name] {
			t.Errorf("expected binding %q not present in List()", name)
		}
	}
}

// treeFingerprint hashes every file's name and content under dir, so a test
// can confirm a directory returned to its exact original contents without
// hand-listing every file itself.
func treeFingerprint(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var parts []string
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", e.Name(), err)
		}
		parts = append(parts, e.Name()+"\x00"+string(data))
	}
	return strings.Join(parts, "\x01")
}
