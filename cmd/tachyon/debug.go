package main

import (
	"context"
	"fmt"

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/compose"
)

// Config is one debug run's inputs: exactly what [Run] needs to resolve a
// composition and invoke it. main.go fills this in from flags and from the
// same defaults the app itself would use (bundle.DefaultRootStore,
// state.BootRoot); a test fills it in directly.
type Config struct {
	// Target is the boot target: a saved binding's name, or a bare profile
	// id. Required.
	Target string
	// Bundle is the active bundle root. Required.
	Bundle string
	// BootRoot is where boot directories are planted. Required — see D9;
	// this command never defaults it silently any more than
	// internal/compose.Build does.
	BootRoot string
	// Scope, if non-empty, overrides the binding's own scope (cairn boot
	// --scope). Optional.
	Scope string
}

// Report is everything one [Run] learned, in typed form, so main.go's
// printing and a test's assertions read the same values rather than one
// re-deriving what the other already computed.
type Report struct {
	// Composition is exactly what was passed to compose.Build.
	Composition compose.Composition
	// Argv is the cairn boot argv compose.Build produced from Composition
	// — everything passed to the cairn binary named at CairnPath, not
	// including the binary name itself (matching internal/compose's own
	// boundary).
	Argv []string
	// CairnPath is the cairn binary Argv was run through.
	CairnPath string
	// Key is boot.Key(Composition.Target): the directory segment cairn
	// plants into.
	Key string
	// ExpectedBootDir is boot.CurrentPath(Composition.BootRoot, Key),
	// computed independently of anything cairn reports — a caller can
	// compare it against Result.BootDir to catch exactly the drift
	// internal/boot's TestKeyReconcilesWithRealCairnPlant guards against.
	ExpectedBootDir string

	// Stderr is cairn's captured stderr, present whether or not the run
	// succeeded — see internal/boot.Invoke's doc: it is a debugging aid,
	// never a success/failure signal on its own.
	Stderr []byte

	// Result and HarnessArgv are only meaningful when InvokeErr is nil.
	Result      boot.Result
	HarnessArgv []string

	// InvokeErr is whatever internal/boot.Invoke returned: typically a
	// *boot.InvokeError from a non-zero cairn exit — including cairn's own
	// "boot directory already exists", the expected shape of running this
	// command twice against the same target and boot root, since Run never
	// calls boot.Prepare to clear the way first. Argv, Key and
	// ExpectedBootDir are still meaningful on a Report with a non-nil
	// InvokeErr; Result and HarnessArgv are not.
	InvokeErr error
}

// Run resolves cfg into a compose.Composition, builds the argv
// internal/compose.Build would build for the app, and runs it through
// runner via internal/boot.Invoke — the same two entry points the app
// itself binds (T08, T10), so nothing here re-implements argv-building or
// --json decoding.
//
// Run never calls boot.Prepare. That is deliberate, not an oversight: this
// command's whole reason to exist is to be safe to run repeatedly without
// moving an existing `current` boot directory aside — see this package's
// doc comment. Run also spawns nothing itself; runner is supplied by the
// caller (main.go wires boot.ExecRunner, the one subprocess this whole
// design allows; a test supplies a fake).
//
// The returned error is non-nil only when cfg could not even be turned into
// an argv (compose.Build's own validation — an empty Target, Bundle or
// BootRoot). A failure to run cairn itself, or to parse its output, is
// reported inside the returned Report's InvokeErr instead, alongside
// whatever was already known (Argv, Key, ExpectedBootDir) — a caller
// showing that to a person benefits from seeing what was about to run, not
// just that it failed.
func Run(ctx context.Context, cfg Config, runner boot.Runner) (Report, error) {
	comp := compose.Composition{
		Target:   cfg.Target,
		Bundle:   cfg.Bundle,
		BootRoot: cfg.BootRoot,
		Scope:    cfg.Scope,
	}

	argv, err := compose.Build(comp)
	if err != nil {
		return Report{Composition: comp}, fmt.Errorf("tachyon: building cairn argv: %w", err)
	}

	key := boot.Key(cfg.Target)
	report := Report{
		Composition:     comp,
		Argv:            argv,
		Key:             key,
		ExpectedBootDir: boot.CurrentPath(cfg.BootRoot, key),
	}

	result, stderr, invokeErr := boot.Invoke(ctx, runner, argv)
	report.Stderr = stderr
	if invokeErr != nil {
		report.InvokeErr = invokeErr
		return report, nil
	}

	report.Result = result
	report.HarnessArgv = boot.HarnessArgv(result)
	return report, nil
}

// argvHasFlag reports whether flag appears as one of argv's own elements
// (not as a substring of some other element's value) — how both
// printReport and this package's tests check whether --settings made it
// into a harness argv.
func argvHasFlag(argv []string, flag string) bool {
	for _, a := range argv {
		if a == flag {
			return true
		}
	}
	return false
}
