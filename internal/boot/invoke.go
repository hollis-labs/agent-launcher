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

// ProjectDirPlaceholder is the token Cairn's --json report leaves standing
// in project_dir_arg wherever the scope goes — see [Result.ProjectDirArgv]
// and [ProjectDirArgv]. Named to match cairn's own
// cmd/cairn/bootjson.go:projectDirPlaceholder.
const ProjectDirPlaceholder = "{{.ProjectDir}}"

// Result is what `cairn boot --json` prints, decoded: everything a
// launcher needs to open the directory Cairn just planted and build the
// harness's argv, without reading any file inside that directory to find
// out. It mirrors Cairn's own bootReport (cmd/cairn/bootjson.go in the
// cairn repo, and examples/README.md §5) key for key — six keys, flat,
// snake_case, every one of them present on every successful boot.
//
// Scope, SettingsPath and ProjectDirArg are nil-able (a pointer or a nil
// slice) because Cairn's own contract makes null meaningful and distinct
// from the zero value of the underlying type: Scope nil means the binding
// declared no scope and none was given on the command line; SettingsPath
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

	// Provider is the harness the directory was rendered for — "claude"
	// today. Cairn's own contract for a second provider (Codex) requires
	// no change here, only a different value.
	Provider string `json:"provider"`

	// Scope is the directory the instance works in, absolute and
	// symlink-resolved, or nil when the binding declared none and no
	// --scope was given.
	Scope *string `json:"scope"`

	// SettingsPath is the absolute path of the file at the harness's
	// settings path, or nil when the render produced none.
	// [HarnessArgv] deliberately does not read this field — see its doc
	// comment.
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
// [HarnessArgv] does not call this method today: the human gate on
// spec.access.directories settled that this flag is redundant for any
// launcher that always passes --settings, which Tachyon's does (see
// CW-20260518-0061 hazard 2, and CW-20260903-0014's corrected section).
// It is exported and tested on its own because Cairn's contract exists so
// a second provider (Codex) works without a second code path in Tachyon —
// a future caller that needs this flag for a provider without a
// settings-based access grant has it ready here, proven correct
// independently of whether Claude Code's own launch path ever calls it.
func (r Result) ProjectDirArgv() []string {
	if r.Scope == nil {
		return nil
	}
	return ProjectDirArgv(r.ProjectDirArg, *r.Scope)
}

// HarnessArgv builds the argv Tachyon spawns the harness (Claude Code)
// with, from one Result. Spawning itself is a later task (T12,
// CW-20260903-0016); this returns the argv and nothing else.
//
// --settings <BootDir>/.claude/settings.json is unconditional, permanent,
// and computed directly from result.BootDir joined with the fixed
// ".claude/settings.json" relative path — never read from
// Result.SettingsPath, which Cairn's own contract allows to be nil. A
// settings.json merely sitting in the boot directory is read as the
// untrusted "projectSettings" tier and defaultMode: auto is silently
// refused there; passing --settings is what promotes it to the trusted
// "flagSettings" tier, which is what makes Tachyon's auto mode survive at
// all. No future task may drop this flag — see
// TestHarnessArgv_AlwaysIncludesSettingsFlag, a permanent regression
// guard, and CW-20260903-0014's hazard section for the full argument.
//
// The provider's project-dir flag (ProjectDirArg — "--add-dir" for Claude
// Code) is deliberately never added here: the human gate on
// spec.access.directories settled that it is redundant for any launcher
// that always passes --settings, and this one does. See
// [Result.ProjectDirArgv] for where that substitution still lives, tested
// on its own, for a provider that would actually need it.
func HarnessArgv(result Result) []string {
	settingsPath := filepath.Join(result.BootDir, ".claude", "settings.json")
	return []string{"--settings", settingsPath}
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
