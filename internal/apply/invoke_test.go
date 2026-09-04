package apply_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/apply"
)

// fakeRunner returns an [apply.Runner] that ignores dir/env and hands back
// the given stdout, stderr and err -- the same shape
// internal/boot/invoke_test.go's fakeRunner uses for [boot.Invoke].
func fakeRunner(stdout, stderr []byte, err error) apply.Runner {
	return func(ctx context.Context, dir string, env []string) ([]byte, []byte, error) {
		return stdout, stderr, err
	}
}

func TestInvoke_SuccessReturnsOutputAndPaths(t *testing.T) {
	runner := fakeRunner([]byte("staged into /scratch/agents\n"), nil, nil)
	result, err := apply.Invoke(context.Background(), runner, "/scratch/bundle", "/scratch/agents")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.BundleRoot != "/scratch/bundle" || result.AgentsHome != "/scratch/agents" {
		t.Errorf("Result = %+v; want BundleRoot/AgentsHome echoed back", result)
	}
	if result.Stdout != "staged into /scratch/agents\n" {
		t.Errorf("Result.Stdout = %q", result.Stdout)
	}
}

// TestInvoke_FailureSurfacesMakesOwnStderr is the acceptance bullet
// "failure surfaces make's own output" -- proven against the returned
// error's message directly, not a generic "apply failed".
func TestInvoke_FailureSurfacesMakesOwnStderr(t *testing.T) {
	underlying := errors.New("exit status 2")
	stderrText := "make: *** No rule to make target 'install-system'.  Stop."
	runner := fakeRunner(nil, []byte(stderrText), underlying)

	_, err := apply.Invoke(context.Background(), runner, "/scratch/bundle-no-makefile", "/scratch/agents")
	if err == nil {
		t.Fatal("Invoke returned no error for a failing runner")
	}
	if !strings.Contains(err.Error(), stderrText) {
		t.Errorf("error message = %q; want it to contain make's own stderr %q", err.Error(), stderrText)
	}

	var invokeErr *apply.InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("error is not a *apply.InvokeError: %T (%v)", err, err)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("errors.Is(err, underlying) = false; InvokeError.Unwrap must expose the runner's own error")
	}
	if string(invokeErr.Stderr) != stderrText {
		t.Errorf("InvokeError.Stderr = %q; want %q", invokeErr.Stderr, stderrText)
	}
}

// TestInvoke_FailureFallsBackToStdoutWhenStderrEmpty covers a runner that
// fails with output only on stdout (some tools do) -- the error message
// must still surface something useful rather than just the bare Go error.
func TestInvoke_FailureFallsBackToStdoutWhenStderrEmpty(t *testing.T) {
	underlying := errors.New("exit status 1")
	stdoutText := "make: nothing to be done, but something still failed"
	runner := fakeRunner([]byte(stdoutText), nil, underlying)

	_, err := apply.Invoke(context.Background(), runner, "/scratch/bundle", "/scratch/agents")
	if err == nil {
		t.Fatal("Invoke returned no error")
	}
	if !strings.Contains(err.Error(), stdoutText) {
		t.Errorf("error message = %q; want it to fall back to stdout %q when stderr is empty", err.Error(), stdoutText)
	}
}

// TestInvoke_BinaryNotFoundStillReportsSomething covers the "make itself
// missing" case: no stdout, no stderr, just an underlying error (what
// exec.LookPath's failure looks like through a Runner).
func TestInvoke_BinaryNotFoundStillReportsSomething(t *testing.T) {
	underlying := errors.New(`exec: "make": executable file not found in $PATH`)
	runner := fakeRunner(nil, nil, underlying)

	_, err := apply.Invoke(context.Background(), runner, "/scratch/bundle", "/scratch/agents")
	if err == nil {
		t.Fatal("Invoke returned no error")
	}
	if !strings.Contains(err.Error(), "executable file not found") {
		t.Errorf("error message = %q; want the underlying exec error surfaced", err.Error())
	}
}

