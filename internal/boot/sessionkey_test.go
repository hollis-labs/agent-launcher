package boot_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// TestEveryAxisGetsItsOwnSegment is the defect the layout exists to close,
// and the shape it closes it with.
//
// A saved binding used to BE the boot target, so cairn's own
// <boot-root>/<target>/<session> layout gave every binding its own
// directory. Launch profiles ended that: the target is now always the bare
// agent profile, so `engineer` under two launch profiles, or in two
// projects, is one target three times over. All three would plant in one
// directory, replanted out from under whichever session got there first.
//
// The answer is one segment per axis —
// <boot-root>/<project>/<profile>/<launch profile> — so no two of them can
// collapse into each other. Both derived segments are checked because each
// fails differently and both fail silently: a shared launch profile hands a
// Claude session a Codex tree, and a shared project hands a running session
// a settings document granting the other project's path.
func TestEveryAxisGetsItsOwnSegment(t *testing.T) {
	scopeA := "/Users/somebody/dev/projects/tachyon"
	scopeB := "/Users/somebody/dev/projects/cairn"

	if boot.ProjectKey(scopeA) == boot.ProjectKey(scopeB) {
		t.Error("two scopes share a project segment; a running session's grant would name the other project")
	}
	if boot.SessionKey("default") == boot.SessionKey("codex") {
		t.Error("two launch profiles share a session segment; the second launch would replant over the first")
	}
}

// TestProjectKeyDisambiguatesASharedBasename is the case the digest exists
// for, and the reason the segment is not just the basename: two projects can
// end in the same word, and sharing a boot directory is exactly the failure
// above.
func TestProjectKeyDisambiguatesASharedBasename(t *testing.T) {
	a := boot.ProjectKey("/Users/somebody/dev/projects/frag")
	b := boot.ProjectKey("/Users/somebody/dev/other/frag")
	if a == b {
		t.Fatalf("two different scopes ending in %q share the project key %q", "frag", a)
	}
	// Both still READ as the project, which is the point of the segment.
	for _, got := range []string{a, b} {
		if !strings.HasPrefix(got, "frag-") {
			t.Errorf("ProjectKey = %q; want it to open with the project's own name", got)
		}
	}
}

// TestProjectKeyIsShortEnoughToRead is the complaint that produced this
// shape. The previous key was one segment carrying a slug of the whole scope
// plus a full 64-character SHA-256 — 105 characters for an ordinary project,
// in a directory a person browses.
func TestProjectKeyIsShortEnoughToRead(t *testing.T) {
	got := boot.ProjectKey("/Users/somebody/dev/hollis-labs/apps/fragments-engine")
	if len(got) > 40 {
		t.Errorf("ProjectKey = %q (%d chars); the segment is read by a person, not only by a program", got, len(got))
	}
	if !strings.HasPrefix(got, "fragments-engine-") {
		t.Errorf("ProjectKey = %q; want the project's own name in front", got)
	}
}

// TestSessionKeyReadsAsTheLaunchProfile: a launch profile's name is already
// a safe path segment, so the leaf should be the name itself rather than a
// hash of it. This is what makes the whole path readable end to end —
// boot/cairn-<digest>/engineer/codex.
func TestSessionKeyReadsAsTheLaunchProfile(t *testing.T) {
	for _, name := range []string{"default", "codex", "eng-nanite", "a.b_c-1"} {
		if got := boot.SessionKey(name); got != name {
			t.Errorf("SessionKey(%q) = %q; a safe name must pass through unchanged", name, got)
		}
	}
}

// TestKeysAreStable is the property ~/.claude.json's per-path trust entries
// depend on: the same composition must yield the same directory forever, or
// every launch adds an entry (7,129 measured 2026-09-02, 7,070 of them
// already dead) and the stable path buys nothing.
func TestKeysAreStable(t *testing.T) {
	const scope = "/some/scope"
	project, session := boot.ProjectKey(scope), boot.SessionKey("default")
	for i := 0; i < 5; i++ {
		if got := boot.ProjectKey(scope); got != project {
			t.Fatalf("ProjectKey is not deterministic: %q then %q", project, got)
		}
		if got := boot.SessionKey("default"); got != session {
			t.Fatalf("SessionKey is not deterministic: %q then %q", session, got)
		}
	}
}

// TestKeysAreAlwaysSafeSegments: both are joined onto a path, and one of
// them is derived from a scope — an arbitrary absolute path.
func TestKeysAreAlwaysSafeSegments(t *testing.T) {
	for _, scope := range []string{
		"", "/", "..", "../../../etc", "/Users/somebody/dev/projects/tachyon",
		"/tmp/scope with spaces/and'quotes", "/tmp/....",
	} {
		assertSafeSegment(t, "ProjectKey", scope, boot.ProjectKey(scope))
	}
	for _, name := range []string{"", "default", "..", "a/b"} {
		assertSafeSegment(t, "SessionKey", name, boot.SessionKey(name))
	}
}

func assertSafeSegment(t *testing.T, fn, in, got string) {
	t.Helper()
	if got == "" || got == "." || got == ".." {
		t.Errorf("%s(%q) = %q, which is not a usable segment", fn, in, got)
	}
	if strings.ContainsRune(got, os.PathSeparator) || strings.ContainsRune(got, '/') {
		t.Errorf("%s(%q) = %q, which contains a path separator", fn, in, got)
	}
	if strings.HasPrefix(got, boot.PrevPrefix) {
		t.Errorf("%s(%q) = %q, which a sweep would mistake for a moved-aside directory", fn, in, got)
	}
}

