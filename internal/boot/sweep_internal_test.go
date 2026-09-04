package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This file is a white-box test (package boot, not boot_test) for the same
// reason spawn_test.go is: it exercises probe and parseLsofRows directly by
// name, the exact unexported functions [Sweep] itself calls for both its
// positive control and every candidate check. The acceptance criteria for
// CW-20260903-0019 require the positive-control proof to "exercise the
// real code path, not a mock of it" -- these tests do that literally, by
// calling the same probe function Sweep calls, through a real
// [ExecLsofRunner], against real spawned processes. sweep_test.go (package
// boot_test) covers Sweep's own end-to-end behavior, including the same
// property one level up.

// requireLsof skips the test if lsof is not on PATH, so this file degrades
// gracefully on a machine without it rather than failing for an unrelated
// reason. Every environment this was developed and run against has lsof at
// /usr/sbin/lsof (macOS 24.6.0, lsof 4.91).
func requireLsof(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("lsof")
	if err != nil {
		t.Skip("lsof not found on PATH; skipping real-lsof test")
	}
	return path
}

// spawnHolder starts a real, long-lived process (sleep) with its working
// directory set to dir, and arranges for it to be killed when the test
// ends -- via context cancellation, which exec.CommandContext wires to an
// automatic kill, not a manual Process.Kill/Wait dance. dir must already
// exist. This is the only way this file (or sweep_test.go) proves a
// directory is "live": a real process, with a real cwd, that this test
// itself started and controls -- never the real Tachyon boot root, and
// never anything Chrispian's own use of the app produced.
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

// waitForRows polls probe against target until it observes sane and at
// least minRows rows, or fails the test after a bounded deadline. A freshly
// started process's cwd is visible to lsof almost immediately, but not
// provably within the same instant Start() returns -- polling here removes
// that race rather than papering over it with a fixed sleep that could
// still flake on a loaded machine.
func waitForRows(t *testing.T, runner LsofRunner, target string, minRows int) probeOutcome {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last probeOutcome
	for {
		last = probe(context.Background(), runner, target)
		if last.sane && last.rows >= minRows {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe(%s) never observed >= %d row(s) within the deadline; last outcome: sane=%v rows=%d detail=%q",
				target, minRows, last.sane, last.rows, last.detail)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestProbe_RealLsof_PositiveControl is the positive control at the
// lowest level this package has: a real ExecLsofRunner, a real spawned
// process holding a real directory open as its cwd, asserting probe finds
// it -- and, in the same test, that the identical call against a
// genuinely free scratch directory finds nothing. Read together, as
// CW-20260903-0019 comment 2630/2633 both insist on: the first half is
// what makes the second half interpretable, rather than "no output" being
// assumed to mean "no holders" on faith.
func TestProbe_RealLsof_PositiveControl(t *testing.T) {
	lsofPath := requireLsof(t)
	runner := ExecLsofRunner(lsofPath)

	root := t.TempDir()
	held := filepath.Join(root, "held")
	free := filepath.Join(root, "free")
	for _, d := range []string{held, free} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", d, err)
		}
	}

	spawnHolder(t, held)

	gotHeld := waitForRows(t, runner, held, 1)
	if gotHeld.rows < 1 {
		t.Fatalf("probe(%s) rows = %d; want >= 1 (a real process has this as its cwd)", held, gotHeld.rows)
	}
	t.Logf("positive control: probe(%s) sane=%v rows=%d", held, gotHeld.sane, gotHeld.rows)

	gotFree := probe(context.Background(), runner, free)
	if !gotFree.sane {
		t.Fatalf("probe(%s) sane = false, detail = %q; want a clean zero-holder result", free, gotFree.detail)
	}
	if gotFree.rows != 0 {
		t.Fatalf("probe(%s) rows = %d; want 0 (nothing holds this directory open)", free, gotFree.rows)
	}
	t.Logf("free candidate: probe(%s) sane=%v rows=%d", free, gotFree.sane, gotFree.rows)
}

// TestProbe_RealLsof_SubdirectoryHolder is the exact case CW-20260903-0019
// exists for, proven at the probe level: a process whose cwd is a
// subdirectory of the candidate (mirroring a session that has cd'd into
// <bootdir>/.claude, which every real boot directory contains) is still
// found by +D. This is the property that rules out the exact-path form
// once and for all -- see the sibling assertion in sweep_test.go's
// TestSweep_SubdirectoryHolderPreventsDeletion for the same property
// exercised through Sweep end to end.
func TestProbe_RealLsof_SubdirectoryHolder(t *testing.T) {
	lsofPath := requireLsof(t)
	runner := ExecLsofRunner(lsofPath)

	root := t.TempDir()
	prevDir := filepath.Join(root, ".prev-20260101T000000.000000000Z")
	claudeDir := filepath.Join(prevDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", claudeDir, err)
	}

	spawnHolder(t, claudeDir)

	got := waitForRows(t, runner, prevDir, 1)
	t.Logf("+D on %s (holder is in %s) sane=%v rows=%d", prevDir, claudeDir, got.sane, got.rows)
	if got.rows < 1 {
		t.Fatalf("probe(%s) rows = %d; want >= 1 -- a holder in the .claude subdirectory must count", prevDir, got.rows)
	}
}

// TestParseLsofRows exercises the row-counting parser directly, including
// the two shapes that matter most for this design: genuinely empty output
// (this host's ordinary "no holders" shape under +D, see [Sweep]'s doc)
// and a header with no data rows -- both zero rows, both "ok" to trust as
// a count, as distinct from output that doesn't look like lsof's format at
// all, which is not.
func TestParseLsofRows(t *testing.T) {
	cases := []struct {
		name     string
		stdout   string
		wantRows int
		wantOK   bool
	}{
		{"empty", "", 0, true},
		{"whitespace only", "   \n  \n", 0, true},
		{"header only, no data rows", "COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF   NODE NAME\n", 0, true},
		{
			"header plus one row",
			"COMMAND   PID      USER   FD   TYPE DEVICE SIZE/OFF      NODE NAME\n" +
				"sleep   65163 chrispian  cwd    DIR   1,13       64 313115800 /some/path\n",
			1, true,
		},
		{
			"header plus three rows",
			"COMMAND   PID      USER   FD   TYPE DEVICE SIZE/OFF      NODE NAME\n" +
				"zsh     35183 chrispian  cwd    DIR   1,13       64 313115800 /some/path\n" +
				"claude  35332 chrispian  cwd    DIR   1,13       64 313115801 /some/path\n" +
				"mux     35412 chrispian  cwd    DIR   1,13       64 313115802 /some/path\n",
			3, true,
		},
		{"unparseable garbage", "lsof: WARNING: something something\n", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, ok := parseLsofRows([]byte(tc.stdout))
			if ok != tc.wantOK || rows != tc.wantRows {
				t.Errorf("parseLsofRows(%q) = (%d, %v); want (%d, %v)", tc.stdout, rows, ok, tc.wantRows, tc.wantOK)
			}
		})
	}
}
