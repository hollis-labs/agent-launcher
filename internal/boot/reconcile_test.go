package boot_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
)

// liveBundle is the real agent-setup checkout these tests read. Nothing here
// writes into it: --profile only reads, and every path written lives under a
// t.TempDir().
func liveBundle(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	bundle := filepath.Join(home, "dev", "projects", "agent-setup")
	if _, err := os.Stat(bundle); err != nil {
		t.Skipf("no bundle at %s (agent-setup not present on this machine)", bundle)
	}
	// A bundle that IS present but does not hold the profile this test
	// names is a break to report, not to skip. That distinction is the
	// whole value of these live tests: the binding-shaped versions of them
	// are what caught agent-setup retiring bindings/ out from under
	// Tachyon, and they caught it by failing loudly rather than skipping.
	profile := filepath.Join(bundle, "profiles", "engineer.md")
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("the bundle at %s is present but has no profiles/engineer.md (%v) -- update this test's target, or the bundle", bundle, err)
	}
	return bundle
}

// liveLaunchProfile writes a launch profile into a scratch directory and
// returns its path, ready to hand cairn as --with.
//
// It declares a provider, which is the one key it must carry: no profile in
// agent-setup names one since 2026-09-10, and cairn refuses to render
// without one rather than writing one harness's files into another's
// directory.
func liveLaunchProfile(t *testing.T, name, provider string) string {
	t.Helper()
	st := launchprofile.Open(t.TempDir())
	p, err := st.Create(name, launchprofile.Scaffold(name, provider))
	if err != nil {
		t.Fatalf("writing the launch profile: %v", err)
	}
	return p.Path
}

// TestKeyReconcilesWithRealCairnPlant is CW-20260903-0014 comment 2537's
// empirical check, made a permanent regression guard: [boot.Key] and
// [boot.SessionKey] together must derive exactly the path the real Cairn
// binary plants into. If they disagreed, [boot.SessionPath] would name a
// directory Cairn never writes: Prepare would move aside a path nothing
// plants into, while the real boot directory silently accumulated a fresh
// ~/.claude.json trust entry per launch under a different name -- defeating
// the one thing the stable path exists for, with no error anywhere.
//
// It shells out to the real cairn binary against the real bundle rather than
// reasoning about the match from two independent constructions of the same
// expected string -- comment 2537 asked for exactly that distinction: "a
// comparison computed from Key() on one side and the actual planted path on
// the other -- not two constructions of the same expected string, which is a
// second copy that happens to agree."
//
// It now covers BOTH segments. Cairn plants at <boot-root>/<target>/
// <session>, and until launch profiles existed Tachyon fixed the session to
// the literal "current", so only the middle segment could drift. The session
// segment is now derived too, which is a second place the two sides can
// disagree and the one this test was extended to cover.
func TestKeyReconcilesWithRealCairnPlant(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping the real-binary reconciliation check: %v", err)
	}
	bundle := liveBundle(t)
	scratchRoot := t.TempDir()
	scope := t.TempDir()
	launchPath := liveLaunchProfile(t, "reconcile", "claude")

	const target = "engineer"
	session := boot.SessionKey("reconcile")
	projectRoot := filepath.Join(scratchRoot, boot.ProjectKey(scope))

	runner := boot.ExecRunner(cairnPath)
	argv := []string{
		"boot", target,
		"--profile", bundle,
		"--boot-root", projectRoot,
		"--session", session,
		"--with", launchPath,
		"--scope", scope,
		"--json",
	}

	result, stderr, err := boot.Invoke(context.Background(), runner, argv)
	if err != nil {
		t.Fatalf("real `cairn %v` failed: %v\nstderr:\n%s", argv, err, stderr)
	}

	want := boot.SessionPath(projectRoot, boot.Key(target), session)
	if result.BootDir != want {
		t.Fatalf(
			"MISMATCH between boot.Key()/SessionKey()/SessionPath and where cairn actually planted -- "+
				"this is a design question, not something to patch around here "+
				"(see comment 2537 on CW-20260903-0014):\n"+
				"  boot.SessionPath(projectRoot, boot.Key(%q), %q) = %q\n"+
				"  cairn boot --json reported boot_dir            = %q",
			target, session, want, result.BootDir,
		)
	}

	if info, statErr := os.Stat(result.BootDir); statErr != nil {
		t.Fatalf("cairn reported boot_dir %q but nothing exists there: %v", result.BootDir, statErr)
	} else if !info.IsDir() {
		t.Fatalf("cairn reported boot_dir %q but it is not a directory", result.BootDir)
	}
}

