package boot_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// --- fakes and helpers -------------------------------------------------

// lsofStdout renders synthetic `lsof -a -d cwd +D` stdout for rows holder
// rows: empty for zero (this host's ordinary "no holders" shape, measured
// directly while building this package -- see sweep.go's doc), otherwise a
// real lsof-shaped header followed by that many data lines.
func lsofStdout(rows int) string {
	if rows == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("COMMAND   PID      USER   FD   TYPE DEVICE SIZE/OFF      NODE NAME\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, "proc%-3d %5d chrispian  cwd    DIR   1,13       64 %9d /fake/path\n", i, 10000+i, 900000+i)
	}
	return b.String()
}

// targetFromArgs asserts a captured call matches the exact guard form
// Sweep always uses -- ["-a", "-d", "cwd", "+D", target] -- and returns
// target. Any other shape fails the test outright: a test asserting on
// per-target behavior is meaningless if Sweep silently changed the form it
// calls the guard in.
func targetFromArgs(t *testing.T, args []string) string {
	t.Helper()
	if len(args) != 5 || args[0] != "-a" || args[1] != "-d" || args[2] != "cwd" || args[3] != "+D" {
		t.Fatalf("lsof called with args %v; want exactly [-a -d cwd +D <target>]", args)
	}
	return args[4]
}

// lsofFake is a [boot.LsofRunner] driven entirely by target path, so a
// test controls exactly what the guard "finds" for the sweep's own
// positive-control target (its process cwd) independently of what it
// finds for each candidate -- the two are decided by different table
// entries even though Sweep issues both calls in the identical form.
type lsofFake struct {
	t      *testing.T
	rows   map[string]int
	stderr map[string]string
	err    map[string]error
	calls  []string
}

func (f *lsofFake) run(_ context.Context, args ...string) ([]byte, []byte, error) {
	target := targetFromArgs(f.t, args)
	f.calls = append(f.calls, target)
	if err, ok := f.err[target]; ok {
		return nil, nil, err
	}
	return []byte(lsofStdout(f.rows[target])), []byte(f.stderr[target]), nil
}

// controlTarget returns exactly what [boot.Sweep] will use as its positive
// control target: its own process's cwd, via os.Getwd() -- the same call
// Sweep itself makes. Tests using [lsofFake] key their rows/stderr/err
// tables on this so the control can be made to succeed or fail
// deterministically without touching Sweep's signature.
func controlTarget(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	return wd
}

// testProject stands in for the project segment every real boot directory
// carries: the tree is <root>/<project>/<profile>/<session>, and a .prev-* is
// a sibling of a session. Fixtures here plant at that depth rather than one
// shallower, because a sweep that scanned the wrong depth would find nothing
// and look exactly like a sweep with nothing to do.
//
// It is a literal rather than boot.ProjectKey's output: these tests are
// about the sweep's walk, not about how a project segment is derived, and a
// hand-written one keeps them from failing if that derivation changes.
const testProject = "someproject-0123456789"

// profileDir is <root>/<project>/<profile> — the directory a .prev-* and a
// live session sit inside.
func profileDir(root, profile string) string {
	return filepath.Join(root, testProject, profile)
}

// mkPrev creates <root>/<project>/<profile>/.prev-<suffix> as a directory
// and returns its path.
func mkPrev(t *testing.T, root, profile, suffix string) string {
	t.Helper()
	p := filepath.Join(profileDir(root, profile), boot.PrevPrefix+suffix)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", p, err)
	}
	return p
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to still exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to have been removed; stat err = %v", path, err)
	}
}

// requireRealLsof mirrors the internal test file's requireLsof, resolved
// independently here since this is a different (external) test package.
func requireRealLsof(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("lsof")
	if err != nil {
		t.Skip("lsof not found on PATH; skipping real-lsof test")
	}
	return path
}

// spawnHolder starts a real long-lived process with its cwd set to dir and
// kills it when the test ends. See sweep_internal_test.go's own copy for
// the full rationale; duplicated here rather than exported across a
// package boundary that otherwise has no reason to exist.
func spawnHolder(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sleep", "300")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("spawnHolder: starting sleep with cwd %s: %v", dir, err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
}

