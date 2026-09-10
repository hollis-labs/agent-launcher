package boot_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// --- Key -------------------------------------------------------------------

// TestKey_SafeSeedUnchanged proves the identity case this package's doc
// comment leans on: a seed that is already a single, filesystem-safe path
// segment — the shape internal/binding constrains a Binding.Name to — comes
// back from Key completely unchanged. That is what keeps this package's
// idea of a binding's boot directory in sync with the literal <name>
// segment Cairn plants under when the same string is passed as the boot
// target (target architecture §5).
func TestKey_SafeSeedUnchanged(t *testing.T) {
	seeds := []string{
		"planner",
		"planner-fast",
		"a.b_c-9",
		"9x",
		"A",
		strings.Repeat("x", 60), // longer than maxSlugLen, but already safe
	}
	for _, seed := range seeds {
		if got := boot.Key(seed); got != seed {
			t.Errorf("Key(%q) = %q; want unchanged", seed, got)
		}
	}
}

// TestKey_Stable proves the same seed always yields the same key, across
// repeated calls — the property "launching the same binding twice produces
// the same current path both times" rests on.
func TestKey_Stable(t *testing.T) {
	seeds := []string{"planner", "role/with parts, and: punctuation", "", "..", "üñïçødé seed"}
	for _, seed := range seeds {
		first := boot.Key(seed)
		for i := 0; i < 5; i++ {
			if got := boot.Key(seed); got != first {
				t.Errorf("Key(%q) call %d = %q; want %q (first call)", seed, i, got, first)
			}
		}
	}
}

// TestKey_UnsafeSeedIsFilesystemSafeAndDistinct exercises the path an
// unsaved composition's identity string is expected to take: not already a
// safe single segment. It checks the result is usable as one path segment
// (no separators, not "." or ".."), and — the crux of "two different
// bindings never collide on a key" — that seeds which are deliberately
// engineered to sanitize down to the *same* human-readable slug still
// produce different keys, because the collision guarantee rides on a hash
// of the whole seed, not on the slug.
func TestKey_UnsafeSeedIsFilesystemSafeAndDistinct(t *testing.T) {
	// Every pair below sanitizes to the same slug ("a-b") under the
	// substitution Key uses for an unsafe character, but each seed is a
	// distinct string.
	collidingUnderNaiveSanitizing := []string{
		"a/b",
		"a b",
		"a:b",
		"a;b",
		"a\\b",
		"a\tb",
	}

	seen := map[string]string{}
	for _, seed := range collidingUnderNaiveSanitizing {
		key := boot.Key(seed)

		if key == "." || key == ".." {
			t.Errorf("Key(%q) = %q; must not be a special directory name", seed, key)
		}
		if strings.ContainsAny(key, "/\\") {
			t.Errorf("Key(%q) = %q; must not contain a path separator", seed, key)
		}
		if prior, ok := seen[key]; ok {
			t.Errorf("Key(%q) = %q collides with Key(%q); keys must never collide", seed, key, prior)
		}
		seen[key] = seed
	}

	// Also cover degenerate seeds: empty, and a seed that is entirely
	// separator/dot characters (so a naive sanitizer could hand back "."
	// or "..").
	for _, seed := range []string{"", "..", "/", "...", "   "} {
		key := boot.Key(seed)
		if key == "" {
			t.Errorf("Key(%q) = %q; must not be empty", seed, key)
		}
		if key == "." || key == ".." {
			t.Errorf("Key(%q) = %q; must not be a special directory name", seed, key)
		}
	}
}

// TestKey_DistinctBindingNamesNeverCollide is the binding-level reading of
// "two different bindings never collide on a key": since Key returns an
// already-safe binding name unchanged, and binding names are guaranteed
// distinct by internal/binding's Store, this holds trivially — proved here
// directly rather than merely asserted.
func TestKey_DistinctBindingNamesNeverCollide(t *testing.T) {
	names := []string{"planner", "architect", "director", "planner-fast", "planner_fast"}
	seen := map[string]string{}
	for _, name := range names {
		key := boot.Key(name)
		if prior, ok := seen[key]; ok && prior != name {
			t.Errorf("Key(%q) = %q collides with Key(%q)", name, key, prior)
		}
		seen[key] = name
	}
}

