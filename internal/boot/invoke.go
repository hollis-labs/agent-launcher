package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// The providers Tachyon knows how to launch. They are the values Cairn's
// own --json report puts in provider, and the only two bootdir.LayoutFor
// renders a directory for. A third is a third case in [HarnessArgv] and a
// third entry in internal/launch's harnessBinary map — not a new code path.
const (
	// ProviderClaude is Claude Code.
	ProviderClaude = "claude"
	// ProviderCodex is the Codex CLI.
	ProviderCodex = "codex"
)

// ProjectDirPlaceholder is the token Cairn's --json report leaves standing
// in project_dir_arg wherever the scope goes — see [Result.ProjectDirArgv]
// and [ProjectDirArgv]. Named to match cairn's own
// cmd/cairn/bootjson.go:projectDirPlaceholder.
const ProjectDirPlaceholder = "{{.ProjectDir}}"

// Result is what `cairn boot --json` prints, decoded: everything a
// launcher needs to open the directory Cairn just planted and build the
// harness's argv, without reading any file inside that directory to find
// out. It mirrors Cairn's own bootReport (cmd/cairn/bootjson.go in the
// cairn repo, and examples/README.md §5): flat, snake_case, every key
// present on every successful boot. Cairn's contract is that new keys are
// free and a rename is breaking, so this struct names the keys Tachyon
// reads and silently ignores the ones it does not (profile_root describes
// the bundle a boot was composed out of, which this launcher already knows
// because it passed it).
//
// Scope, SettingsPath and ProjectDirArg are nil-able (a pointer or a nil
// slice) because Cairn's own contract makes null meaningful and distinct
// from the zero value of the underlying type: Scope nil means no --scope
// was given, because no project was selected; SettingsPath
// nil means the render produced no file at the harness's settings path (a
// real case — a profile with no spec.settings and nothing to grant
// produces none); ProjectDirArg nil means this provider needs no flag at
// all to grant a directory. None of the three is ever "" or [] on the
// wire, and decoding into these types preserves that distinction rather
// than collapsing null into a zero value indistinguishable from "Cairn had
// nothing to say."
type Result struct {
	// BootDir is the directory Cairn just planted, absolute.
	BootDir string `json:"boot_dir"`

	// Provider is the harness the directory was rendered for:
	// [ProviderClaude] or [ProviderCodex], the two Cairn renders a layout
	// for. Cairn's own contract held — a second provider needed no change
	// to this field, only a different value in it — and [HarnessArgv] is
	// where that value is turned into a command line.
	Provider string `json:"provider"`

	// Scope is the directory the instance works in, absolute and
	// symlink-resolved, or nil when no --scope was given.
	Scope *string `json:"scope"`

	// SettingsPath is the absolute path of the file at the harness's
	// settings path, or nil when the render produced none. [HarnessArgv]
	// prefers it over its own join, and falls back when it is nil — see
	// that function's doc for why the preference and the fallback answer
	// two different questions.
	SettingsPath *string `json:"settings_path"`

	// CwdPreference is where the harness expects to be invoked: "boot_dir"
	// or "project_dir". Resolving it into an actual working directory for
	// the spawned process is the caller's decision (T12,
	// CW-20260903-0016), not this package's — Cairn itself declines to
	// make that choice on a launcher's behalf, and this package follows
	// suit.
	CwdPreference string `json:"cwd_preference"`

	// ProjectDirArg is the provider's own flag for granting access to
	// Scope, already split into argv tokens by Cairn with
	// [ProjectDirPlaceholder] left standing in whichever token carries it
	// — ["--add-dir", "{{.ProjectDir}}"] for Claude Code — or nil when the
	// provider needs no such flag. The flag name is data, read from this
	// field; nothing in this package hardcodes "--add-dir". See
	// [Result.ProjectDirArgv] and [ProjectDirArgv].
	ProjectDirArg []string `json:"project_dir_arg"`

	// EnvAmendments are the provider-declared KEY=VALUE entries a launcher
	// must add to the environment of the process it spawns, with Cairn's
	// own placeholders ([BootDirPlaceholder], [ProjectDirPlaceholder]) left
	// standing for the launcher to substitute at spawn time — exactly the
	// same split-there/substitute-here division ProjectDirArg follows. nil
	// when the provider declares none, which is Claude Code's case;
	// Codex declares one, "CODEX_HOME={{.BootDir}}". [Environment] performs
	// the substitution; internal/boot.SpawnITerm2 puts the result in front
	// of the command the terminal actually runs.
	EnvAmendments []string `json:"env_amendments"`

	// HomeResourcePaths are provider-home-relative resources Cairn
	// deliberately does not render into the boot directory, but that a
	// launcher pointing the provider's home at that directory (which is
	// what following EnvAmendments does) must provide before launching.
	// nil when Cairn knows of none. Codex reports auth.json, hooks.json and
	// hooks: live credentials and live hook registrations, which Cairn
	// refuses to copy into a disposable directory because copying them
	// would make Cairn the owner of operator state. [PrepareHomeResources]
	// is where Tachyon carries that ownership boundary rather than erasing
	// it — it links, and never copies.
	HomeResourcePaths []string `json:"home_resource_paths"`
}

