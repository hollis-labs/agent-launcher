package apply_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// TestEndToEnd_EditBootReadsTheBundleWithoutApply re-baselines T29's old
// staged-copy scenario against agent-setup's current architecture.
//
// # The bundle is the source
//
// The profile names $CAIRN_PROFILE_ROOT/prompts, matching agent-setup 6da0e38.
// Cairn therefore reads prompt content from the same scratch bundle passed to
// --profile. There is no installed copy and no Apply step; editing the bundle
// is immediately visible to the next boot. Nothing here reads or writes the
// real agent-setup checkout or ~/.config/agents.
//
// # What this proves
//
//  1. Boot from the scratch bundle and read the original prompt.
//  2. Edit that bundle's prompt.
//  3. Boot a fresh session and read the edited prompt, with no staging seam.
func TestEndToEnd_EditBootReadsTheBundleWithoutApply(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping the end-to-end check: %v", err)
	}

	bundleRoot := t.TempDir()
	bootRoot := t.TempDir()

	mustWrite(t, filepath.Join(bundleRoot, "Makefile"), scratchMakefile)

	const original = "# hello\n\nThis is the ORIGINAL planted content.\n"
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), original)

	profile := "---\n" +
		"id: t29-e2e-scratch\n" +
		"name: T29 end-to-end scratch\n" +
		"provider: claude\n" +
		"spec:\n" +
		"  prompts_dir: $CAIRN_PROFILE_ROOT/prompts\n" +
		"  prompts:\n" +
		"    - hello\n" +
		"---\n"
	mustWrite(t, filepath.Join(bundleRoot, "profiles", "t29-e2e-scratch.md"), profile)

	runner := boot.ExecRunner(cairnPath)
	bootOnce := func(session string) string {
		t.Helper()
		argv := []string{
			"boot", "t29-e2e-scratch",
			"--profile", bundleRoot,
			"--boot-root", bootRoot,
			"--session", session,
			"--json",
		}
		result, stderr, err := boot.Invoke(context.Background(), runner, argv)
		if err != nil {
			t.Fatalf("cairn boot (session=%s): %v\nstderr:\n%s", session, err, stderr)
		}
		planted := filepath.Join(result.BootDir, ".claude", "commands", "boot", "hello.md")
		got, readErr := os.ReadFile(planted)
		if readErr != nil {
			t.Fatalf("reading planted prompt at %s: %v", planted, readErr)
		}
		return string(got)
	}

	// --- Step 1: the first boot reads the ORIGINAL content from the bundle. ---
	if got := bootOnce("before-edit"); got != original {
		t.Fatalf("planted prompt before edit = %q; want the original content %q", got, original)
	}

	// --- Step 2: edit the bundle's prompt directly. ---
	const edited = "# hello\n\nThis is the EDITED planted content -- T29 end-to-end proof.\n"
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), edited)

	// --- Step 3: a fresh boot reads the EDITED content immediately. ---
	if got := bootOnce("after-edit"); got != edited {
		t.Fatalf("planted prompt after edit = %q; want the EDITED content %q", got, edited)
	}
}