// --- SessionPath -------------------------------------------------------------

func TestSessionPath(t *testing.T) {
	got := boot.SessionPath("/state/boot", "planner", boot.DefaultSession)
	want := filepath.Join("/state/boot", "planner", "current")
	if got != want {
		t.Fatalf("SessionPath = %q; want %q", got, want)
	}
	if filepath.Base(got) != boot.DefaultSession {
		t.Fatalf("SessionPath's last segment = %q; want %q", filepath.Base(got), boot.DefaultSession)
	}
}

// TestCurrentPath_SameBindingTwiceSamePath is the acceptance bullet stated
// directly: launching the same binding twice produces the same current path
// both times.
func TestSessionPath_SameCompositionTwiceSamePath(t *testing.T) {
	root := t.TempDir()
	key := boot.Key("planner")

	first := boot.SessionPath(root, key, boot.DefaultSession)
	second := boot.SessionPath(root, boot.Key("planner"), boot.DefaultSession)
	if first != second {
		t.Fatalf("SessionPath differed across two launches of the same composition: %q vs %q", first, second)
	}
}

// --- Prepare -----------------------------------------------------------------

// plant simulates what T10 does after Prepare clears the path: create the
// directory Prepare returned and drop one marker file in it with the given
// content. This package never plants for real — that is T10's job — so
// tests stand in for it with a plain mkdir + write.
func plant(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("plant: mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("plant: write AGENTS.md in %s: %v", dir, err)
	}
}

func TestPrepare_FirstLaunchNothingToMoveAside(t *testing.T) {
	root := t.TempDir()
	key := boot.Key("planner")

	plan, err := boot.Prepare(root, key, "")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Moved() {
		t.Fatalf("Plan.Moved() = true on a first launch; want false (nothing existed to move aside): %+v", plan)
	}
	if want := boot.SessionPath(root, key, boot.DefaultSession); plan.Current != want {
		t.Fatalf("Plan.Current = %q; want %q", plan.Current, want)
	}
	if _, err := os.Lstat(plan.Current); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat(Current) after Prepare = %v; want ErrNotExist (Prepare must not plant)", err)
	}
	// The key directory itself must exist so a plant can land directly in it.
	if _, err := os.Stat(filepath.Dir(plan.Current)); err != nil {
		t.Fatalf("key directory not created by Prepare: %v", err)
	}
}

