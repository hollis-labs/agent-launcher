package boot_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/testbundle"
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
// It skips, with a message saying why, only when a prerequisite is
// genuinely absent from the machine running it: cairn not on PATH, or the
// ~/dev/projects/agent-setup bundle itself not present at all. A bundle
// that IS present but whose bindings this build of Tachyon cannot read --
// missing, unreadable, or in a shape it does not understand, which is
// exactly what happens once Cairn's bindings storage moves out from under
// this reader (CW-20260904-0003 / T24) -- is a different condition and
// FAILS the test instead; see [testbundle.Resolve] for that distinction,
// proven directly (without touching this real bundle) by
// internal/testbundle's own unit tests. Nothing here writes into that
// bundle -- --profile only reads it (confirmed by this task's own
// before/after `git status --short` check on that repo), and every path
// this test writes to lives under its own t.TempDir() boot root.
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

	bindings, skip, err := testbundle.Resolve(bundle)
	if skip {
		t.Skipf("no bundle at %s (agent-setup not present on this machine), skipping the real-binary reconciliation check", bundle)
	}
	if err != nil {
		t.Fatalf("bundle at %s is present but its bindings cannot be read -- this is exactly the break CW-20260904-0003 (T24) exists to catch loudly, not silence: %v", bundle, err)
	}

	// eng-nanite was a saved binding in agent-setup's bindings storage
	// (profile engineer, scope nanite) as of 2026-09-03 -- see the guard
	// above (which now fails loudly, not silently, once that stops being
	// true) rather than this comment for the current state.
	const target = "eng-nanite"
	found := false
	for _, b := range bindings {
		if b.Name == target {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bundle at %s is readable but has no binding named %q -- update this test's target, or the bundle", bundle, target)
	}
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