// TestLaunchProfileSuppliesTheProvider is the seam the whole launch-profile
// design rests on, checked against the real binary: a part passed by PATH,
// from outside the bundle, resolves the provider through cairn's own
// cascade. If this stops holding, every launch refuses -- no profile in
// agent-setup declares a provider, and `--provider` is no longer passed.
func TestLaunchProfileSuppliesTheProvider(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH: %v", err)
	}
	bundle := liveBundle(t)

	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			scratchRoot := t.TempDir()
			scope := t.TempDir()
			launchPath := liveLaunchProfile(t, "prov", provider)

			result, stderr, err := boot.Invoke(context.Background(), boot.ExecRunner(cairnPath), []string{
				"boot", "engineer",
				"--profile", bundle,
				"--boot-root", filepath.Join(scratchRoot, boot.ProjectKey(scope)),
				"--session", boot.SessionKey("prov"),
				"--with", launchPath,
				"--scope", scope,
				"--json",
			})
			if err != nil {
				t.Fatalf("cairn refused a boot whose only provider came from the launch profile: %v\nstderr:\n%s", err, stderr)
			}
			if result.Provider != provider {
				t.Fatalf("cairn reported provider %q; want %q, which only the launch profile declared", result.Provider, provider)
			}
		})
	}
}

// TestSeededCodexProfileRendersThePostureKeys is the live half of
// TestCodexSeedCarriesTheTwoKeys: the seed's keys must survive cairn's
// cascade into the rendered config.toml, beside the writable_roots cairn
// computes from the scope.
//
// Without them a Codex session boots and then cannot write to its own scope
// -- CODEX_HOME points at the boot directory, so Codex reads THAT
// config.toml, inherits none of ~/.codex's per-project trust, falls back to
// a read-only sandbox and refuses the --add-dir grant. See Torque
// CW-20260910-0038 and Tesseract codex_boot_sandbox_posture.
func TestSeededCodexProfileRendersThePostureKeys(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH: %v", err)
	}
	bundle := liveBundle(t)
	scratchRoot := t.TempDir()
	scope := t.TempDir()

	// The real seed, not a hand-written stand-in: this test is worth
	// nothing if it renders a fixture the app does not ship.
	st := launchprofile.Open(t.TempDir())
	if err := launchprofile.EnsureSeeds(st, nil); err != nil {
		t.Fatalf("EnsureSeeds: %v", err)
	}
	seeded, err := st.Get("codex")
	if err != nil {
		t.Fatalf("Get(codex): %v", err)
	}

	result, stderr, err := boot.Invoke(context.Background(), boot.ExecRunner(cairnPath), []string{
		"boot", "engineer",
		"--profile", bundle,
		"--boot-root", filepath.Join(scratchRoot, boot.ProjectKey(scope)),
		"--session", boot.SessionKey("codex"),
		"--with", seeded.Path,
		"--scope", scope,
		"--json",
	})
	if err != nil {
		t.Fatalf("booting the seeded codex launch profile failed: %v\nstderr:\n%s", err, stderr)
	}
	if result.SettingsPath == nil {
		t.Fatal("cairn reported no settings path for a codex boot; there is nowhere for the posture keys to be")
	}

	data, err := os.ReadFile(*result.SettingsPath)
	if err != nil {
		t.Fatalf("reading the rendered %s: %v", *result.SettingsPath, err)
	}
	got := string(data)

	for _, want := range []string{"approval_policy = 'never'", "sandbox_mode = 'workspace-write'"} {
		if !strings.Contains(got, want) {
			t.Errorf("the rendered config.toml does not carry %s:\n%s", want, got)
		}
	}
	// The grant cairn computes must survive beside them -- sandbox_mode
	// widens nothing on its own, it makes this grant take effect.
	if !strings.Contains(got, "writable_roots") {
		t.Errorf("the rendered config.toml lost cairn's own writable_roots grant:\n%s", got)
	}
}