// TestPrepare_RelaunchMovesAsideExactlyOnePrev is the acceptance bullet
// stated directly: after a relaunch, exactly one .prev-* exists and current
// is freshly planted.
func TestPrepare_RelaunchMovesAsideExactlyOnePrev(t *testing.T) {
	root := t.TempDir()
	key := boot.Key("planner")

	// Launch 1.
	plan1, err := boot.Prepare(root, key, "")
	if err != nil {
		t.Fatalf("Prepare (launch 1): %v", err)
	}
	plant(t, plan1.Current, "generation 1")

	// Launch 2: a relaunch.
	plan2, err := boot.Prepare(root, key, "")
	if err != nil {
		t.Fatalf("Prepare (launch 2): %v", err)
	}
	if !plan2.Moved() {
		t.Fatalf("Plan.Moved() = false on a relaunch; want true")
	}
	if plan2.Current != plan1.Current {
		t.Fatalf("Current changed across a relaunch: %q -> %q; want the same path", plan1.Current, plan2.Current)
	}
	if _, err := os.Lstat(plan2.Current); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Current exists right after Prepare on relaunch; want it cleared for a fresh plant")
	}
	plant(t, plan2.Current, "generation 2")

	keyDir := filepath.Dir(plan1.Current)
	entries, err := os.ReadDir(keyDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", keyDir, err)
	}
	var prevDirs []string
	var sawCurrent bool
	for _, e := range entries {
		switch {
		case e.Name() == boot.DefaultSession:
			sawCurrent = true
		case strings.HasPrefix(e.Name(), boot.PrevPrefix):
			prevDirs = append(prevDirs, e.Name())
		default:
			t.Errorf("unexpected entry in key directory: %q", e.Name())
		}
	}
	if !sawCurrent {
		t.Errorf("no %q entry in key directory after relaunch and re-plant", boot.DefaultSession)
	}
	if len(prevDirs) != 1 {
		t.Fatalf("key directory has %d .prev-* entries after one relaunch; want exactly 1: %v", len(prevDirs), prevDirs)
	}
	if filepath.Base(plan2.MovedAside) != prevDirs[0] {
		t.Errorf("Plan.MovedAside = %q; does not match the .prev-* entry found on disk %q", plan2.MovedAside, prevDirs[0])
	}

	// The moved-aside directory carries generation 1's content untouched;
	// the fresh current carries generation 2's.
	got1, err := os.ReadFile(filepath.Join(plan2.MovedAside, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md from moved-aside dir: %v", err)
	}
	if string(got1) != "generation 1" {
		t.Errorf("moved-aside AGENTS.md = %q; want %q", got1, "generation 1")
	}
	got2, err := os.ReadFile(filepath.Join(plan2.Current, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md from fresh current: %v", err)
	}
	if string(got2) != "generation 2" {
		t.Errorf("fresh current AGENTS.md = %q; want %q", got2, "generation 2")
	}
}

// TestPrepare_RepeatedCallWithNoPlantIsANoOp proves calling Prepare twice
// with nothing planted in between produces no .prev-* directory — there is
// nothing at Current to move aside the second time.
func TestPrepare_RepeatedCallWithNoPlantIsANoOp(t *testing.T) {
	root := t.TempDir()
	key := boot.Key("planner")

	if _, err := boot.Prepare(root, key, ""); err != nil {
		t.Fatalf("Prepare (1st): %v", err)
	}
	plan, err := boot.Prepare(root, key, "")
	if err != nil {
		t.Fatalf("Prepare (2nd): %v", err)
	}
	if plan.Moved() {
		t.Fatalf("Plan.Moved() = true on a second Prepare with nothing planted in between; want false: %+v", plan)
	}

	keyDir := filepath.Dir(plan.Current)
	entries, err := os.ReadDir(keyDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", keyDir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), boot.PrevPrefix) {
			t.Errorf("unexpected .prev-* entry %q after two Prepare calls with no plant between them", e.Name())
		}
	}
}

// TestPrepare_OpenHandleSurvivesRelaunch is the rename's whole
// justification, made a test rather than a comment: a live session's
// working directory is a handle on the directory's inode, not on its name.
// This test opens both a file handle and a directory handle inside current,
// triggers a relaunch (Prepare, standing in for what T10 does before a
// re-plant), and shows both handles still resolve to exactly the files and
// directory they were opened against — proving a rename, unlike
// os.RemoveAll, cannot pull the ground out from under a running session.
func TestPrepare_OpenHandleSurvivesRelaunch(t *testing.T) {
	root := t.TempDir()
	key := boot.Key("planner")

	plan1, err := boot.Prepare(root, key, "")
	if err != nil {
		t.Fatalf("Prepare (launch 1): %v", err)
	}
	plant(t, plan1.Current, "the session's own AGENTS.md, opened before the relaunch")

	// A live session's cwd is a directory handle. Open one, exactly as a
	// running shell would hold its cwd open.
	dirHandle, err := os.Open(plan1.Current)
	if err != nil {
		t.Fatalf("open directory handle on current: %v", err)
	}
	defer dirHandle.Close()
	dirInfoBefore, err := dirHandle.Stat()
	if err != nil {
		t.Fatalf("stat via directory handle: %v", err)
	}

	// A live session also holds files inside that directory open — its
	// harness's settings file, its AGENTS.md. Open one and keep reading
	// from it across the relaunch below.
	agentsPath := filepath.Join(plan1.Current, "AGENTS.md")
	fileHandle, err := os.Open(agentsPath)
	if err != nil {
		t.Fatalf("open file handle on AGENTS.md: %v", err)
	}
	defer fileHandle.Close()

	// The relaunch: this is the operation under test. If this used
	// os.RemoveAll instead of a rename, both handles above would now point
	// at unlinked, orphaned files.
	plan2, err := boot.Prepare(root, key, "")
	if err != nil {
		t.Fatalf("Prepare (relaunch): %v", err)
	}
	if !plan2.Moved() {
		t.Fatalf("Plan.Moved() = false on the relaunch under test; want true")
	}

	// The file handle, opened before the relaunch and never reopened,
	// still reads the exact bytes it was opened against.
	got, err := io.ReadAll(fileHandle)
	if err != nil {
		t.Fatalf("read from the pre-relaunch file handle: %v", err)
	}
	if string(got) != "the session's own AGENTS.md, opened before the relaunch" {
		t.Fatalf("pre-relaunch file handle now reads %q; the rename must not have changed its content", got)
	}

	// The directory handle still resolves to the exact same inode: Stat
	// through the handle, Stat the path it was renamed to, and compare with
	// os.SameFile -- the only correct way to prove "this handle is still
	// this file" without relying on inode numbers being portable.
	dirInfoAfter, err := dirHandle.Stat()
	if err != nil {
		t.Fatalf("stat via directory handle after relaunch: %v", err)
	}
	if !os.SameFile(dirInfoBefore, dirInfoAfter) {
		t.Fatalf("directory handle's own Stat changed identity across the relaunch")
	}
	movedInfo, err := os.Stat(plan2.MovedAside)
	if err != nil {
		t.Fatalf("stat the moved-aside directory by its new path: %v", err)
	}
	if !os.SameFile(dirInfoBefore, movedInfo) {
		t.Fatalf(
			"the directory handle opened on the original current does not resolve to the same file as "+
				"the .prev-* directory Prepare renamed it to (%s); the handle would have been orphaned",
			plan2.MovedAside,
		)
	}

	// And the handle can still be used as a live cwd would use it: list its
	// own contents by the fd, not by the (now stale) original path.
	names, err := dirHandle.Readdirnames(-1)
	if err != nil {
		t.Fatalf("Readdirnames via the pre-relaunch directory handle: %v", err)
	}
	sort.Strings(names)
	if len(names) != 1 || names[0] != "AGENTS.md" {
		t.Fatalf("directory handle listing after relaunch = %v; want [AGENTS.md]", names)
	}
}

