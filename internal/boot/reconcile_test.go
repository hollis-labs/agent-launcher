package boot_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// TestKeyReconcilesWithRealCairnPlant is CW-20260903-0014 comment 2537's
// empirical check, made a permanent regression guard: internal/boot.Key()
// must derive exactly the directory segment the real Cairn binary plants a
// saved binding into, because a saved binding's boot-target string and its
// Key() seed are, by construction, meant to be the same string (see
// key.go's doc comment, and Tesseract's tachyon_vnext_target_architecture
// D6 -- the design document that first stated it is archived history and
// is deliberately not cited here). If they ever
// disagreed, boot.CurrentPath would name a directory Cairn never plants
// into: Prepare would move aside a path nothing writes, while the real
// boot directory silently accumulated a fresh ~/.claude.json trust entry
// per launch under a different name -- defeating the one thing T09 exists
// for, with no error anywhere.
//
// This shells out to the real cairn binary against a real bundle with
// saved bindings (~/dev/projects/agent-setup) rather than reasoning about
// the match from two independent constructions of the same expected
// string -- comment 2537 asked for exactly that distinction: "a comparison
// computed from Key() on one side and the actual planted path on the
// other -- not two constructions of the same expected string, which is a
// second copy that happens to agree."
//
// It skips, rather than fails, when a prerequisite is absent from the
// machine running it: cairn not on PATH, or
// ~/dev/projects/agent-setup/bindings.yaml not present. Nothing here
// writes into that bundle -- --profile only reads it (confirmed by this
// task's own before/after `git status --short` check on that repo), and
// every path this test writes to lives under its own t.TempDir() boot
// root.
func TestKeyReconcilesWithRealCairnPlant(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping the real-binary reconciliation check: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	bundle := filepath.Join(home, "dev", "projects", "agent-setup")
	if _, err := os.Stat(filepath.Join(bundle, "bindings.yaml")); err != nil {
		t.Skipf("no bindings.yaml under %s (agent-setup bundle not present on this machine): %v", bundle, err)
	}

	// eng-nanite is a saved binding in agent-setup's bindings.yaml (profile
	// engineer, scope nanite) as of 2026-09-03 -- see that file for the
	// current list if this ever needs to change.
	const target = "eng-nanite"
	scratchRoot := t.TempDir()

	runner := boot.ExecRunner(cairnPath)
	argv := []string{
		"boot", target,
		"--profile", bundle,
		"--boot-root", scratchRoot,
		"--session", boot.CurrentSegment,
		"--json",
	}

	result, stderr, err := boot.Invoke(context.Background(), runner, argv)
	if err != nil {
		t.Fatalf("real `cairn %v` failed: %v\nstderr:\n%s", argv, err, stderr)
	}

	want := boot.CurrentPath(scratchRoot, boot.Key(target))
	if result.BootDir != want {
		t.Fatalf(
			"MISMATCH between boot.Key()/CurrentPath and where cairn actually planted -- "+
				"this is a design question for the planner, not something to patch around here "+
				"(see comment 2537 on CW-20260903-0014):\n"+
				"  boot.CurrentPath(scratchRoot, boot.Key(%q)) = %q\n"+
				"  cairn boot --json reported boot_dir           = %q",
			target, want, result.BootDir,
		)
	}

	if info, statErr := os.Stat(result.BootDir); statErr != nil {
		t.Fatalf("cairn reported boot_dir %q but nothing exists there: %v", result.BootDir, statErr)
	} else if !info.IsDir() {
		t.Fatalf("cairn reported boot_dir %q but it is not a directory", result.BootDir)
	}
}