// ProjectDirArgv substitutes projectDir into every token of tokens and
// returns the result, without ever joining tokens into one string and
// re-splitting it. tokens is exactly a Result's ProjectDirArg — Cairn's
// own array of un-substituted argv tokens; this function performs only the
// second half of the two-step contract Cairn's --json documents (split
// there, substitute here).
//
// Substituting into a joined string and splitting the joined result
// afterwards is the trap this function exists to make structurally
// impossible: a projectDir containing a space — comment 2480 on
// CW-20260903-0014 measured a scope named "r&d <x> with space" — silently
// turns one argument into several (argc=5, against argc=2 for the array
// form). Operating token-by-token, as here, cannot: substituting inside
// one already-delimited element can never create or remove an argv
// boundary.
//
// tokens' first element is the provider's own flag spelling ("--add-dir"
// for Claude Code) — ProjectDirArgv never assumes or hardcodes it, and
// simply carries it through. It returns nil, not an empty slice, for a nil
// or empty tokens — Cairn's own spelling of "this provider needs no such
// flag."
func ProjectDirArgv(tokens []string, projectDir string) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]string, len(tokens))
	for i, tok := range tokens {
		out[i] = strings.ReplaceAll(tok, ProjectDirPlaceholder, projectDir)
	}
	return out
}

// ProjectDirArgv substitutes r's own ProjectDirArg with r's own Scope,
// following the reference launcher in Cairn's examples/README.md §5
// exactly: nil, not an empty slice, when either the provider declared no
// flag (ProjectDirArg is empty) or there is no scope to substitute in
// (Scope is nil). A nil Scope is deliberately not treated as "substitute
// the empty string" — an empty projectDir would still produce a
// well-formed but meaningless flag, and Cairn's own contract already has a
// way to say "nothing to grant": Scope being nil.
//
// [HarnessArgv] calls this for Codex and not for Claude Code. The human
// gate on spec.access.directories settled that the flag is redundant for
// any launcher that always passes --settings, which Tachyon's Claude path
// does (see CW-20260518-0061 hazard 2, and CW-20260903-0014's corrected
// section). Codex has no --settings to carry the grant, so the flag is how
// its scope is granted at all. This method was exported and tested on its
// own before either caller existed, on the bet that Cairn's contract would
// let a second provider work without a second code path in Tachyon; that
// is what happened — the Codex branch of HarnessArgv is one call to this,
// and it hardcodes no flag name.
func (r Result) ProjectDirArgv() []string {
	if r.Scope == nil {
		return nil
	}
	return ProjectDirArgv(r.ProjectDirArg, *r.Scope)
}

// HarnessArgv builds the flags Tachyon spawns result.Provider's harness
// with, from one Result. It returns the flags only — the binary name is
// internal/launch's to prepend, and spawning is internal/boot.SpawnITerm2's
// to do.
//
// A provider this function has no argv for is a returned error naming it,
// never an empty argv. Launching the wrong harness's command line, or a
// bare `claude` with no flags at all, is the failure this refusal exists to
// prevent: a Codex boot directory opened by Claude Code with no --settings
// is a session that starts, looks fine, and carries none of what was
// planted.
//
// # Claude Code: --settings, unconditional and permanent
//
// The flag is ALWAYS emitted. A settings.json merely sitting in the boot
// directory is read as the untrusted "projectSettings" tier and
// defaultMode: auto is silently refused there; passing --settings is what
// promotes it to the trusted "flagSettings" tier, which is what makes
// Tachyon's auto mode survive at all. No future task may drop this flag —
// see TestHarnessArgv_AlwaysIncludesSettingsFlag, a permanent regression
// guard, and CW-20260903-0014's hazard section for the full argument.
//
// Which PATH it names is a separate question from whether it appears, and
// the two were conflated while there was only one answer. Result.SettingsPath
// is what cairn reports it actually rendered, and it is preferred; the
// <BootDir>/.claude/settings.json join is the fallback for the case cairn's
// contract explicitly allows, SettingsPath being null because the render
// produced no file there.
//
// The order matters in one direction only. Gating the flag on SettingsPath
// would drop it whenever cairn rendered nothing — silently downgrading the
// launch, which is the whole hazard. Preferring the reported path when there
// IS one costs nothing and closes a different gap: cairn moved every
// harness's paths out of Go and into bootdir/layouts/<provider>.yaml
// precisely so a path is data, and a hardcoded join here is Tachyon holding
// a second copy of one of those documents. If the Claude layout ever moves
// that file, settings_path follows it and the join does not — and the
// permanent guard would keep passing, because it asserts the flag is
// present, not that it names something real.
//
// Claude Code deliberately gets no project-dir flag (--add-dir): the human
// gate on spec.access.directories settled that it is redundant for any
// launcher that always passes --settings, and this one does.
//
// # Codex: the project-dir flag, and nothing else
//
// Codex has no --settings equivalent — Cairn renders its settings into
// config.toml inside the boot directory, which Codex reads because
// CODEX_HOME points there ([Environment]) — so the access grant that
// --settings carries for Claude Code has to be made on the command line
// instead. That is exactly what Cairn's project_dir_arg is for, and this
// branch is [Result.ProjectDirArgv] and nothing more: the flag's own
// spelling is read from the report, never hardcoded here, and a boot with
// no scope produces no flag rather than a flag granting "".
//
// What must never appear here is --skip-git-repo-check. The installed CLI
// rejects it outright on an interactive launch; it is a `codex exec` flag,
// needed there because a boot directory is not a git repository. Copying it
// out of a non-interactive probe recipe into this argv turns every Codex
// launch into an immediate usage error — see
// TestHarnessArgv_CodexNeverEmitsTheExecOnlyFlag.
func HarnessArgv(result Result) ([]string, error) {
	switch result.Provider {
	case ProviderClaude:
		settingsPath := filepath.Join(result.BootDir, ".claude", "settings.json")
		if result.SettingsPath != nil && *result.SettingsPath != "" {
			settingsPath = *result.SettingsPath
		}
		return []string{"--settings", settingsPath}, nil
	case ProviderCodex:
		return result.ProjectDirArgv(), nil
	default:
		return nil, fmt.Errorf("boot: no harness argv known for provider %q", result.Provider)
	}
}