// TestPrepare_TwoBindingsNeverCollide plants two different bindings under
// the same root and confirms neither Prepare call disturbs the other's
// directory — the filesystem-level reading of "two different bindings never
// collide on a key."
func TestPrepare_TwoBindingsNeverCollide(t *testing.T) {
	root := t.TempDir()
	keyA := boot.Key("planner")
	keyB := boot.Key("architect")
	if keyA == keyB {
		t.Fatalf("test setup: Key(planner) == Key(architect) == %q", keyA)
	}

	planA, err := boot.Prepare(root, keyA, "")
	if err != nil {
		t.Fatalf("Prepare(A): %v", err)
	}
	plant(t, planA.Current, "A")

	planB, err := boot.Prepare(root, keyB, "")
	if err != nil {
		t.Fatalf("Prepare(B): %v", err)
	}
	plant(t, planB.Current, "B")

	if planA.Current == planB.Current {
		t.Fatalf("two different bindings resolved to the same Current path: %q", planA.Current)
	}

	gotA, err := os.ReadFile(filepath.Join(planA.Current, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read A's AGENTS.md: %v", err)
	}
	if string(gotA) != "A" {
		t.Fatalf("A's AGENTS.md = %q; want %q -- B's Prepare/plant must not have touched it", gotA, "A")
	}
	gotB, err := os.ReadFile(filepath.Join(planB.Current, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read B's AGENTS.md: %v", err)
	}
	if string(gotB) != "B" {
		t.Fatalf("B's AGENTS.md = %q; want %q", gotB, "B")
	}
}

func TestPrepare_RejectsInvalidKey(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"", ".", "..", "a/b", "a" + string(filepath.Separator) + "b"} {
		if _, err := boot.Prepare(root, key, ""); !errors.Is(err, boot.ErrInvalidKey) {
			t.Errorf("Prepare(root, %q) error = %v; want ErrInvalidKey", key, err)
		}
	}
}

func TestPrepare_RejectsEmptyRoot(t *testing.T) {
	if _, err := boot.Prepare("", boot.Key("planner"), ""); err == nil {
		t.Fatal("Prepare with an empty root returned no error")
	}
}

// --- The delete hazard ------------------------------------------------------

// removeAllAuthorized is the exact, exhaustive set of files in this
// package permitted to reference RemoveAll -- as of T15 (CW-20260903-0019),
// exactly one: sweep.go, which is this package's one deliberate, guarded
// exception to "never delete" -- see this test's own doc comment for why
// that exception belongs here rather than weakening the rule everywhere
// else in the package.
var removeAllAuthorized = map[string]bool{
	"sweep.go": true,
}

// TestPackageNeverCallsRemoveAll is the acceptance bullet "no os.RemoveAll
// (or equivalent) anywhere in this package [outside the one guarded
// exception]" made mechanically checkable rather than merely a claim in a
// commit message. It parses every .go file in this package's directory --
// production and test sources alike, this file included -- and fails if
// the identifier RemoveAll is ever actually referenced in code outside
// [removeAllAuthorized]: as os.RemoveAll(...), as a bare RemoveAll(...)
// after a dot-import, or assigned/passed as a func value.
//
// Until T15 (CW-20260903-0019) this package never deleted anything at
// all -- [Prepare] only ever renames a boot directory aside, and this
// test enforced that with a blanket ban. T15 adds the one place in
// Tachyon that does delete: [Sweep], in sweep.go, and only ever a
// .prev-* directory, and only ever behind the lsof liveness guard and its
// per-run positive control documented on Sweep itself. That is a
// deliberate, reviewed exception, not a loosening of the rule -- this
// test still fails the build the moment RemoveAll (or an equivalent)
// shows up anywhere else in this package, including in boot.go, key.go,
// invoke.go, spawn.go, or a future file nobody added to the authorized
// list above.
//
// This deliberately walks the AST rather than grepping file text, because a
// plain substring search cannot tell a real call site from this package's
// own doc comments and test names explaining, by name, exactly the function
// this package must (almost) never call -- doc.go's rationale for the
// rename, and this very test's own name and comment, both say "RemoveAll"
// in prose without ever making the call.
func TestPackageNeverCallsRemoveAll(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot locate this package's directory")
	}
	dir := filepath.Dir(thisFile)

	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no .go files found to scan -- test setup is broken")
	}

	forbidden := "Remove" + "All" // built, not written as a literal identifier, in this file
	fset := token.NewFileSet()
	var offenders []string
	for _, path := range matches {
		if removeAllAuthorized[filepath.Base(path)] {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if ok && ident.Name == forbidden {
				offenders = append(offenders, fmt.Sprintf("%s:%d", path, fset.Position(ident.Pos()).Line))
			}
			return true
		})
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("the identifier %q is referenced in code at: %v -- this package must only ever rename a "+
			"boot directory aside, or delete a .prev-* directory from within the authorized exception in "+
			"sweep.go -- never delete anywhere else", forbidden, offenders)
	}
}

