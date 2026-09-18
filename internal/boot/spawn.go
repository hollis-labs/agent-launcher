package boot

import (
	"fmt"
	"os/exec"
	"strings"
	"unicode"
)

// starter is the seam SpawnITerm2 shells out through: injectable so a test
// can assert on the exact osascript invocation this package builds, without
// ever launching a real iTerm2 window during go test. Same spirit as this
// package's own Runner in invoke.go, minus the context and captured
// output -- SpawnITerm2 never waits for what it starts (see its own doc
// below), so there is nothing to capture and nothing to cancel.
type starter func(name string, args ...string) error

// realStarter is the one real implementation, and the one subprocess call
// in this file: exec.Command(...).Start() starts the process and returns
// immediately, without waiting for it to exit.
func realStarter(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

// SpawnITerm2 opens iTerm2 on argv, run with its working directory set to
// cwd and env added to its environment: a new tab in the current window if
// one is already open, a new window otherwise. argv[0] is the harness
// binary; argv[1:] are its arguments -- the full command line, binary
// included, unlike [HarnessArgv]'s own output, which is flags only. Wiring
// the two together -- prepending the harness binary Provider maps to -- is a
// caller's job (T12, CW-20260903-0016's internal/launch package).
//
// # Where env has to land, and where it must not
//
// env is [Environment]'s output: already-substituted KEY=VALUE entries, or
// nil when the provider declares none (Claude Code's case, and the shape
// this function had before Codex existed -- a nil env produces byte-for-byte
// the command line it always produced). Each entry becomes a shell
// assignment PREFIXED TO THE COMMAND ITSELF, inside the line iTerm2 runs:
//
//	cd '<cwd>' && CODEX_HOME='<bootdir>' 'codex' '--add-dir' '<scope>'
//
// That placement is the whole point and it is easy to get wrong in a way
// that looks right. The obvious alternative -- setting the variable on the
// osascript process this function starts -- puts it in the environment of a
// short-lived AppleScript interpreter that then asks a long-running iTerm2
// (a separate process, started long ago, by launchd) to run a command. Not
// one byte of osascript's environment reaches that command. The session
// would open, Codex would start, and it would read the operator's real
// ~/.codex instead of the boot directory -- a launch that succeeds and
// carries none of what was planted. So the variable travels as text, in the
// command line, which is the only channel this design has to the process
// that actually runs.
//
// The assignment prefix form is POSIX (and zsh) for "set this in the
// environment of exactly this command," so nothing here leaks into the
// interactive shell that survives the harness exiting.
//
// This is a direct port of the deleted Swift app's openITerm /
// appleScriptString / shellQuote / EngineClient.iTermCommand -- recoverable
// with `git show ad91842^:app/main.swift` -- not a reinvention: same
// shell-quoting rule, same AppleScript template, same fire-and-forget
// posture. SpawnITerm2 starts the osascript process and returns
// immediately. It does not wait for osascript, does not wait for or poll
// iTerm2 itself, and retains no handle on anything it starts -- Tachyon
// holds no session handle, ever (D7; plan CW-20260518-0061). The one
// deliberate departure from the Swift original is that a failure to even
// start osascript (not found, no permission, ...) is a real returned error
// here, rather than swallowed the way Swift's `try? process.run()` did;
// nothing about waiting for or monitoring the spawned session is added on
// top of that.
func SpawnITerm2(argv []string, cwd string, env []string) error {
	return spawnITerm2(realStarter, argv, cwd, env)
}

// spawnITerm2 is SpawnITerm2's body, taking start explicitly so
// spawn_test.go can substitute a fake and assert on the exact shell command
// and AppleScript source built, without ever invoking osascript for real.
func spawnITerm2(start starter, argv []string, cwd string, env []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("boot: SpawnITerm2: argv is empty")
	}
	command, err := shellCommand(argv, cwd, env)
	if err != nil {
		return err
	}
	script := appleScript(command)
	if err := start("/usr/bin/osascript", "-e", script); err != nil {
		return fmt.Errorf("boot: SpawnITerm2: starting osascript: %w", err)
	}
	return nil
}