// TestNoScopeIsNamedRatherThanEmpty: an empty first segment would collapse
// <root>/<project>/<profile> to <root>/<profile>, putting an unscoped launch
// somewhere a project named after a profile could later land.
func TestNoScopeIsNamedRatherThanEmpty(t *testing.T) {
	got := boot.ProjectKey("")
	if got == "" {
		t.Fatal("ProjectKey(\"\") is empty; the segment must be spelled")
	}
	if got == boot.ProjectKey("/some/real/scope") {
		t.Fatal("an unscoped launch shares a project segment with a scoped one")
	}
}

// TestPrepareSeparatesCompositionsOnDisk is the same property one level
// down, against real directories: two compositions must not clear each
// other's boot directory.
func TestPrepareSeparatesCompositionsOnDisk(t *testing.T) {
	root := filepath.Join(t.TempDir(), boot.ProjectKey("/scope/a"))
	key := boot.Key("engineer")

	planA, err := boot.Prepare(root, key, boot.SessionKey("default"))
	if err != nil {
		t.Fatalf("Prepare(A): %v", err)
	}
	if err := os.MkdirAll(planA.Current, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(planA.Current, "marker")
	if err := os.WriteFile(marker, []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}

	planB, err := boot.Prepare(root, key, boot.SessionKey("codex"))
	if err != nil {
		t.Fatalf("Prepare(B): %v", err)
	}
	if planB.Current == planA.Current {
		t.Fatalf("two launch profiles prepared the same directory: %q", planB.Current)
	}
	if planB.Moved() {
		t.Errorf("preparing B moved something aside (%q); it must not have touched A's directory", planB.MovedAside)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "A" {
		t.Fatalf("A's boot directory did not survive B's Prepare: (%q, %v)", got, err)
	}
}

// TestPrepareRefusesAPrevPrefixedSession: a session named ".prev-anything"
// would be swept as though it were a moved-aside directory — deleted, by the
// one code path in this package allowed to delete.
func TestPrepareRefusesAPrevPrefixedSession(t *testing.T) {
	root := t.TempDir()
	if _, err := boot.Prepare(root, boot.Key("engineer"), boot.PrevPrefix+"20260910"); err == nil {
		t.Fatal("Prepare accepted a session segment named like a moved-aside directory")
	}
}

// TestPrepareRefusesATraversingSession: the session now carries
// caller-derived content, so it is exactly the value that must never spell
// "..".
func TestPrepareRefusesATraversingSession(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"..", ".", "a/b", "../escape"} {
		if _, err := boot.Prepare(root, boot.Key("engineer"), bad); err == nil {
			t.Errorf("Prepare accepted session %q", bad)
		}
	}
}

// TestPrepareEmptySessionIsTheDefault keeps every existing caller's behavior
// intact: no session supplied means the segment everything used before.
func TestPrepareEmptySessionIsTheDefault(t *testing.T) {
	root := t.TempDir()
	plan, err := boot.Prepare(root, boot.Key("engineer"), "")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if filepath.Base(plan.Current) != boot.DefaultSession {
		t.Fatalf("Prepare with no session planted at %q; want a %q leaf", plan.Current, boot.DefaultSession)
	}
}

// TestProjectRootKeepsAnEmptyBootRootEmpty is D9's guard, at the one place
// the project segment is joined on.
//
// filepath.Join("", "cairn-abc") is "cairn-abc" — a RELATIVE path, which
// compose.Build would accept and which resolves against whatever the
// process's cwd happens to be. An empty boot root has to stay empty so
// compose.ErrNoBootRoot still fires: a boot root a caller never chose is
// exactly the hazard, and a silent relative one is the worst shape of it.
func TestProjectRootKeepsAnEmptyBootRootEmpty(t *testing.T) {
	if got := boot.ProjectRoot("", "/some/scope"); got != "" {
		t.Fatalf("ProjectRoot(\"\", scope) = %q; want \"\" so the caller's own refusal still fires", got)
	}
	if got := boot.ProjectRoot("", ""); got != "" {
		t.Fatalf("ProjectRoot(\"\", \"\") = %q; want \"\"", got)
	}
}

// TestProjectRootAppendsExactlyOneSegment: cairn appends <target>/<session>
// to whatever it is handed, so this must contribute the project and nothing
// more, or the tree grows a level nobody asked for.
func TestProjectRootAppendsExactlyOneSegment(t *testing.T) {
	const bootRoot = "/state/tachyon/boot"
	got := boot.ProjectRoot(bootRoot, "/Users/somebody/dev/projects/cairn")

	rel, err := filepath.Rel(bootRoot, got)
	if err != nil {
		t.Fatalf("ProjectRoot returned %q, which is not under %q: %v", got, bootRoot, err)
	}
	if strings.ContainsRune(rel, os.PathSeparator) {
		t.Fatalf("ProjectRoot added %q; want exactly one segment", rel)
	}
	if rel != boot.ProjectKey("/Users/somebody/dev/projects/cairn") {
		t.Fatalf("ProjectRoot added %q; want ProjectKey's own value", rel)
	}
}
