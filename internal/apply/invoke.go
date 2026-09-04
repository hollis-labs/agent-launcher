package apply

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner is the smallest seam this package needs to actually run `make
// install-system`, so a unit test can substitute a fake instead of
// shelling out to a real make binary — the same shape internal/boot.Runner
// gives internal/boot.Invoke, for the identical reason. dir is the working
// directory the command runs in (the bundle root); env is extra
// environment variables appended after the process's own (AGENTS_HOME=...,
// specifically — see [Invoke]).
type Runner func(ctx context.Context, dir string, env []string) (stdout, stderr []byte, err error)

// ExecRunner returns a [Runner] that actually shells out to `make
// install-system` via os/exec — the one subprocess this package invokes.
// It never reimplements the three rsyncs the Makefile runs (see the
// package doc): if make itself is not on PATH, or the bundle has no
// install-system target (no Makefile at all, or one without that target),
// that failure comes back from the OS or from make's own stderr unchanged
// — nothing here falls back to copying files by hand.
func ExecRunner() Runner {
	return func(ctx context.Context, dir string, env []string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, "make", "install-system")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
}

// Result is what a successful [Invoke] produced: `make install-system`'s
// own output, plus the two paths it ran against, so a caller can show a
// completion message without a second round trip to ask what just
// happened.
type Result struct {
	BundleRoot string `json:"bundleRoot"`
	AgentsHome string `json:"agentsHome"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
}

// InvokeError reports that `make install-system` itself failed — a
// non-zero exit (including "No rule to make target", when the bundle has
// no such target, or a bundle with no Makefile at all), or make not being
// reachable at all (not on PATH). Its Error() surfaces make's own stderr,
// falling back to stdout, so a caller reporting this to a person shows
// them what make actually said rather than a generic "apply failed" — the
// acceptance criterion this exists for.
type InvokeError struct {
	// Dir is the bundle root the command ran in.
	Dir string
	// AgentsHome is the value AGENTS_HOME was set to.
	AgentsHome string
	// Stdout and Stderr are make's captured output, which may be empty if
	// make failed before writing anything (e.g. the binary could not be
	// started at all).
	Stdout, Stderr []byte
	// Err is the underlying error the Runner returned — typically an
	// *exec.ExitError for a non-zero exit, or an *exec.Error if make could
	// not be started.
	Err error
}

func (e *InvokeError) Error() string {
	out := strings.TrimSpace(string(e.Stderr))
	if out == "" {
		out = strings.TrimSpace(string(e.Stdout))
	}
	prefix := fmt.Sprintf("apply: make install-system (dir=%s AGENTS_HOME=%s)", e.Dir, e.AgentsHome)
	if out == "" {
		return fmt.Sprintf("%s: %v", prefix, e.Err)
	}
	return fmt.Sprintf("%s: %v: %s", prefix, e.Err, out)
}

// Unwrap exposes the underlying error a caller matching with errors.Is or
// errors.As (against *exec.ExitError, for instance) needs.
func (e *InvokeError) Unwrap() error { return e.Err }

// Invoke runs `make install-system` in bundleRoot via runner, with
// AGENTS_HOME set to agentsHome — the one command this package ever runs
// to actually change anything on disk (see the package doc's "staging is
// deliberate, not implicit"). It never reimplements what that target
// does.
//
// A non-zero exit — or a failure to start make at all — is reported as an
// *[InvokeError] carrying make's own stdout and stderr, never a generic
// failure.
func Invoke(ctx context.Context, runner Runner, bundleRoot, agentsHome string) (Result, error) {
	env := []string{"AGENTS_HOME=" + agentsHome}
	stdout, stderr, err := runner(ctx, bundleRoot, env)
	if err != nil {
		return Result{}, &InvokeError{
			Dir: bundleRoot, AgentsHome: agentsHome,
			Stdout: stdout, Stderr: stderr, Err: err,
		}
	}
	return Result{
		BundleRoot: bundleRoot,
		AgentsHome: agentsHome,
		Stdout:     string(stdout),
		Stderr:     string(stderr),
	}, nil
}

// ResolveAgentsHome is the real AGENTS_HOME Apply targets when nothing
// overrides it: $HOME/.config/agents, matching agent-setup's own
// Makefile default — `AGENTS_HOME ?= $(HOME)/.config/agents` — exactly, so
// this package's idea of "the installed layer" never drifts from what a
// person running `make install-system` by hand, with no override, would
// get.
//
// A test never calls this: see [Options.AgentsHome] for how a test points
// [Service] at a scratch directory instead — this function is the one
// place the real ~/.config/agents path is ever computed, so a test that
// needs a different one overrides the whole resolver rather than this
// function's own logic.
func ResolveAgentsHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("apply: locating home directory: %w", err)
	}
	return filepath.Join(home, ".config", "agents"), nil
}