// shellCommand builds the literal shell command line the spawned iTerm2
// session runs: `cd <cwd> && <KEY=VALUE>... <argv[0]> <argv[1]>...`, every
// path and argument POSIX-quoted with [shellQuote], and every environment
// value quoted the same way. This is the deleted Swift app's
// EngineClient.iTermCommand, env-var block included -- that block was
// dropped when this was ported because nothing populated one then, and
// Codex's CODEX_HOME is what populates it now. cwd is omitted entirely --
// no leading "cd ... &&" -- when empty, and a nil or empty env adds
// nothing at all.
//
// An entry's key is placed unquoted, because a shell assignment prefix
// cannot be quoted and still be an assignment; [Environment] has already
// refused any key that is not a plain shell variable name, and this
// function refuses one too rather than trusting a caller that skipped it.
// Values are quoted exactly as arguments are, so a boot directory with a
// space or a quote in its path travels intact.
//
// Every component is checked for a control character first. A newline in a
// path would not be a quoting bug but a structural one: this string is
// embedded in AppleScript source and then written to a terminal as a line,
// and either layer would end the line early and run whatever followed as
// its own command. Single quotes cannot contain that hazard, so it is
// refused instead.
func shellCommand(argv []string, cwd string, env []string) (string, error) {
	parts := make([]string, 0, len(argv)+len(env)+1)

	if cwd != "" {
		if err := refuseControlChars("working directory", cwd); err != nil {
			return "", err
		}
		parts = append(parts, "cd "+shellQuote(cwd)+" &&")
	}

	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			return "", fmt.Errorf("boot: SpawnITerm2: environment entry %q has no %q", entry, "=")
		}
		if !envKeyRe.MatchString(key) {
			return "", fmt.Errorf("boot: SpawnITerm2: environment entry %q: %q is not a shell variable name", entry, key)
		}
		if err := refuseControlChars("environment value for "+key, value); err != nil {
			return "", err
		}
		parts = append(parts, key+"="+shellQuote(value))
	}

	for _, a := range argv {
		if err := refuseControlChars("argument", a); err != nil {
			return "", err
		}
		parts = append(parts, shellQuote(a))
	}

	return strings.Join(parts, " "), nil
}

// refuseControlChars reports an error naming what carried a control
// character, rather than quoting one into a command line that would split
// at it. See [shellCommand] for why quoting is not an answer here.
func refuseControlChars(what, value string) error {
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("boot: SpawnITerm2: %s %q contains a control character (%q) and cannot be sent to a terminal safely", what, value, r)
		}
	}
	return nil
}

// shellQuote wraps value in single quotes for POSIX shell, escaping any
// embedded single quote as '\” -- ported unchanged from the deleted Swift
// app's shellQuote.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// appleScript builds the AppleScript source SpawnITerm2 hands to
// `osascript -e`: activate iTerm2, add a tab to the current window if one
// is already open, otherwise create a window, then write command into the
// resulting session -- ported unchanged from the deleted Swift app's
// openITerm.
func appleScript(command string) string {
	return `tell application id "com.googlecode.iterm2"
  activate
  if (count of windows) = 0 then
    create window with default profile
  else
    tell current window
      create tab with default profile
    end tell
  end if
  tell current session of current window
    write text ` + appleScriptString(command) + `
  end tell
end tell`
}

// appleScriptString wraps value in double quotes for embedding into
// AppleScript source, escaping backslash then double-quote, in that order
// -- ported unchanged from the deleted Swift app's appleScriptString.
// Order matters: escaping the quote first would double-escape the
// backslashes the first pass just inserted.
func appleScriptString(value string) string {
	v := strings.ReplaceAll(value, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}