// Runner is the smallest seam this package needs to actually run
// `cairn boot`, so a unit test can substitute a fake instead of shelling
// out to a real binary. argv is exactly what internal/compose.Build (T08)
// returns — this package does not build it and does not know or care what
// built it, matching that package's own stated boundary ("Running Cairn is
// a different package's job").
type Runner func(ctx context.Context, argv []string) (stdout, stderr []byte, err error)

// ExecRunner returns a [Runner] that actually shells out to cairnPath via
// os/exec — the one subprocess this whole design allows (target
// architecture §5; see this package's doc comment). Everything else in
// this package is plain Go with no other os/exec call, exactly like
// internal/compose has none at all. Production code wires ExecRunner in
// once, at the call site that owns "off the UI thread" (T12,
// CW-20260903-0016); every test in this package except the one real
// integration test (TestKeyReconcilesWithRealCairnPlant, in
// reconcile_test.go) uses a fake Runner instead.
func ExecRunner(cairnPath string) Runner {
	return func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, cairnPath, argv...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
}

// InvokeError reports that running Cairn itself failed — a non-zero exit,
// or Cairn not being reachable at all (not on PATH, for instance). Its
// Error() surfaces Stderr, per this package's doc: Cairn's stderr is a
// debugging aid, not a gate, and a caller reporting this error to a person
// should show them what Cairn actually said rather than a generic
// "invocation failed."
type InvokeError struct {
	// Argv is exactly what was passed to the Runner.
	Argv []string
	// Stderr is Cairn's captured stderr, which may be empty if Cairn
	// failed before writing anything (e.g. the binary could not be
	// started at all).
	Stderr []byte
	// Err is the underlying error the Runner returned — typically an
	// *exec.ExitError for a non-zero exit, or an *exec.Error if the binary
	// could not be started.
	Err error
}

func (e *InvokeError) Error() string {
	stderr := strings.TrimSpace(string(e.Stderr))
	if stderr == "" {
		return fmt.Sprintf("boot: cairn %s: %v", strings.Join(e.Argv, " "), e.Err)
	}
	return fmt.Sprintf("boot: cairn %s: %v: %s", strings.Join(e.Argv, " "), e.Err, stderr)
}

// Unwrap exposes the underlying error a caller matching with errors.Is or
// errors.As (against *exec.ExitError, for instance) needs.
func (e *InvokeError) Unwrap() error { return e.Err }

// Invoke runs the one Cairn subprocess a launch needs: argv (built by
// internal/compose.Build, T08 — always ending in --json) via runner, and
// decodes stdout as a [Result]. It always returns Cairn's captured
// stderr alongside the result, even on a successful (exit 0) run —
// surfaced, per this package's doc, as a debugging aid a caller should
// show, never as a reason to fail a launch that otherwise succeeded. It is
// itself pure of any UI concern; calling it off Tachyon's UI thread, as
// the target architecture requires, is the caller's responsibility (T12,
// CW-20260903-0016) — this package imports no UI toolkit and makes no
// threading decision of its own.
//
// A non-zero exit — or a failure to start Cairn at all — is reported as an
// *[InvokeError] carrying Cairn's stderr, never a generic failure. Cairn
// producing output this function cannot parse as its --json contract is
// also a real error returned as-is: this package never falls back to
// scraping AGENTS.md, or anything else, to recover a scope some other way.
func Invoke(ctx context.Context, runner Runner, argv []string) (Result, []byte, error) {
	stdout, stderr, err := runner(ctx, argv)
	if err != nil {
		return Result{}, stderr, &InvokeError{Argv: argv, Stderr: stderr, Err: err}
	}

	var result Result
	if err := json.Unmarshal(stdout, &result); err != nil {
		return Result{}, stderr, fmt.Errorf("boot: parse cairn --json output: %w", err)
	}
	return result, stderr, nil
}
