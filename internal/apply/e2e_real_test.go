package apply_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/apply"
	"github.com/hollis-labs/tachyon/internal/boot"
)

// TestEndToEnd_EditApplyBootReadsThePlantedFile is CW-20260904-0023's own
// acceptance bullet, proven literally: "edit a prompt, Apply, boot, read
// the planted file."
//
// # The scratch-profile design (see this task's own instructions)
//
// agent-setup's real profiles/base.md hardcodes skills_dir and prompts_dir
// as literal ~/.config/agents/... paths -- not overridable via AGENTS_HOME
// or any env var on Cairn's *reading* side (only `make install-system`'s
// *writing* side respects AGENTS_HOME). A real `cairn boot` against
// base.md would therefore always read from the real ~/.config/agents,
// which this whole task must never write to in a test.
//
// The way through, verified by hand before any of this was written (see
// this task's own report): build a small scratch profile of our own,
// mirroring base.md's structure only as far as needed (id, name, provider,
// spec.prompts_dir, spec.prompts), whose prompts_dir points at a scratch
// AGENTS_HOME this test owns rather than the real one. Cairn's own
// --profile flag reads the catalog (the profile file) from wherever it is
// told, and spec.prompts_dir is a plain absolute path resolved
// independently of --profile -- so a profile that names a scratch
// directory there resolves prompts from that scratch directory regardless
// of which bundle --profile points at. That is exactly what this test
// exploits: nothing here ever reads or writes the real
// ~/dev/projects/agent-setup or ~/.config/agents.
//
// # What this proves
//
//  1. Stage the bundle's original prompt for real (apply.Invoke, real
//     make install-system), boot for real, read the planted file: it
//     carries the original content.
//  2. Edit the bundle's prompt. Compare must now read "differs".
//  3. Apply again, for real. Compare must go dark again.
//  4. Boot again (a fresh session), read the newly planted file: it
//     carries the EDITED content -- proving the edit actually reached a
//     real boot only once Apply staged it, and not before.
func TestEndToEnd_EditApplyBootReadsThePlantedFile(t *testing.T) {
	requireMake(t)
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping the end-to-end check: %v", err)
	}

	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()
	bootRoot := t.TempDir()

	mustWrite(t, filepath.Join(bundleRoot, "Makefile"), scratchMakefile)
	// templates/ and skills/ must exist for install-system's rsync lines to
	// find a source directory at all (rsync fails outright against a
	// missing source) -- empty is fine, this test's whole concern is
	// prompts/.
	if err := os.MkdirAll(filepath.Join(bundleRoot, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundleRoot, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}

	const original = "# hello\n\nThis is the ORIGINAL planted content.\n"
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), original)

	profile := "---\n" +
		"id: t29-e2e-scratch\n" +
		"name: T29 end-to-end scratch\n" +
		"provider: claude\n" +
		"spec:\n" +
		"  prompts_dir: " + filepath.Join(agentsHome, "prompts") + "\n" +
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

	// --- Step 0: before anything is staged, prompts_dir/hello.md does not
	// exist yet, so cairn boot must fail outright -- the measured claim
	// this whole task exists to fix ("editing this kind changes nothing
	// about any boot until it is staged").
	{
		argv := []string{
			"boot", "t29-e2e-scratch",
			"--profile", bundleRoot,
			"--boot-root", bootRoot,
			"--session", "before-apply",
			"--json",
		}
		_, _, err := boot.Invoke(context.Background(), runner, argv)
		if err == nil {
			t.Fatal("cairn boot succeeded before anything was staged; want it to fail (prompts_dir/hello.md does not exist yet)")
		}
		t.Logf("boot before Apply failed as expected (nothing staged yet): %v", err)
	}

	// --- Step 1: Apply for real, stage the ORIGINAL content ---
	if _, err := apply.Invoke(context.Background(), apply.ExecRunner(), bundleRoot, agentsHome); err != nil {
		t.Fatalf("apply.Invoke (stage original): %v", err)
	}
	if got := bootOnce("after-first-apply"); got != original {
		t.Fatalf("planted prompt after first Apply = %q; want the original content %q", got, original)
	}

	// --- Step 2: edit the bundle's prompt; enablement must light up ---
	const edited = "# hello\n\nThis is the EDITED planted content -- T29 end-to-end proof.\n"
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), edited)

	mid, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare (after edit, before re-Apply): %v", err)
	}
	if !mid.Differs {
		t.Fatalf("Compare (after edit) = %+v; want Differs=true", mid)
	}
	if !strings.Contains(mid.Description, "prompt") {
		t.Errorf("Compare (after edit).Description = %q; want it to mention the prompt kind", mid.Description)
	}

	// --- Step 3: Apply again, for real; enablement must go dark ---
	if _, err := apply.Invoke(context.Background(), apply.ExecRunner(), bundleRoot, agentsHome); err != nil {
		t.Fatalf("apply.Invoke (stage edited): %v", err)
	}
	after, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare (after re-Apply): %v", err)
	}
	if after.Differs {
		t.Fatalf("Compare (after re-Apply) = %+v; want Differs=false", after)
	}

	// --- Step 4: boot again (a fresh session) and read the planted file:
	// it must carry the EDITED content now. ---
	if got := bootOnce("after-second-apply"); got != edited {
		t.Fatalf("planted prompt after the edit + re-Apply = %q; want the EDITED content %q", got, edited)
	}
}
