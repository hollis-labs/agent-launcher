// Package launch turns a picked binding into a running terminal: the
// palette's one entry point once a name has been chosen from the list
// internal/binding.Service already reads. It is small on purpose --
// internal/compose (T08) already builds the cairn boot argv, internal/boot
// (T09/T10) already runs cairn and decodes its --json report, and
// internal/boot.SpawnITerm2 (T12, CW-20260903-0016, the same task as this
// package) already knows how to open iTerm2 on an argv. This package's
// whole job is gluing those three together behind one Wails-bindable
// method, [Service.Launch].
//
// # No session handle, ever (D7)
//
// [Service.Launch] returns only an error. It stores nothing about what it
// started -- no PID, no *os.Process, no session id -- and nothing in this
// package can answer "is it still running." Tachyon is a launcher, not a
// console: a managed session with a list, attach and resume was considered
// and rejected (plan CW-20260518-0061, D7) because it pulls Tachyon back
// toward the daemon-backed stack this whole design deliberately separates
// from. See internal/boot.SpawnITerm2's own doc for the identical
// discipline one layer down: it starts osascript and does not wait for it
// either.
package launch

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/state"
)

// bindingsFileName is bindings.yaml's name within a bundle root -- the same
// constant internal/binding/service.go keeps to itself. Duplicated rather
// than imported: internal/binding.Service does not export a way to open a
// [binding.Store] without also exposing List/Create/Update/Delete to JS,
// none of which this package wants on its own surface, and
// internal/binding itself does not export its own fileName. See
// [Service.resolveBinding].
const bindingsFileName = "bindings.yaml"

// harnessBinary maps a [boot.Result.Provider] value to the binary
// SpawnITerm2 actually execs. "claude" is the only provider Cairn's own
// --json contract documents today (internal/boot/invoke.go's doc on
// Provider); this is a lookup table, not a plugin system -- a second
// provider is a second map entry, not a new code path through this
// package.
var harnessBinary = map[string]string{
	"claude": "claude",
}

// Service is bound to the frontend as a Wails service: the palette's
// launch entry point. Its exported method is callable from JavaScript as
// "github.com/hollis-labs/tachyon/internal/launch.Service.Launch".
//
// Like internal/binding.Service and internal/manager.Service, it holds a
// [bundle.RootStore] rather than a fixed path or an already-open
// [binding.Store]: every call resolves the active bundle root fresh, so
// the binding a launch resolves is read from whichever bundle the palette
// itself is currently showing.
type Service struct {
	store bundle.RootStore
}

// NewService returns a Service that resolves bindings against whichever
// bundle store.Resolve() names.
func NewService(store bundle.RootStore) *Service {
	return &Service{store: store}
}

// Launch resolves name to a binding, runs the one `cairn boot` invocation
// that produces its argv (internal/compose.Build, internal/boot.Invoke),
// and spawns iTerm2 on the result (internal/boot.SpawnITerm2). It returns
// only an error -- see the package doc's "no session handle, ever."
//
// A non-nil error here is safe to show a person directly: every error this
// method can return already carries whatever underlying detail made it
// fail (cairn's own stderr via *boot.InvokeError, an unresolved binding
// name, an unrecognized cwd_preference, ...), matching this whole design's
// standing aversion to a generic failure message that throws away why.
func (s *Service) Launch(name string) error {
	b, bundleRoot, err := s.resolveBinding(name)
	if err != nil {
		return err
	}

	bootRoot, err := state.BootRoot()
	if err != nil {
		return fmt.Errorf("launch: resolving boot root: %w", err)
	}

	// The real cairn binary on PATH, the same lookup cmd/tachyon/main.go
	// (T11) performs for its own --cairn-less default.
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		return fmt.Errorf("launch: cairn not found on PATH: %w", err)
	}

	return launch(context.Background(), b, bundleRoot, bootRoot, boot.ExecRunner(cairnPath), boot.SpawnITerm2)
}

// resolveBinding opens a fresh [binding.FileStore] over the active
// bundle's bindings.yaml -- mirroring internal/binding.Service.open() --
// and looks name up in it, returning both the resolved binding and the
// bundle root it came from, so Launch does not resolve the same root
// twice (compose.Composition.Bundle needs it too).
func (s *Service) resolveBinding(name string) (binding.Binding, string, error) {
	root, err := s.store.Resolve()
	if err != nil {
		return binding.Binding{}, "", fmt.Errorf("launch: resolving bundle root: %w", err)
	}
	store := binding.NewFileStore(filepath.Join(root, bindingsFileName))
	b, err := store.Get(name)
	if err != nil {
		return binding.Binding{}, "", fmt.Errorf("launch: resolving binding %q: %w", name, err)
	}
	return b, root, nil
}

