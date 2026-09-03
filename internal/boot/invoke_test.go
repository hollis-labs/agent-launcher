package boot_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// fakeRunner returns a [boot.Runner] that ignores argv and hands back the
// given stdout, stderr and err — every test in this file but the one real
// integration test (reconcile_test.go) drives [boot.Invoke] through one of
// these instead of shelling out, per the acceptance bullet that the actual
// subprocess call must be injectable/mockable.
func fakeRunner(stdout, stderr []byte, err error) boot.Runner {
	return func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		return stdout, stderr, err
	}
}

// realBootReportFixture is the literal stdout captured from a real
// invocation, run for this task: `cairn boot eng-nanite --profile
// ~/dev/projects/agent-setup --boot-root /tmp/tachyon-t10-scratch-boot
// --session current --json` against the installed cairn binary
// (/Users/chrispian/go/bin/cairn) on 2026-09-03. Decoding tests below are
// grounded in this actual document, not an assumed shape.
const realBootReportFixture = `{
  "boot_dir": "/tmp/tachyon-t10-scratch-boot/eng-nanite/current",
  "provider": "claude",
  "scope": "/Users/chrispian/dev/hollis-labs/apps/nanite",
  "settings_path": "/tmp/tachyon-t10-scratch-boot/eng-nanite/current/.claude/settings.json",
  "cwd_preference": "boot_dir",
  "project_dir_arg": [
    "--add-dir",
    "{{.ProjectDir}}"
  ]
}
`

// --- Invoke ------------------------------------------------------------

