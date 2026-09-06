package boot_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// codexScene builds the two directories a Codex launch involves — the
// operator's own provider home, and the boot directory Cairn just planted —
// and returns a Result shaped like the real captured report but pointing at
// them. Nothing here writes a credential: auth.json is a marker file, and
// PrepareHomeResources never opens what it links anyway.
func codexScene(t *testing.T, resources ...string) (result boot.Result, sourceHome string) {
	t.Helper()
	root := t.TempDir()
	sourceHome = filepath.Join(root, "codex-home")
	bootDir := filepath.Join(root, "boot", "codex-coord-agent-setup", "current")

	if err := os.MkdirAll(filepath.Join(sourceHome, "hooks"), 0o755); err != nil {
		t.Fatalf("creating the source home: %v", err)
	}
	for _, name := range []string{"auth.json", "hooks.json"} {
		if err := os.WriteFile(filepath.Join(sourceHome, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(bootDir, 0o755); err != nil {
		t.Fatalf("creating the boot directory: %v", err)
	}

	if len(resources) == 0 {
		resources = []string{"auth.json", "hooks.json", "hooks"}
	}
	scope := filepath.Join(root, "scope")
	return boot.Result{
		BootDir:           bootDir,
		Provider:          boot.ProviderCodex,
		Scope:             &scope,
		CwdPreference:     "boot_dir",
		EnvAmendments:     []string{"CODEX_HOME=" + boot.BootDirPlaceholder},
		HomeResourcePaths: resources,
	}, sourceHome
}

// TestPrepareHomeResources_LinksEveryResourceCairnNamed is the ordinary
// Codex case: the three resources Cairn reports become three links in the
// boot directory, pointing at the operator's own files.
func TestPrepareHomeResources_LinksEveryResourceCairnNamed(t *testing.T) {
	result, sourceHome := codexScene(t)

	prepared, err := boot.PrepareHomeResources(result, sourceHome)
	if err != nil {
		t.Fatalf("PrepareHomeResources: %v", err)
	}
	want := []string{
		filepath.Join(result.BootDir, "auth.json"),
		filepath.Join(result.BootDir, "hooks.json"),
		filepath.Join(result.BootDir, "hooks"),
	}
	if !slices.Equal(prepared, want) {
		t.Fatalf("prepared = %v; want %v", prepared, want)
	}
	for i, dst := range want {
		info, err := os.Lstat(dst)
		if err != nil {
			t.Fatalf("%s: %v", dst, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink — it must never be a copy of live operator state", dst)
		}
		target, err := os.Readlink(dst)
		if err != nil {
			t.Fatalf("readlink %s: %v", dst, err)
		}
		if wantTarget := filepath.Join(sourceHome, result.HomeResourcePaths[i]); target != wantTarget {
			t.Errorf("%s -> %s; want %s", dst, target, wantTarget)
		}
	}
}

// TestPrepareHomeResources_NeverCopies pins the ownership boundary Cairn
// draws and Tachyon carries: the operator keeps one copy of their
// credentials and hook registrations, and a disposable boot directory never
// becomes a second one that goes stale on re-authentication.
func TestPrepareHomeResources_NeverCopies(t *testing.T) {
	result, sourceHome := codexScene(t)
	if _, err := boot.PrepareHomeResources(result, sourceHome); err != nil {
		t.Fatalf("PrepareHomeResources: %v", err)
	}

	// Change the operator's own file; the boot directory must see it,
	// which is only true of a link.
	updated := []byte(`{"rotated":true}` + "\n")
	if err := os.WriteFile(filepath.Join(sourceHome, "auth.json"), updated, 0o600); err != nil {
		t.Fatalf("rewriting the source: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(result.BootDir, "auth.json"))
	if err != nil {
		t.Fatalf("reading through the link: %v", err)
	}
	if string(got) != string(updated) {
		t.Fatalf("the boot directory holds a stale copy (%q), not a link", got)
	}
}

// TestPrepareHomeResources_IsIdempotent: relaunching into a boot directory
// that already carries the same links is not a conflict.
func TestPrepareHomeResources_IsIdempotent(t *testing.T) {
	result, sourceHome := codexScene(t)
	if _, err := boot.PrepareHomeResources(result, sourceHome); err != nil {
		t.Fatalf("first PrepareHomeResources: %v", err)
	}
	if _, err := boot.PrepareHomeResources(result, sourceHome); err != nil {
		t.Fatalf("second PrepareHomeResources: %v", err)
	}
}

// TestPrepareHomeResources_NothingForClaude: no reported resources means
// nothing to prepare and nothing to resolve, which is every Claude launch.
func TestPrepareHomeResources_NothingForClaude(t *testing.T) {
	prepared, err := boot.PrepareHomeResources(mustDecode(t, realClaudeBootReportFixture), "")
	if err != nil {
		t.Fatalf("PrepareHomeResources: %v", err)
	}
	if prepared != nil {
		t.Fatalf("prepared = %v; want nil for a provider with no home resources", prepared)
	}
}

// TestPrepareHomeResources_MissingResourceRefusesAndNamesIt is the decision
// this increment made explicitly: a launch that quietly proceeded without
// hooks.json would open a session whose hooks simply do not run, with
// nothing anywhere saying so. The refusal has to name the path and the way
// out.
func TestPrepareHomeResources_MissingResourceRefusesAndNamesIt(t *testing.T) {
	result, sourceHome := codexScene(t)
	if err := os.Remove(filepath.Join(sourceHome, "hooks.json")); err != nil {
		t.Fatalf("removing hooks.json: %v", err)
	}

	_, err := boot.PrepareHomeResources(result, sourceHome)
	if err == nil {
		t.Fatal("PrepareHomeResources launched without a resource cairn said the provider needs")
	}
	if !errors.Is(err, boot.ErrHomeResource) {
		t.Errorf("error does not wrap ErrHomeResource: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(sourceHome, "hooks.json")) {
		t.Errorf("refusal does not name the missing path: %v", err)
	}
}

// TestPrepareHomeResources_MissingResourceLeavesNothingBehind: everything is
// checked before anything is linked, so a refused preparation leaves a boot
// directory that is not half-prepared.
func TestPrepareHomeResources_MissingResourceLeavesNothingBehind(t *testing.T) {
	result, sourceHome := codexScene(t)
	// os.Remove, not a recursive delete: this package forbids the latter
	// outright (see TestPackageNeverCallsRemoveAll), and the fixture's
	// hooks/ is empty anyway.
	if err := os.Remove(filepath.Join(sourceHome, "hooks")); err != nil {
		t.Fatalf("removing hooks/: %v", err)
	}

	if _, err := boot.PrepareHomeResources(result, sourceHome); err == nil {
		t.Fatal("PrepareHomeResources did not refuse")
	}
	// auth.json comes first in the report and would have been linked by a
	// one-pass implementation before it reached the missing directory.
	if _, err := os.Lstat(filepath.Join(result.BootDir, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused preparation left auth.json linked: %v", err)
	}
}

// TestPrepareHomeResources_AllMissingPathsAreNamedAtOnce: one refusal, the
// whole checklist — not one failed launch per missing file.
func TestPrepareHomeResources_AllMissingPathsAreNamedAtOnce(t *testing.T) {
	result, sourceHome := codexScene(t)
	for _, name := range []string{"auth.json", "hooks.json"} {
		if err := os.Remove(filepath.Join(sourceHome, name)); err != nil {
			t.Fatalf("removing %s: %v", name, err)
		}
	}
	_, err := boot.PrepareHomeResources(result, sourceHome)
	if err == nil {
		t.Fatal("PrepareHomeResources did not refuse")
	}
	for _, name := range []string{"auth.json", "hooks.json"} {
		if !strings.Contains(err.Error(), filepath.Join(sourceHome, name)) {
			t.Errorf("refusal does not name %s: %v", name, err)
		}
	}
}

// TestPrepareHomeResources_RefusesToReplaceARealFile: replacing something
// Cairn rendered, at a path the provider is about to read, is destructive in
// the one direction that cannot be undone.
func TestPrepareHomeResources_RefusesToReplaceARealFile(t *testing.T) {
	result, sourceHome := codexScene(t)
	occupied := filepath.Join(result.BootDir, "hooks.json")
	if err := os.WriteFile(occupied, []byte("rendered by something else\n"), 0o600); err != nil {
		t.Fatalf("occupying the destination: %v", err)
	}

	_, err := boot.PrepareHomeResources(result, sourceHome)
	if err == nil {
		t.Fatal("PrepareHomeResources replaced a real file")
	}
	if !strings.Contains(err.Error(), occupied) {
		t.Errorf("refusal does not name the occupied path: %v", err)
	}
	got, readErr := os.ReadFile(occupied)
	if readErr != nil {
		t.Fatalf("reading the occupied path: %v", readErr)
	}
	if string(got) != "rendered by something else\n" {
		t.Fatalf("the occupied file was modified: %q", got)
	}
}

// TestPrepareHomeResources_RefusesToRepointAnExistingLink: a link already
// pointing somewhere else is a conflict, not something to silently fix.
func TestPrepareHomeResources_RefusesToRepointAnExistingLink(t *testing.T) {
	result, sourceHome := codexScene(t)
	elsewhere := filepath.Join(t.TempDir(), "someone-elses-auth.json")
	if err := os.WriteFile(elsewhere, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("creating the other target: %v", err)
	}
	dst := filepath.Join(result.BootDir, "auth.json")
	if err := os.Symlink(elsewhere, dst); err != nil {
		t.Fatalf("planting the conflicting link: %v", err)
	}

	if _, err := boot.PrepareHomeResources(result, sourceHome); err == nil {
		t.Fatal("PrepareHomeResources repointed an existing link")
	}
	target, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != elsewhere {
		t.Fatalf("the existing link was repointed to %s", target)
	}
}

// TestPrepareHomeResources_RefusesASourceHomeThatIsTheBootDirectory is the
// failure mode of resolving the source home too late — after the launch
// environment already says CODEX_HOME=<bootdir>. Every resource would link
// to itself.
func TestPrepareHomeResources_RefusesASourceHomeThatIsTheBootDirectory(t *testing.T) {
	result, _ := codexScene(t)
	for _, home := range []string{result.BootDir, filepath.Join(result.BootDir, "nested")} {
		if _, err := boot.PrepareHomeResources(result, home); err == nil {
			t.Errorf("PrepareHomeResources accepted a source home of %s", home)
		}
	}
	if _, err := os.Lstat(filepath.Join(result.BootDir, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a self-link was created: %v", err)
	}
}

// TestPrepareHomeResources_RefusesASourceHomeReachedThroughALink covers the
// same guard when the two paths are only equal after resolution.
func TestPrepareHomeResources_RefusesASourceHomeReachedThroughALink(t *testing.T) {
	result, _ := codexScene(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(result.BootDir, alias); err != nil {
		t.Fatalf("creating the alias: %v", err)
	}
	if _, err := boot.PrepareHomeResources(result, alias); err == nil {
		t.Fatal("PrepareHomeResources accepted a source home that resolves to the boot directory")
	}
}

// TestPrepareHomeResources_RefusesAResourceNameThatEscapesTheBootDirectory:
// the destinations stay inside the boot directory, whatever a report says.
func TestPrepareHomeResources_RefusesAResourceNameThatEscapesTheBootDirectory(t *testing.T) {
	for _, name := range []string{"../auth.json", "/etc/auth.json", "sub/../../auth.json"} {
		result, sourceHome := codexScene(t, name)
		if _, err := boot.PrepareHomeResources(result, sourceHome); err == nil {
			t.Errorf("PrepareHomeResources accepted the resource name %q", name)
		}
	}
}

// TestPrepareHomeResources_RefusesWithNoSourceHome: a provider that reports
// resources but no resolvable home is a refusal, never a best-effort launch.
func TestPrepareHomeResources_RefusesWithNoSourceHome(t *testing.T) {
	result, _ := codexScene(t)
	if _, err := boot.PrepareHomeResources(result, ""); err == nil {
		t.Fatal("PrepareHomeResources proceeded with no source home")
	}
}