func waitUntilSwept(t *testing.T, run func() boot.Report, want string) boot.Report {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last boot.Report
	for {
		last = run()
		for _, s := range last.Swept {
			if s == want {
				return last
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Sweep never reported %s as swept within the deadline; last report: %+v", want, last)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// --- acceptance: subdirectory and same-directory holders ---------------

// TestSweep_SubdirectoryHolderPreventsDeletion is the exact case this task
// exists for: a real process whose cwd is <candidate>/.claude -- exactly
// what a session that has cd'd into its settings directory looks like --
// must prevent the candidate's deletion. Uses the real lsof binary and a
// real spawned process; no fakes anywhere in this test.
func TestSweep_SubdirectoryHolderPreventsDeletion(t *testing.T) {
	lsofPath := requireRealLsof(t)
	root := t.TempDir()
	prev := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")
	claudeDir := filepath.Join(prev, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", claudeDir, err)
	}

	spawnHolder(t, claudeDir)

	// Give lsof a moment to be able to see the freshly started process --
	// polled via a few Sweep runs rather than a single fixed sleep, since a
	// real process's visibility to lsof is not guaranteed at the exact
	// instant Start() returns.
	var report boot.Report
	deadline := time.Now().Add(3 * time.Second)
	for {
		var err error
		report, err = boot.Sweep(context.Background(), root, boot.ExecLsofRunner(lsofPath))
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if !report.GuardOK {
			t.Fatalf("Sweep reported GuardOK=false unexpectedly: %s", report.GuardDetail)
		}
		if len(report.Swept) == 0 {
			break // candidate survived, as expected -- confirmed below
		}
		if time.Now().After(deadline) {
			t.Fatalf("Sweep kept swallowing the held candidate even after the holder should have been visible; last report: %+v", report)
		}
		time.Sleep(50 * time.Millisecond)
	}

	mustExist(t, prev)
	mustExist(t, claudeDir)
	found := false
	for _, sk := range report.Skipped {
		if sk.Path == prev {
			found = true
			t.Logf("skip reason: %s", sk.Reason)
		}
	}
	if !found {
		t.Fatalf("expected %s in report.Skipped; report = %+v", prev, report)
	}
}

// TestSweep_SameDirectoryHolderPreventsDeletion is the same property one
// layer simpler: a process whose cwd IS the candidate, not a subdirectory
// of it.
func TestSweep_SameDirectoryHolderPreventsDeletion(t *testing.T) {
	lsofPath := requireRealLsof(t)
	root := t.TempDir()
	prev := mkPrev(t, root, "planner", "20260904T000000.000000000Z")

	spawnHolder(t, prev)

	var report boot.Report
	deadline := time.Now().Add(3 * time.Second)
	for {
		var err error
		report, err = boot.Sweep(context.Background(), root, boot.ExecLsofRunner(lsofPath))
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if !report.GuardOK {
			t.Fatalf("Sweep reported GuardOK=false unexpectedly: %s", report.GuardDetail)
		}
		if len(report.Swept) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Sweep kept swallowing the held candidate; last report: %+v", report)
		}
		time.Sleep(50 * time.Millisecond)
	}

	mustExist(t, prev)
}

// TestSweep_RemovesGenuinelyFreeCandidates proves the other half with real
// lsof: a candidate nothing holds open is actually removed, and the run's
// own positive control -- Sweep's own test-process cwd, definitionally
// live while this test runs -- genuinely succeeds through the real
// ExecLsofRunner, not a fake standing in for it.
func TestSweep_RemovesGenuinelyFreeCandidates(t *testing.T) {
	lsofPath := requireRealLsof(t)
	root := t.TempDir()
	prev := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")

	report := waitUntilSwept(t, func() boot.Report {
		r, err := boot.Sweep(context.Background(), root, boot.ExecLsofRunner(lsofPath))
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		return r
	}, prev)

	if !report.GuardOK {
		t.Fatalf("report.GuardOK = false; want true (the real positive control against this test process's own cwd should succeed): %s", report.GuardDetail)
	}
	mustNotExist(t, prev)
	if len(report.Skipped) != 0 {
		t.Errorf("report.Skipped = %+v; want empty for a lone genuinely free candidate", report.Skipped)
	}
}

// --- acceptance: the broken-guard safety property (comment 2633) -------

// TestSweep_ControlFindsZeroRows_SweepsNothing is the corrected design's
// central new safety property: when the positive control itself observes
// zero rows -- simulating the lsof mechanism being broken on this run,
// which produces byte-identical output to a genuine "no holders" answer
// -- NOTHING is swept, even for a candidate that is (in this fake) also
// reporting zero rows and would otherwise look genuinely free. Comment
// 2633 on CW-20260903-0019 is explicit that this must be tested, not just
// asserted in a comment.
func TestSweep_ControlFindsZeroRows_SweepsNothing(t *testing.T) {
	root := t.TempDir()
	prev := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")

	fr := &lsofFake{t: t, rows: map[string]int{
		// Every target, including the control target, defaults to 0 rows
		// via the zero value -- deliberately not populating an entry for
		// controlTarget(t) here, so the control observes zero holders of
		// a directory that (in reality) is definitely live. That is
		// exactly the "mechanism is broken" shape.
	}}

	report, err := boot.Sweep(context.Background(), root, fr.run)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if report.GuardOK {
		t.Fatalf("report.GuardOK = true; want false when the positive control finds zero rows")
	}
	if report.GuardDetail == "" {
		t.Error("report.GuardDetail is empty; want an explanation of why the guard could not be trusted")
	}
	if len(report.Swept) != 0 {
		t.Fatalf("report.Swept = %v; want empty -- a broken guard must sweep nothing, even for a candidate that also looked free", report.Swept)
	}
	mustExist(t, prev)

	found := false
	for _, sk := range report.Skipped {
		if sk.Path == prev {
			found = true
			if !strings.Contains(sk.Reason, "guard unavailable") {
				t.Errorf("skip reason for %s = %q; want it to name the guard as unavailable", prev, sk.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("expected %s in report.Skipped with a guard-unavailable reason; report = %+v", prev, report)
	}
}

// --- acceptance: invocation-sanity failures fail closed -----------------

// TestSweep_LsofMissing_SweepsNothing exercises "the binary cannot be
// found" two ways: an [boot.ExecLsofRunner] pointed at a path that does
// not exist (an injected unfindable binary), and one resolved from an
// empty PATH (so even the bare "lsof" name cannot be found). Both must
// report GuardOK=false and remove nothing.
func TestSweep_LsofMissing_SweepsNothing(t *testing.T) {
	t.Run("unfindable explicit path", func(t *testing.T) {
		root := t.TempDir()
		prev := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")

		runner := boot.ExecLsofRunner(filepath.Join(t.TempDir(), "definitely-not-a-real-lsof-binary"))
		report, err := boot.Sweep(context.Background(), root, runner)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if report.GuardOK {
			t.Fatal("report.GuardOK = true; want false when lsof cannot be found")
		}
		if len(report.Swept) != 0 {
			t.Fatalf("report.Swept = %v; want empty", report.Swept)
		}
		mustExist(t, prev)
	})

	t.Run("empty PATH", func(t *testing.T) {
		root := t.TempDir()
		prev := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")

		emptyPathDir := t.TempDir() // a directory guaranteed to hold no "lsof"
		t.Setenv("PATH", emptyPathDir)

		runner := boot.ExecLsofRunner("") // bare "lsof"; resolved against PATH at Start() time
		report, err := boot.Sweep(context.Background(), root, runner)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if report.GuardOK {
			t.Fatal("report.GuardOK = true; want false when PATH has no lsof")
		}
		if len(report.Swept) != 0 {
			t.Fatalf("report.Swept = %v; want empty", report.Swept)
		}
		mustExist(t, prev)
	})
}

// TestSweep_HardExecError_SweepsNothing simulates a runner whose
// underlying process could not be started at all -- an *exec.Error, the
// same shape a real ExecLsofRunner produces for a missing binary -- for
// both the control call and, separately, a single candidate's call, and
// confirms each is classified as an invocation failure rather than "found
// nothing".
func TestSweep_HardExecError_SweepsNothing(t *testing.T) {
	root := t.TempDir()
	prev := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")

	fr := &lsofFake{
		t:   t,
		err: map[string]error{controlTarget(t): &exec.Error{Name: "lsof", Err: exec.ErrNotFound}},
	}

	report, err := boot.Sweep(context.Background(), root, fr.run)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if report.GuardOK {
		t.Fatal("report.GuardOK = true; want false when the control call itself hard-errors")
	}
	if !strings.Contains(report.GuardDetail, "lsof") {
		t.Errorf("report.GuardDetail = %q; want it to mention the underlying failure", report.GuardDetail)
	}
	mustExist(t, prev)
}

// TestSweep_UnparseableOutput_SkipsOnlyThatCandidate proves the guard
// distinguishes a control failure (sweeps nothing at all) from a single
// candidate's own invocation-sanity failure (only that candidate is
// skipped; a genuinely free sibling candidate in the same run still gets
// swept, and GuardOK stays true because the control itself was fine).
func TestSweep_UnparseableOutput_SkipsOnlyThatCandidate(t *testing.T) {
	root := t.TempDir()
	garbage := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")
	free := mkPrev(t, root, "eng-nanite", "20260904T020000.000000000Z")

	fr := &lsofFake{
		t:      t,
		rows:   map[string]int{controlTarget(t): 1},
		stderr: map[string]string{},
	}
	// Route garbage's own probe to unparseable stdout by giving it a hard
	// error path instead is wrong (that tests something else); simulate
	// unparseable output directly via a small wrapper below.
	base := fr.run
	runner := func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		target := targetFromArgs(t, args)
		if target == garbage {
			return []byte("lsof: WARNING: something something\n"), nil, nil
		}
		return base(ctx, args...)
	}

	report, err := boot.Sweep(context.Background(), root, runner)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !report.GuardOK {
		t.Fatalf("report.GuardOK = false; want true -- the control itself was fine: %s", report.GuardDetail)
	}
	sort.Strings(report.Swept)
	if len(report.Swept) != 1 || report.Swept[0] != free {
		t.Fatalf("report.Swept = %v; want exactly [%s]", report.Swept, free)
	}
	mustExist(t, garbage)
	mustNotExist(t, free)

	found := false
	for _, sk := range report.Skipped {
		if sk.Path == garbage {
			found = true
			if !strings.Contains(sk.Reason, "guard invocation failed") {
				t.Errorf("skip reason for %s = %q; want it to name an invocation failure", garbage, sk.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("expected %s in report.Skipped; report = %+v", garbage, report)
	}
}

// --- acceptance: scope -- only .prev-* under root is ever touched -------

// TestSweep_OnlyPrevPrefixedDirectoriesAreEverRemoved builds a boot root
// with everything Sweep must leave alone sitting right next to what it
// must remove: [boot.DefaultSession] itself, a differently-named
// directory, a stray file directly in the boot root, and a file (not a
// directory) that happens to be named like a .prev-* candidate. Only the
// one genuine .prev-* directory is removed.
func TestSweep_OnlyPrevPrefixedDirectoriesAreEverRemoved(t *testing.T) {
	root := t.TempDir()

	current := filepath.Join(profileDir(root, "eng-nanite"), boot.DefaultSession)
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", current, err)
	}
	marker := filepath.Join(current, "AGENTS.md")
	if err := os.WriteFile(marker, []byte("do not touch"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", marker, err)
	}

	notPrev := filepath.Join(profileDir(root, "eng-nanite"), "not-a-prev-dir")
	if err := os.MkdirAll(notPrev, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", notPrev, err)
	}

	strayFile := filepath.Join(root, "stray.txt")
	if err := os.WriteFile(strayFile, []byte("stray"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", strayFile, err)
	}

	prevNamedFile := filepath.Join(profileDir(root, "eng-nanite"), boot.PrevPrefix+"but-a-file")
	if err := os.WriteFile(prevNamedFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", prevNamedFile, err)
	}

	genuine := mkPrev(t, root, "eng-nanite", "20260904T000000.000000000Z")
	genuine2 := mkPrev(t, root, "other-binding", "20260904T010000.000000000Z")

	fr := &lsofFake{t: t, rows: map[string]int{controlTarget(t): 1}} // everything else defaults to 0

	report, err := boot.Sweep(context.Background(), root, fr.run)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !report.GuardOK {
		t.Fatalf("report.GuardOK = false; want true: %s", report.GuardDetail)
	}

	sort.Strings(report.Swept)
	wantSwept := []string{genuine, genuine2}
	sort.Strings(wantSwept)
	if fmt.Sprint(report.Swept) != fmt.Sprint(wantSwept) {
		t.Fatalf("report.Swept = %v; want %v", report.Swept, wantSwept)
	}

	mustNotExist(t, genuine)
	mustNotExist(t, genuine2)

	mustExist(t, current)
	mustExist(t, marker)
	mustExist(t, notPrev)
	mustExist(t, strayFile)
	mustExist(t, prevNamedFile)

	// Confirm lsof was never even asked about anything but the control and
	// the two genuine .prev-* candidates -- current, the stray file, the
	// not-a-prev directory and the prev-named file must never appear as a
	// probe target at all, which is the strongest form of "never touched".
	untouchable := map[string]bool{
		current: true, notPrev: true, strayFile: true, prevNamedFile: true, root: true,
	}
	for _, c := range fr.calls {
		if untouchable[c] {
			t.Errorf("lsof was asked about %s, which must never be a candidate", c)
		}
	}
}

// TestSweep_NeverFollowsAProviderHomeLink is the safety property Codex
// launches introduced (CW-20260906-0001): a boot directory now carries
// symbolic links to the operator's live Codex credentials and hook
// registrations (see [boot.PrepareHomeResources]), so the sweep that removes
// old .prev-* directories must remove those LINKS and never what they point
// at. os.RemoveAll unlinks rather than descends, which makes this true by
// construction — this test is what keeps it true if the removal ever changes.
//
// It also covers the guard's own walk: lsof is invoked with +D over the
// candidate's tree, and neither that nor anything else here may reach out of
// the boot root and touch the operator's real home.
func TestSweep_NeverFollowsAProviderHomeLink(t *testing.T) {
	lsofPath := requireRealLsof(t)
	root := t.TempDir()
	prev := mkPrev(t, root, "codex-coord-agent-setup", "20260906T000000.000000000Z")

	// Stand in for the operator's real ~/.codex, well outside the boot root.
	sourceHome := t.TempDir()
	auth := filepath.Join(sourceHome, "auth.json")
	hooksDir := filepath.Join(sourceHome, "hooks")
	hookScript := filepath.Join(hooksDir, "session-start.sh")
	if err := os.WriteFile(auth, []byte(`{"token":"redacted"}`+"\n"), 0o600); err != nil {
		t.Fatalf("creating the source auth.json: %v", err)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("creating the source hooks dir: %v", err)
	}
	if err := os.WriteFile(hookScript, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating the source hook: %v", err)
	}

	for _, link := range []struct{ target, name string }{
		{target: auth, name: "auth.json"},
		{target: hooksDir, name: "hooks"},
	} {
		if err := os.Symlink(link.target, filepath.Join(prev, link.name)); err != nil {
			t.Fatalf("linking %s into the old boot directory: %v", link.name, err)
		}
	}

	report := waitUntilSwept(t, func() boot.Report {
		r, err := boot.Sweep(context.Background(), root, boot.ExecLsofRunner(lsofPath))
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		return r
	}, prev)

	if !report.GuardOK {
		t.Fatalf("report.GuardOK = false; want true: %s", report.GuardDetail)
	}
	mustNotExist(t, prev)

	// The whole point: the links went, the operator's own files did not.
	mustExist(t, auth)
	mustExist(t, hooksDir)
	mustExist(t, hookScript)
}

// TestSweepFindsWhatPrepareMovedAside is the drift guard prevCandidates' own
// doc names: the sweep's walk and Prepare's layout are one shape, and if they
// ever disagree the sweep quietly stops finding anything.
//
// That failure is invisible from the outside — a sweep with nothing to do and
// a sweep looking in the wrong place report the same empty result — so this
// plants through the real Prepare, at a real ProjectKey, and requires the
// sweep to report it as a candidate. It is the reason neither side can be
// changed alone.
func TestSweepFindsWhatPrepareMovedAside(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, boot.ProjectKey("/Users/somebody/dev/projects/cairn"))
	key := boot.Key("engineer")
	session := boot.SessionKey("codex")

	// First plant: nothing to move aside.
	plan, err := boot.Prepare(projectRoot, key, session)
	if err != nil {
		t.Fatalf("Prepare (first): %v", err)
	}
	if err := os.MkdirAll(plan.Current, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", plan.Current, err)
	}

	// Relaunch: this is what produces a .prev-*.
	second, err := boot.Prepare(projectRoot, key, session)
	if err != nil {
		t.Fatalf("Prepare (relaunch): %v", err)
	}
	if !second.Moved() {
		t.Fatal("the relaunch moved nothing aside; there is no candidate to find")
	}

	// The guard is deliberately made unavailable so nothing is deleted: what
	// this test needs is that the candidate was FOUND, and a skipped
	// candidate is reported by path just as a swept one is.
	fr := &lsofFake{t: t, rows: map[string]int{}} // control finds zero holders -> guard not trusted
	report, err := boot.Sweep(context.Background(), root, fr.run)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	found := false
	for _, s := range report.Skipped {
		if s.Path == second.MovedAside {
			found = true
		}
	}
	for _, s := range report.Swept {
		if s == second.MovedAside {
			found = true
		}
	}
	if !found {
		t.Fatalf(
			"Sweep did not find %q, which Prepare had just created.\n"+
				"prevCandidates walks <root>/<project>/<profile>/.prev-* and Prepare plants at that\n"+
				"depth; one of them moved. report = %+v",
			second.MovedAside, report,
		)
	}
}