// TestInvoke_DecodesRealCapturedReport proves Result decodes the exact
// document a real cairn boot --json run produced, field for field.
func TestInvoke_DecodesRealCapturedReport(t *testing.T) {
	runner := fakeRunner([]byte(realBootReportFixture), nil, nil)

	result, stderr, err := boot.Invoke(context.Background(), runner, []string{"boot", "eng-nanite", "--json"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(stderr) != 0 {
		t.Errorf("stderr = %q; want empty", stderr)
	}

	if result.BootDir != "/tmp/tachyon-t10-scratch-boot/eng-nanite/current" {
		t.Errorf("BootDir = %q", result.BootDir)
	}
	if result.Provider != "claude" {
		t.Errorf("Provider = %q; want %q", result.Provider, "claude")
	}
	if result.Scope == nil || *result.Scope != "/Users/chrispian/dev/hollis-labs/apps/nanite" {
		t.Errorf("Scope = %v; want a pointer to the nanite scope", result.Scope)
	}
	if result.SettingsPath == nil || *result.SettingsPath != "/tmp/tachyon-t10-scratch-boot/eng-nanite/current/.claude/settings.json" {
		t.Errorf("SettingsPath = %v", result.SettingsPath)
	}
	if result.CwdPreference != "boot_dir" {
		t.Errorf("CwdPreference = %q; want %q", result.CwdPreference, "boot_dir")
	}
	wantArg := []string{"--add-dir", "{{.ProjectDir}}"}
	if len(result.ProjectDirArg) != len(wantArg) || result.ProjectDirArg[0] != wantArg[0] || result.ProjectDirArg[1] != wantArg[1] {
		t.Errorf("ProjectDirArg = %v; want %v", result.ProjectDirArg, wantArg)
	}
}

// TestInvoke_NullFieldsDecodeAsNilNotZeroValue covers the case
// examples/README.md §5 calls out explicitly: a profile that grants a
// directory it is not scoped to renders settings_path non-null with scope
// null. Scope must decode as a nil pointer, not a pointer to "" — Cairn's
// contract says null and "" mean different things, and collapsing them
// would silently turn "no scope" into "scope is the empty string."
func TestInvoke_NullFieldsDecodeAsNilNotZeroValue(t *testing.T) {
	doc := `{
  "boot_dir": "/scratch/boot/some-profile/current",
  "provider": "claude",
  "scope": null,
  "settings_path": "/scratch/boot/some-profile/current/.claude/settings.json",
  "cwd_preference": "boot_dir",
  "project_dir_arg": ["--add-dir", "{{.ProjectDir}}"]
}`
	runner := fakeRunner([]byte(doc), nil, nil)
	result, _, err := boot.Invoke(context.Background(), runner, nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Scope != nil {
		t.Fatalf("Scope = %v; want nil (null on the wire)", result.Scope)
	}
	if result.SettingsPath == nil {
		t.Fatalf("SettingsPath = nil; want non-nil per the fixture")
	}
	// Scope nil must make ProjectDirArgv nil even though ProjectDirArg
	// itself is populated -- there is nothing to substitute it with.
	if got := result.ProjectDirArgv(); got != nil {
		t.Errorf("ProjectDirArgv() = %v; want nil when Scope is nil", got)
	}
}

// TestInvoke_SurfacesStderrOnSuccessWithoutFailing is D8 made concrete for
// this package: a slot that warns on stderr with cairn still exiting 0
// must not fail Invoke, and that stderr must still reach the caller to
// surface, per the acceptance bullet "Cairn's stderr is surfaced to the
// user and does not fail the launch."
func TestInvoke_SurfacesStderrOnSuccessWithoutFailing(t *testing.T) {
	warn := []byte("warning: slot \"missing-thing\" did not resolve\n")
	runner := fakeRunner([]byte(realBootReportFixture), warn, nil)

	result, stderr, err := boot.Invoke(context.Background(), runner, nil)
	if err != nil {
		t.Fatalf("Invoke returned an error for a non-empty stderr with exit 0: %v", err)
	}
	if string(stderr) != string(warn) {
		t.Errorf("stderr = %q; want %q surfaced unchanged", stderr, warn)
	}
	if result.BootDir == "" {
		t.Errorf("Result was not decoded even though the run succeeded")
	}
}

// TestInvoke_NonZeroExitReportsStderrNotGenericError is the acceptance
// bullet "a non-zero exit from Cairn reports the stderr rather than a
// generic failure," proven directly against the returned error's message
// and its Unwrap chain.
func TestInvoke_NonZeroExitReportsStderrNotGenericError(t *testing.T) {
	underlying := errors.New("exit status 1")
	stderrText := "cairn: profile \"nonexistent\" not found"
	runner := fakeRunner(nil, []byte(stderrText), underlying)

	argv := []string{"boot", "nonexistent", "--json"}
	_, stderr, err := boot.Invoke(context.Background(), runner, argv)
	if err == nil {
		t.Fatal("Invoke returned no error for a non-zero exit")
	}
	if string(stderr) != stderrText {
		t.Errorf("returned stderr = %q; want %q", stderr, stderrText)
	}
	if !strings.Contains(err.Error(), stderrText) {
		t.Errorf("error message = %q; want it to contain cairn's actual stderr %q, not a generic failure", err.Error(), stderrText)
	}

	var invokeErr *boot.InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("error is not a *boot.InvokeError: %T (%v)", err, err)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("errors.Is(err, underlying) = false; InvokeError.Unwrap must expose the runner's own error")
	}
	if string(invokeErr.Stderr) != stderrText {
		t.Errorf("InvokeError.Stderr = %q; want %q", invokeErr.Stderr, stderrText)
	}
}

// TestInvoke_MalformedJSONIsARealErrorNoScrapeFallback is "No AGENTS.md
// scrape... if --json is somehow unavailable at runtime, that's a real
// error to surface" made concrete: stdout that is not the --json contract
// (e.g. --json silently unsupported by whatever ran) must surface as an
// error from Invoke, never silently produce a zero-value Result that a
// caller might treat as "no scope, proceed anyway."
func TestInvoke_MalformedJSONIsARealErrorNoScrapeFallback(t *testing.T) {
	runner := fakeRunner([]byte("/plain/bare/path\n"), nil, nil)
	_, _, err := boot.Invoke(context.Background(), runner, nil)
	if err == nil {
		t.Fatal("Invoke returned no error for stdout that is not the --json contract")
	}
}

// --- ProjectDirArgv / Result.ProjectDirArgv -----------------------------

// TestProjectDirArgv_SplitFirstSubstituteSecond is the acceptance bullet
// "a test proving an array-shaped project_dir_arg is split before
// substitution -- a scope path containing a space yields the right argc,"
// reproducing comment 2480's own measurement: the array form yields
// argc=2 against a scope named "r&d <x> with space," where
// substitute-then-split yields argc=5. Both numbers are asserted here, so
// the second failing (i.e. someone "fixing" the naive approach so it no
// longer breaks) would itself be caught as a change to this test's own
// premise.
func TestProjectDirArgv_SplitFirstSubstituteSecond(t *testing.T) {
	tokens := []string{"--add-dir", boot.ProjectDirPlaceholder}
	const scopeWithSpace = "r&d <x> with space"

	got := boot.ProjectDirArgv(tokens, scopeWithSpace)
	if len(got) != 2 {
		t.Fatalf("ProjectDirArgv (split-first) argc = %d %v; want 2", len(got), got)
	}
	if got[0] != "--add-dir" {
		t.Errorf("got[0] = %q; want %q", got[0], "--add-dir")
	}
	if got[1] != scopeWithSpace {
		t.Errorf("got[1] = %q; want the whole scope as one argument: %q", got[1], scopeWithSpace)
	}

	// The trap this function exists to avoid, reproduced for contrast: the
	// upstream go-providers examples substitute into the joined pattern
	// string and split afterwards. That recipe breaks on this exact input.
	pattern := strings.Join(tokens, " ") // "--add-dir {{.ProjectDir}}"
	naive := strings.Fields(strings.ReplaceAll(pattern, boot.ProjectDirPlaceholder, scopeWithSpace))
	if len(naive) != 5 {
		t.Fatalf("naive substitute-then-split argc = %d %v; want 5 (this is the trap comment 2480 measured, reproduced here as a control)", len(naive), naive)
	}
}

// TestProjectDirArgv_NilForNoTokens covers Cairn's own "this provider
// needs no such flag" spelling: nil, not an empty slice, for nil or empty
// tokens.
func TestProjectDirArgv_NilForNoTokens(t *testing.T) {
	if got := boot.ProjectDirArgv(nil, "/some/scope"); got != nil {
		t.Errorf("ProjectDirArgv(nil, ...) = %v; want nil", got)
	}
	if got := boot.ProjectDirArgv([]string{}, "/some/scope"); got != nil {
		t.Errorf("ProjectDirArgv([]string{}, ...) = %v; want nil", got)
	}
}

// TestResultProjectDirArgv_FlagNameFromJSONNotHardcoded is the acceptance
// bullet "the project-dir flag name comes from the JSON... a test with a
// fixture JSON carrying a non---add-dir value proves it is not hardcoded."
// The fixture below is a hypothetical provider's report -- not Claude
// Code's -- carrying a deliberately different flag spelling than
// "--add-dir", to prove ProjectDirArgv carries through whatever token
// Cairn actually reported rather than assuming Claude Code's own spelling.
func TestResultProjectDirArgv_FlagNameFromJSONNotHardcoded(t *testing.T) {
	doc := `{
  "boot_dir": "/scratch/boot/codex-profile/current",
  "provider": "codex",
  "scope": "/Users/chrispian/dev/projects/some project",
  "settings_path": null,
  "cwd_preference": "project_dir",
  "project_dir_arg": ["--grant-access", "{{.ProjectDir}}"]
}`
	var result boot.Result
	if err := json.Unmarshal([]byte(doc), &result); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	got := result.ProjectDirArgv()
	want := []string{"--grant-access", "/Users/chrispian/dev/projects/some project"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ProjectDirArgv() = %v; want %v (flag name read from the JSON, not hardcoded to --add-dir)", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "--add-dir") {
		t.Errorf("ProjectDirArgv() = %v; must not contain the hardcoded Claude Code spelling for a different provider's report", got)
	}
}

// TestResultProjectDirArgv_NilWhenScopeIsNil locks in the reference
// launcher's own guard (Cairn examples/README.md §5): a Result with a
// populated ProjectDirArg but a nil Scope must still produce nil, not a
// flag with an empty-string argument.
func TestResultProjectDirArgv_NilWhenScopeIsNil(t *testing.T) {
	result := boot.Result{
		ProjectDirArg: []string{"--add-dir", boot.ProjectDirPlaceholder},
		Scope:         nil,
	}
	if got := result.ProjectDirArgv(); got != nil {
		t.Errorf("ProjectDirArgv() = %v; want nil when Scope is nil", got)
	}
}

// --- HarnessArgv ---------------------------------------------------------

// TestHarnessArgv_AlwaysIncludesSettingsFlag is the acceptance bullet "a
// test asserting --settings <bootdir>/.claude/settings.json is in every
// built argv. This test is permanent; no task removes it." It is
// table-driven over several otherwise-different Results, including one
// whose SettingsPath is nil, to prove the flag is computed from BootDir
// directly and not read from (or gated on) Result.SettingsPath.
//
// PERMANENT: this test must never be deleted or weakened. Its failure
// means a future change silently downgrades every launch's
// defaultMode: auto -- see CW-20260903-0014's hazard section and
// HarnessArgv's own doc comment.
func TestHarnessArgv_AlwaysIncludesSettingsFlag(t *testing.T) {
	cases := []struct {
		name   string
		result boot.Result
	}{
		{
			name: "ordinary result with a non-nil settings path",
			result: boot.Result{
				BootDir:      "/state/boot/eng-nanite/current",
				SettingsPath: strPtr("/state/boot/eng-nanite/current/.claude/settings.json"),
			},
		},
		{
			name: "SettingsPath nil -- HarnessArgv must not read it, and must still emit --settings",
			result: boot.Result{
				BootDir:      "/state/boot/bare-profile/current",
				SettingsPath: nil,
			},
		},
		{
			name: "scope and project dir arg present too",
			result: boot.Result{
				BootDir:       "/state/boot/scoped/current",
				Scope:         strPtr("/Users/chrispian/dev/projects/agent-setup"),
				ProjectDirArg: []string{"--add-dir", boot.ProjectDirPlaceholder},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv := boot.HarnessArgv(tc.result)
			want := filepath.Join(tc.result.BootDir, ".claude", "settings.json")

			found := false
			for i, tok := range argv {
				if tok == "--settings" {
					if i+1 >= len(argv) {
						t.Fatalf("argv %v: --settings has no following value", argv)
					}
					if argv[i+1] != want {
						t.Fatalf("argv %v: --settings value = %q; want %q", argv, argv[i+1], want)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("argv %v does not contain --settings at all", argv)
			}
		})
	}
}

// TestHarnessArgv_NeverEmitsProjectDirFlag locks in "do not emit --add-dir
// (retired for Tachyon)": even when Result.ProjectDirArg is populated,
// HarnessArgv must never add it (or any project-dir flag) to the launch
// argv, because --settings alone already grants
// permissions.additionalDirectories.
func TestHarnessArgv_NeverEmitsProjectDirFlag(t *testing.T) {
	result := boot.Result{
		BootDir:       "/state/boot/eng-nanite/current",
		Scope:         strPtr("/Users/chrispian/dev/hollis-labs/apps/nanite"),
		ProjectDirArg: []string{"--add-dir", boot.ProjectDirPlaceholder},
	}
	argv := boot.HarnessArgv(result)
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "--add-dir") {
		t.Fatalf("HarnessArgv(%+v) = %v; must never contain --add-dir", result, argv)
	}
	if len(argv) != 2 {
		t.Fatalf("HarnessArgv(%+v) = %v; want exactly [--settings, <path>] with no project-dir flag", result, argv)
	}
}

// --- test helpers ---------------------------------------------------------

func strPtr(s string) *string { return &s }
