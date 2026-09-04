package boot_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// TestEditIsVisibleToTheNextBoot proves the active bundle is the source for
// both authoring and Cairn. Editing a prompt in a scratch bundle is visible to
// the next fresh boot immediately, with no installed or staged copy between
// them. Nothing here reads or writes the real agent-setup checkout or
// ~/.config/agents.
func TestEditIsVisibleToTheNextBoot(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping the end-to-end check: %v", err)
	}

	bundleRoot := t.TempDir()
	bootRoot := t.TempDir()

	const original = "# hello\n\nThis is the ORIGINAL planted content.\n"
	writeBundleFile(t, filepath.Join(bundleRoot, "prompts", "hello.md"), original)

	profile := "---\n" +
		"id: direct-bundle-e2e\n" +
		"name: Direct bundle end-to-end\n" +
		"provider: claude\n" +
		"spec:\n" +
		"  prompts_dir: $CAIRN_PROFILE_ROOT/prompts\n" +
		"  prompts:\n" +
		"    - hello\n" +
		"---\n"
	writeBundleFile(t, filepath.Join(bundleRoot, "profiles", "direct-bundle-e2e.md"), profile)

	runner := boot.ExecRunner(cairnPath)
	bootOnce := func(session string) string {
		t.Helper()
		argv := []string{
			"boot", "direct-bundle-e2e",
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

	if got := bootOnce("before-edit"); got != original {
		t.Fatalf("planted prompt before edit = %q; want %q", got, original)
	}

	const edited = "# hello\n\nThis is the EDITED planted content.\n"
	writeBundleFile(t, filepath.Join(bundleRoot, "prompts", "hello.md"), edited)

	if got := bootOnce("after-edit"); got != edited {
		t.Fatalf("planted prompt after edit = %q; want %q", got, edited)
	}
}

func writeBundleFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s's parent: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