// spawnFunc matches [boot.SpawnITerm2]'s signature -- the seam
// launch_test.go substitutes a fake for, so tests assert on the argv and
// cwd that reach the spawn step without ever invoking osascript.
type spawnFunc func(argv []string, cwd string) error

// launch is Launch's orchestration body once a binding is in hand: build
// the composition, run cairn, resolve a cwd from what it reported, prepend
// the harness binary to the argv HarnessArgv built, and spawn. Kept
// separate from the Service method -- which also resolves the binding, the
// boot root and the cairn path, none of which a unit test should have to
// touch -- so a test can drive exactly this part with a fake runner and a
// fake spawn func instead of a real cairn binary and a real terminal.
//
// b.Name becomes Composition.Target, not b.Profile: `cairn boot <name>`
// checks bindings.yaml first (see that file's own header comment), which
// is precisely how b's own Profile and Scope get resolved server-side --
// so Composition.Scope is deliberately left at its zero value here rather
// than set from b.Scope. Setting it would send an explicit --scope that
// merely restates what Cairn already resolves on its own from the binding
// it just looked up by name. cmd/tachyon's own Config.Scope (T11) makes
// the identical choice: it leaves Scope empty by default, only ever
// populating it from an explicit CLI override that this package's single
// Launch(name) signature has no way to ask for.
func launch(ctx context.Context, b binding.Binding, bundleRoot, bootRoot string, runner boot.Runner, spawn spawnFunc) error {
	// Clear the target before cairn plants into it. cairn boot refuses an
	// already-occupied Current (bootdir.PlantFiles's ErrExists) -- without
	// this, only ever the very first launch of a given binding would
	// succeed. Prepare renames any existing current aside rather than
	// deleting it (T09), which is exactly the stable-directory contract
	// this whole package's Composition.Target = b.Name choice above relies
	// on: the same key T10 already proved reconciles with what cairn plants
	// under.
	if _, err := boot.Prepare(bootRoot, boot.Key(b.Name)); err != nil {
		return fmt.Errorf("launch: preparing boot directory: %w", err)
	}

	comp := compose.Composition{
		Target:   b.Name,
		Bundle:   bundleRoot,
		BootRoot: bootRoot,
	}

	argv, err := compose.Build(comp)
	if err != nil {
		return fmt.Errorf("launch: building cairn argv: %w", err)
	}

	result, _, err := boot.Invoke(ctx, runner, argv)
	if err != nil {
		// Already a *boot.InvokeError (or a JSON-decode error) carrying
		// everything worth showing -- see internal/boot.Invoke's own doc.
		// Wrapping it again here would only add a second generic prefix
		// on top of a message that is already specific.
		return err
	}

	cwd, err := resolveCwd(result)
	if err != nil {
		return err
	}

	binary, ok := harnessBinary[result.Provider]
	if !ok {
		return fmt.Errorf("launch: no harness binary known for provider %q", result.Provider)
	}
	fullArgv := append([]string{binary}, boot.HarnessArgv(result)...)

	return spawn(fullArgv, cwd)
}

// resolveCwd turns result.CwdPreference into the working directory
// SpawnITerm2 should use, per internal/boot/invoke.go's doc on that field:
// "boot_dir" is result.BootDir; "project_dir" is *result.Scope,
// dereferenced. Cairn's own contract should never report "project_dir"
// with a nil Scope, but resolveCwd does not silently fall back to BootDir
// if it somehow does -- it names exactly what happened and returns an
// error, matching this whole design's standing aversion to a silent
// fallback that would launch the harness in the wrong directory without
// saying so.
func resolveCwd(result boot.Result) (string, error) {
	switch result.CwdPreference {
	case "boot_dir":
		return result.BootDir, nil
	case "project_dir":
		if result.Scope == nil {
			return "", fmt.Errorf("launch: cwd_preference is %q but cairn reported no scope", result.CwdPreference)
		}
		return *result.Scope, nil
	default:
		return "", fmt.Errorf("launch: unrecognized cwd_preference %q", result.CwdPreference)
	}
}
