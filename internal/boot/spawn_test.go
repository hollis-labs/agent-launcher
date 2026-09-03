package boot

import (
	"errors"
	"strings"
	"testing"
)

// This file is a white-box test (package boot, not boot_test) because it
// exercises shellQuote, appleScriptString, shellCommand, appleScript and
// spawnITerm2 directly by name -- the exact pure string-building functions
// T12 (CW-20260903-0016) ported from the deleted Swift app -- rather than
// only through SpawnITerm2's public surface. Every other test in this
// package (invoke_test.go, boot_test.go, reconcile_test.go) is
// package boot_test; this one differs on purpose, per this task's own
// acceptance bullet: "shellQuote, appleScriptString, the final osascript
// source... test it thoroughly and directly, asserting exact expected
// strings."

// recordingStarter is a fake [starter] that records exactly what it was
// called with instead of running anything, so these tests assert on the
// precise osascript invocation SpawnITerm2 builds without ever launching
// iTerm2.
type recordingStarter struct {
	name string
	args []string
	err  error
}

func (r *recordingStarter) start(name string, args ...string) error {
	r.name = name
	r.args = append([]string(nil), args...)
	return r.err
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"plain", "claude", "'claude'"},
		{"space", "/Users/chrispian/dev/agent setup", "'/Users/chrispian/dev/agent setup'"},
		{"single quote", "it's", `'it'\''s'`},
		{"double quote passes through unescaped", `say "hi"`, `'say "hi"'`},
		{"empty", "", "''"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shellQuote(tc.value); got != tc.want {
				t.Errorf("shellQuote(%q) = %q; want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestAppleScriptString(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"plain", `cd '/x' && 'claude'`, `"cd '/x' && 'claude'"`},
		{"embedded double quote", `'say "hi"'`, `"'say \"hi\"'"`},
		{"embedded backslash", `C:\path`, `"C:\\path"`},
		{"backslash before quote -- order matters", `\"`, `"\\\""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := appleScriptString(tc.value); got != tc.want {
				t.Errorf("appleScriptString(%q) = %q; want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestShellCommand_CwdOmittedWhenEmpty(t *testing.T) {
	got := shellCommand([]string{"claude", "--settings", "/p/s.json"}, "")
	want := "'claude' '--settings' '/p/s.json'"
	if got != want {
		t.Fatalf("shellCommand = %q; want %q", got, want)
	}
}

func TestShellCommand_CwdWithSpace(t *testing.T) {
	got := shellCommand([]string{"claude", "--settings", "/p/s.json"}, "/Users/chrispian/dev/agent setup")
	want := "cd '/Users/chrispian/dev/agent setup' && 'claude' '--settings' '/p/s.json'"
	if got != want {
		t.Fatalf("shellCommand = %q; want %q", got, want)
	}
}

func TestShellCommand_ArgWithSingleQuote(t *testing.T) {
	got := shellCommand([]string{"claude", "it's-fine"}, "/tmp")
	want := `cd '/tmp' && 'claude' 'it'\''s-fine'`
	if got != want {
		t.Fatalf("shellCommand = %q; want %q", got, want)
	}
}

func TestShellCommand_ArgWithDoubleQuote(t *testing.T) {
	got := shellCommand([]string{"claude", `say "hi"`}, "/tmp")
	want := `cd '/tmp' && 'claude' 'say "hi"'`
	if got != want {
		t.Fatalf("shellCommand = %q; want %q", got, want)
	}
}

func TestAppleScript_Template(t *testing.T) {
	got := appleScript("cd '/tmp' && 'claude'")
	want := `tell application id "com.googlecode.iterm2"
  activate
  if (count of windows) = 0 then
    create window with default profile
  else
    tell current window
      create tab with default profile
    end tell
  end if
  tell current session of current window
    write text "cd '/tmp' && 'claude'"
  end tell
end tell`
	if got != want {
		t.Fatalf("appleScript = %q; want %q", got, want)
	}
}

// TestSpawnITerm2_CwdWithSpace_BuildsExactOsascriptCall covers the
// task's own required case list end to end: a cwd containing a space,
// run through the whole spawnITerm2 pipeline (shellCommand -> appleScript
// -> osascript -e <script>), asserting the exact command embedded in the
// script.
func TestSpawnITerm2_CwdWithSpace_BuildsExactOsascriptCall(t *testing.T) {
	rec := &recordingStarter{}
	argv := []string{"claude", "--settings", "/state/boot/eng-nanite/current/.claude/settings.json"}
	cwd := "/Users/chrispian/dev/agent setup"

	if err := spawnITerm2(rec.start, argv, cwd); err != nil {
		t.Fatalf("spawnITerm2: %v", err)
	}
	if rec.name != "/usr/bin/osascript" {
		t.Errorf("name = %q; want /usr/bin/osascript", rec.name)
	}
	if len(rec.args) != 2 || rec.args[0] != "-e" {
		t.Fatalf("args = %v; want [-e <script>]", rec.args)
	}
	script := rec.args[1]
	wantCommand := "cd '/Users/chrispian/dev/agent setup' && 'claude' '--settings' '/state/boot/eng-nanite/current/.claude/settings.json'"
	wantLine := `write text "` + wantCommand + `"`
	if !strings.Contains(script, wantLine) {
		t.Fatalf("script does not contain expected write text line %q; script=%s", wantLine, script)
	}
}

// TestSpawnITerm2_ArgWithSingleQuoteAndDoubleQuote covers the task's other
// two required cases together: an arg with a single quote in it (must
// still be one shell token after shellQuote) and an arg with a double
// quote in it (must come out backslash-escaped once appleScriptString
// wraps the whole command for AppleScript).
func TestSpawnITerm2_ArgWithSingleQuoteAndDoubleQuote(t *testing.T) {
	rec := &recordingStarter{}
	argv := []string{"claude", "it's", `say "hi"`}
	if err := spawnITerm2(rec.start, argv, ""); err != nil {
		t.Fatalf("spawnITerm2: %v", err)
	}
	script := rec.args[1]
	// shellCommand's shellQuote produces the single backslash in
	// 'it'\''s' (see TestShellCommand_ArgWithSingleQuote); appleScript then
	// wraps the whole command with appleScriptString, which doubles every
	// backslash so it survives as a literal backslash inside AppleScript's
	// own string literal -- so the script embeds 'it'\\''s', not 'it'\''s'.
	if !strings.Contains(script, `'it'\\''s'`) {
		t.Fatalf("script missing single-quote-escaped arg (AppleScript-escaped): %s", script)
	}
	if !strings.Contains(script, `\"hi\"`) {
		t.Fatalf("script missing AppleScript-escaped double quotes: %s", script)
	}
}

func TestSpawnITerm2_EmptyArgvIsAnError(t *testing.T) {
	rec := &recordingStarter{}
	if err := spawnITerm2(rec.start, nil, "/tmp"); err == nil {
		t.Fatal("spawnITerm2(nil argv) returned no error")
	}
	if rec.name != "" {
		t.Fatalf("starter was called for an empty argv: %+v", rec)
	}
}

func TestSpawnITerm2_StartFailureIsSurfacedNotSwallowed(t *testing.T) {
	underlying := errors.New(`exec: "osascript": file does not exist`)
	rec := &recordingStarter{err: underlying}
	err := spawnITerm2(rec.start, []string{"claude"}, "/tmp")
	if err == nil {
		t.Fatal("spawnITerm2 returned no error when the starter failed")
	}
	if !errors.Is(err, underlying) {
		t.Fatalf("spawnITerm2 error does not wrap the starter's error: %v", err)
	}
}