// TestSweepIsTheOnlyRemoveAllCallSite double-checks
// [removeAllAuthorized] itself is not quietly wrong: sweep.go really does
// reference RemoveAll (so the exception is not dead weight hiding a typo'd
// filename), and it is the only production (non-_test.go) file that does.
// Between this and [TestPackageNeverCallsRemoveAll], "sweep.go, and only
// sweep.go, and it really deletes" is enforced from both directions.
func TestSweepIsTheOnlyRemoveAllCallSite(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot locate this package's directory")
	}
	dir := filepath.Dir(thisFile)

	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	forbidden := "Remove" + "All"
	fset := token.NewFileSet()
	var productionOffenders []string
	sweepReferencesIt := false
	for _, path := range matches {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") {
			continue // this test only cares about production sources
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == forbidden {
				found = true
			}
			return true
		})
		if found {
			if base == "sweep.go" {
				sweepReferencesIt = true
			} else {
				productionOffenders = append(productionOffenders, base)
			}
		}
	}
	if !sweepReferencesIt {
		t.Error("sweep.go does not reference RemoveAll at all -- removeAllAuthorized's exception is stale " +
			"and should be removed, or Sweep no longer deletes anything, which would be worth knowing")
	}
	if len(productionOffenders) > 0 {
		sort.Strings(productionOffenders)
		t.Errorf("production files other than sweep.go reference RemoveAll: %v", productionOffenders)
	}
}
