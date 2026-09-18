package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
)

// TestRunAgainstRealCairn exercises this command's own [Run] end to end
// against the real cairn binary and the real agent-setup bundle — the same
// kind of check internal/boot/reconcile_test.go does for T10, extended to
// prove this command's specific "running twice leaves the filesystem
// unchanged" property against cairn's actual behavior, not a fake's (see
// TestRunNeverMovesCurrentAside in debug_test.go for that same property
// proven deterministically).
//
// It skips, with a message saying why, only when a prerequisite is
// genuinely absent from the machine running it: cairn not on PATH, or the
// ~/dev/projects/agent-setup bundle itself not present at all. A bundle
// that IS present but missing the profile this test names is a different
// condition and FAILS instead — the same distinction
// internal/boot/reconcile_test.go relies on, and the one that caught
// agent-setup retiring bindings/ out from under Tachyon. Nothing here
// writes into that bundle — --profile only reads it — and every path this
// test writes to lives under its own t.TempDir() boot root.
func TestRunAgainstRealCairn(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping the real-binary check: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	bundleRoot := filepath.Join(home, "dev", "projects", "agent-setup")

	if _, statErr := os.Stat(bundleRoot); statErr != nil {
		t.Skipf("no bundle at %s (agent-setup not present on this machine), skipping the real-binary check", bundleRoot)
	}

	const target = "engineer"
	if _, statErr := os.Stat(filepath.Join(bundleRoot, "profiles", target+".md")); statErr != nil {
		t.Fatalf("the bundle at %s is present but has no profiles/%s.md (%v) -- update this test's target, or the bundle", bundleRoot, target, statErr)
	}

	// The launch profile is what declares the provider. Without one cairn
	// refuses the render outright, since no profile in agent-setup names a
	// provider any more — so this is also the check that --launch-profile
	// reaches cairn as a --with.
	launchStore := launchprofile.Open(t.TempDir())
	launchProfile, err := launchStore.Create("debug", launchprofile.Scaffold("debug", "claude"))
	if err != nil {
		t.Fatalf("writing the launch profile: %v", err)
	}

	scratchRoot := t.TempDir()
	cfg := Config{
		Target:        target,
		Bundle:        bundleRoot,
		BootRoot:      scratchRoot,
		LaunchProfile: launchProfile.Path,
	}
	runner := boot.ExecRunner(cairnPath)

	report, err := Run(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.InvokeErr != nil {
		t.Fatalf("Run's first invocation of real cairn failed: %v\nstderr:\n%s", report.InvokeErr, report.Stderr)
	}
	if report.Result.BootDir != report.ExpectedBootDir {
		t.Fatalf(
			"boot.Key/SessionKey/SessionPath disagree with cairn's actual boot_dir (see internal/boot/reconcile_test.go):\n"+
				"  expected %q\n  got      %q",
			report.ExpectedBootDir, report.Result.BootDir,
		)
	}
	if !argvHasFlag(report.HarnessArgv, "--settings") {
		t.Fatalf("harness argv %v does not contain --settings", report.HarnessArgv)
	}
	if info, statErr := os.Stat(report.Result.BootDir); statErr != nil {
		t.Fatalf("cairn reported boot_dir %q but nothing exists there: %v", report.Result.BootDir, statErr)
	} else if !info.IsDir() {
		t.Fatalf("cairn reported boot_dir %q but it is not a directory", report.Result.BootDir)
	}

	afterFirst := snapshotTree(t, scratchRoot)

	// Run again, same target, same scratch root. Run never calls
	// boot.Prepare, so the real cairn refuses the second plant ("boot
	// directory already exists") — an *boot.InvokeError, not success — and
	// the filesystem must come out exactly as it was after the first run.
	report2, err := Run(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("Run (2nd call): %v", err)
	}
	if report2.InvokeErr == nil {
		t.Fatalf("expected the 2nd real cairn invocation to fail with 'already exists', got a successful Result: %+v", report2.Result)
	}
	t.Logf("2nd invocation failed as expected (Run never calls boot.Prepare): %v", report2.InvokeErr)

	afterSecond := snapshotTree(t, scratchRoot)
	if !reflect.DeepEqual(afterFirst, afterSecond) {
		t.Fatalf("running Run twice against the real cairn changed the filesystem:\nafter 1st: %v\nafter 2nd: %v", afterFirst, afterSecond)
	}
	for _, p := range afterSecond {
		if strings.Contains(p, boot.PrevPrefix) {
			t.Errorf("found a %s* entry at %q — Run must never call boot.Prepare", boot.PrevPrefix, p)
		}
	}
}
