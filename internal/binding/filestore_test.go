package binding_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
)

// copyFixture copies testdata/name into a fresh temp file and returns its
// path, so every test that writes gets its own scratch copy — testdata/ is
// never mutated by a test run.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("writing scratch copy of %s: %v", name, err)
	}
	return dst
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
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []binding.Binding{
		{Name: "aliased", Profile: "planner", Scope: "~/dev/home"},
		{Name: "literal", Profile: "engineer", Scope: "~/dev/projects/literal"},
		{Name: "quoted", Profile: "odd name", Scope: "~/dev/has space"},
		{Name: "trailing", Profile: "architect", Scope: "~/dev/projects/tool"},
	}
	if len(got) != len(want) {
		t.Fatalf("List returned %d bindings; want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List()[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}

	// The whole point of read-time resolution: nothing in what List hands
	// back may equal a raw scopes: alias key.
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
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)

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

func TestCreateAppendsSurgically(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	before := mustRead(t, path)
	s := binding.NewFileStore(path)

	nb := binding.Binding{Name: "new-one", Profile: "engineer", Scope: "~/dev/projects/new"}
	if err := s.Create(nb); err != nil {
		t.Fatalf("Create: %v", err)
	}

	after := mustRead(t, path)
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatalf("Create did not simply append: existing bytes changed\nbefore:\n%s\nafter:\n%s", before, after)
	}
	appended := strings.TrimPrefix(string(after), string(before))
	if appended != "  new-one: { profile: engineer, scope: ~/dev/projects/new }\n" {
		t.Errorf("appended text = %q", appended)
	}

	got, err := s.Get("new-one")
	if err != nil {
		t.Fatalf("Get(new-one) after Create: %v", err)
	}
	if got != nb {
		t.Errorf("Get(new-one) = %+v; want %+v", got, nb)
	}
}

func TestCreateDuplicateNameRejectedFileUnchanged(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	before := mustRead(t, path)
	s := binding.NewFileStore(path)

	err := s.Create(binding.Binding{Name: "aliased", Profile: "x", Scope: "~/dev/x"})
	if !errors.Is(err, binding.ErrExists) {
		t.Fatalf("Create(duplicate) error = %v; want ErrExists", err)
	}
	if after := mustRead(t, path); string(after) != string(before) {
		t.Errorf("file changed despite a rejected Create")
	}
}

func TestCreateWithAliasAsScopeRejected(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	before := mustRead(t, path)
	s := binding.NewFileStore(path)

	// "home" is a scopes: key in the fixture, not a path. The interface must
	// refuse to let a new binding be saved carrying an alias.
	err := s.Create(binding.Binding{Name: "bad", Profile: "engineer", Scope: "home"})
	if err == nil {
		t.Fatal("Create with an alias-shaped scope succeeded; want an error")
	}
	if after := mustRead(t, path); string(after) != string(before) {
		t.Errorf("file changed despite a rejected Create")
	}
}

func TestUpdateNoOpTouchesNothing(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	before := mustRead(t, path)
	s := binding.NewFileStore(path)

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

func TestUpdateProfileOnlyLeavesScopeTokenAlone(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)

	err := s.Update(binding.Binding{Name: "aliased", Profile: "architect", Scope: "~/dev/home"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	after := string(mustRead(t, path))
	// The raw alias token "home" must survive: only the profile value
	// changed, and this package never rewrites an untouched scope even when
	// it is an alias.
	if !strings.Contains(after, "aliased:   { profile: architect,  scope: home }") {
		t.Errorf("profile-only update did not preserve the scope token / line shape:\n%s", after)
	}
	if strings.Contains(after, "profile: planner") {
		t.Errorf("old profile value still present after update:\n%s", after)
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
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)

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
	// profile field's own spacing untouched.
	if !strings.Contains(after, "aliased:   { profile: planner,") {
		t.Errorf("unrelated part of the line was rewritten:\n%s", after)
	}
}

func TestUpdateScopeToAliasKeyRejected(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	before := mustRead(t, path)
	s := binding.NewFileStore(path)

	err := s.Update(binding.Binding{Name: "literal", Profile: "engineer", Scope: "tool"})
	if err == nil {
		t.Fatal("Update with an alias-shaped scope succeeded; want an error")
	}
	if after := mustRead(t, path); string(after) != string(before) {
		t.Errorf("file changed despite a rejected Update")
	}
}

func TestUpdateUnknownNotFound(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)
	err := s.Update(binding.Binding{Name: "nope", Profile: "x", Scope: "~/dev/x"})
	if !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Update(nope) error = %v; want ErrNotFound", err)
	}
}

