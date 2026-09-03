package boot

import (
	"fmt"
	"os/exec"
	"strings"
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
// cwd: a new tab in the current window if one is already open, a new
// window otherwise. argv[0] is the harness binary; argv[1:] are its
// arguments -- the full command line, binary included, unlike
// [HarnessArgv]'s own output, which is flags only. Wiring the two
// together -- prepending the harness binary Provider maps to -- is a
// caller's job (T12, CW-20260903-0016's internal/launch package).
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
func SpawnITerm2(argv []string, cwd string) error {
	return spawnITerm2(realStarter, argv, cwd)
}

// spawnITerm2 is SpawnITerm2's body, taking start explicitly so
// spawn_test.go can substitute a fake and assert on the exact shell command
// and AppleScript source built, without ever invoking osascript for real.
func spawnITerm2(start starter, argv []string, cwd string) error {
	if len(argv) == 0 {
		return fmt.Errorf("boot: SpawnITerm2: argv is empty")
	}
	script := appleScript(shellCommand(argv, cwd))
	if err := start("/usr/bin/osascript", "-e", script); err != nil {
		return fmt.Errorf("boot: SpawnITerm2: starting osascript: %w", err)
	}
	return nil
}

// shellCommand builds the literal shell command line the spawned iTerm2
// session runs: `cd <cwd> && <argv[0]> <argv[1]>...`, every component
// POSIX-quoted with [shellQuote] -- ported from the deleted Swift app's
// EngineClient.iTermCommand, minus the env-var block that method also
// built: nothing in this design's launch path ever populates one (T12
// adds no env-passing mechanism). cwd is omitted entirely -- no leading
// "cd ... &&" -- when empty.
func shellCommand(argv []string, cwd string) string {
	parts := make([]string, 0, len(argv)+1)
	if cwd != "" {
		parts = append(parts, "cd "+shellQuote(cwd)+" &&")
	}
	for _, a := range argv {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// shellQuote wraps value in single quotes for POSIX shell, escaping any
// embedded single quote as '\'' -- ported unchanged from the deleted Swift
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