func TestDeleteRemovesOnlyThatLine(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	before := string(mustRead(t, path))
	s := binding.NewFileStore(path)

	if err := s.Delete("literal"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	after := string(mustRead(t, path))

	wantLine := "  literal:   { profile: engineer, scope: ~/dev/projects/literal }\n"
	if !strings.Contains(before, wantLine) {
		t.Fatalf("test fixture assumption wrong: line not found verbatim in the original file")
	}
	if want := strings.Replace(before, wantLine, "", 1); after != want {
		t.Fatalf("Delete changed more than the target line.\nwant:\n%s\ngot:\n%s", want, after)
	}

	if _, err := s.Get("literal"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Get(literal) after Delete = %v; want ErrNotFound", err)
	}
	// The comment between "literal" and "quoted" must survive untouched.
	if !strings.Contains(after, "# A comment between entries.") {
		t.Errorf("an unrelated comment was lost by Delete:\n%s", after)
	}
}

func TestDeleteUnknownNotFound(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)
	if err := s.Delete("nope"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Delete(nope) error = %v; want ErrNotFound", err)
	}
}

// TestCreateThenDeleteIsExactInverse is the same property the live-bundle
// acceptance check depends on: creating one new binding and deleting it right
// back through this package's own Delete must return the file to its
// original bytes exactly. This is what makes it safe to prove CRUD against
// the real bundle by creating a throwaway entry and cleaning it up through
// the same interface, rather than trusting eyeballs.
func TestCreateThenDeleteIsExactInverse(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	before := mustRead(t, path)
	s := binding.NewFileStore(path)

	nb := binding.Binding{Name: "roundtrip-probe", Profile: "engineer", Scope: "~/dev/probe"}
	if err := s.Create(nb); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(nb.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	after := mustRead(t, path)
	if string(after) != string(before) {
		t.Fatalf("create+delete did not restore the original bytes\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCreateOnMissingFileSynthesizesSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bindings.yaml")
	s := binding.NewFileStore(path)

	nb := binding.Binding{Name: "first", Profile: "engineer", Scope: "~/dev/first"}
	if err := s.Create(nb); err != nil {
		t.Fatalf("Create on a missing file: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0] != nb {
		t.Fatalf("List = %+v; want exactly [%+v]", got, nb)
	}
}

func TestListOnMissingFileIsEmptyNotError(t *testing.T) {
	s := binding.NewFileStore(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	got, err := s.List()
	if err != nil {
		t.Fatalf("List on a missing file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List on a missing file = %+v; want empty", got)
	}
}

func TestUpdateReplacesQuotedValueSpanCorrectly(t *testing.T) {
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)

	// "quoted" starts as { profile: 'odd name', scope: "~/dev/has space" }.
	// Changing only Profile must replace the whole quoted token — including
	// its quotes — with the new value, and must not disturb the
	// double-quoted scope next to it.
	err := s.Update(binding.Binding{Name: "quoted", Profile: "plain", Scope: "~/dev/has space"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	after := string(mustRead(t, path))
	if !strings.Contains(after, `quoted:    { profile: plain, scope: "~/dev/has space" }`) {
		t.Errorf("quoted profile value was not cleanly replaced:\n%s", after)
	}
	if strings.Contains(after, "odd name") {
		t.Errorf("old quoted profile value still present:\n%s", after)
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
	path := copyFixture(t, "bindings.yaml")
	s := binding.NewFileStore(path)

	// A profile value containing a comma is not safe as a plain scalar next
	// to other flow-map fields; it must be single-quoted on write, or the
	// file would no longer parse as the shape this package expects.
	err := s.Update(binding.Binding{Name: "literal", Profile: "a, b", Scope: "~/dev/projects/literal"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	after := string(mustRead(t, path))
	if !strings.Contains(after, `profile: 'a, b',`) {
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

// TestLiveShapeFixtureNeverLeaksAnAlias runs List against a frozen,
// byte-for-byte copy of the real ~/dev/projects/agent-setup/bindings.yaml
// (testdata/live-shape.yaml — copied once, not read from the live bundle at
// test time) so this exact regression is caught on every `go test`, not only
// when TACHYON_LIVE_BUNDLE=1 is set. Every one of the eight live entries has
// an alias for its scope today; this is the test that would fail if that
// alias ever leaked through unresolved.
func TestLiveShapeFixtureNeverLeaksAnAlias(t *testing.T) {
	s := binding.NewFileStore(filepath.Join("testdata", "live-shape.yaml"))
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
			// b.Scope equalling a raw alias key would only be correct if
			// that also happens to be the resolved path, which none of
			// these are.
			t.Errorf("binding %q surfaced a raw alias key as its scope: %q", b.Name, b.Scope)
		}
	}
	for name := range wantScope {
		if !seen[name] {
			t.Errorf("expected binding %q not present in List()", name)
		}
	}
}
