// Package launch turns a picked binding, or a full compose-form
// selection built on top of one, into a running terminal: the palette's
// entry point once a target has been chosen from the list
// internal/binding.Service already reads. It is small on purpose --
// internal/compose (T08) already builds the cairn boot argv, internal/boot
// (T09/T10) already runs cairn and decodes its --json report, and
// internal/boot.SpawnITerm2 (T12, CW-20260903-0016) already knows how to
// open iTerm2 on an argv. This package's whole job is gluing those three
// together behind two Wails-bindable methods, [Service.Launch] (a bare
// binding name -- unchanged since T12) and [Service.LaunchComposition]
// (T13, CW-20260903-0017 -- the palette's compose form: skills to add,
// a scope override, one-off --set values, additional --with parts, all
// layered on top of the same target). Both funnel through the same
// unexported orchestration core, [runComposition], so there is exactly one
// implementation of "build the argv, run cairn, resolve a cwd, spawn" --
// see that function's own doc.
//
// # No session handle, ever (D7)
//
// Neither exported method returns anything but an error. This package
// stores nothing about what it started -- no PID, no *os.Process, no
// session id -- and nothing in it can answer "is it still running."
// Tachyon is a launcher, not a console: a managed session with a list,
// attach and resume was considered and rejected (plan CW-20260518-0061,
// D7) because it pulls Tachyon back toward the daemon-backed stack this
// whole design deliberately separates from. See internal/boot.SpawnITerm2's
// own doc for the identical discipline one layer down: it starts
// osascript and does not wait for it either.
//
// # Skills are additive only (CW-20260903-0017's fail-alone criterion)
//
// [CompositionInput.Skills] is exactly what [compose.Composition.Skills]
// becomes: skills to ADD on top of whatever the target's own profile
// cascade already resolves to, never a representation of what that cascade
// already carries. This package has no way to compute the latter and must
// never try -- see [CompositionInput]'s own doc for why, and
// internal/compose's package doc for the three independent contributors
// (the profile cascade, any --with part, a binding's own skills field)
// that make a Tachyon-side union not just redundant but actively wrong.
// [binding.Binding] itself carries no skills field at all: there is
// structurally nothing in this package's own data, or in what
// internal/binding.Service ever hands the frontend, for a compose form's
// initial state to seed itself from even by accident. See
// TestBindingCarriesNoSkillsFieldToSeedFrom in launch_test.go, which pins
// that absence down as a permanent regression guard.
package launch

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/state"
)

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
// launch entry point. Its exported methods are callable from JavaScript as
// "github.com/hollis-labs/tachyon/internal/launch.Service.Launch" and
// "...Service.LaunchComposition".
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

// SetInput is one one-off --set slot=value pair as the frontend spells it
// -- lowercase JSON keys, matching every other type this package's Service
// hands across the Wails boundary ([binding.Binding], [ListResult], ...).
// [compose.Set] itself carries no JSON tags (that package builds nothing
// for JS -- see its own doc, "pure, on purpose"), so LaunchComposition
// converts a slice of these into a slice of those rather than adding JSON
// tags to a package that has no other reason to know JSON exists.
type SetInput struct {
	Slot  string `json:"slot"`
	Value string `json:"value"`
}

// CompositionInput is the palette's whole compose-form state: everything a
// person can add on top of a chosen target before launching
// (CW-20260903-0017). It mirrors [compose.Composition]'s own optional
// fields exactly -- Skills, Scope, Sets, Parts -- field for field, so every
// control in the form maps onto exactly one Cairn flag and nothing here is
// synthesized. It deliberately has no Bundle or BootRoot field: unlike
// Target, Skills, Scope, Sets and Parts, which are the palette's own
// choices, those two are never a per-call choice -- [Service.LaunchComposition]
// always resolves Bundle from the actively showing bundle root and
// BootRoot from [state.BootRoot], the same way [Service.Launch] already
// does for Launch(name), and for the identical D9 reason: a boot root a
// caller could leave unset, or point anywhere, is exactly the hazard
// internal/compose's package doc names.
//
// # Skills starts empty, always (the fail-alone criterion)
//
// Skills must be built by the palette starting from an empty slice and
// growing only by direct, explicit user action -- never seeded from a
// binding's or profile's own already-resolved skills. Cairn's own --skill
// flag is additive only (its own --help text: "nothing in cairn removes a
// member of a collection keyed by its own id, so a session that wants
// fewer skills boots a different profile"), so a form that pre-checks the
// profile's existing skills and lets a person UNCHECK one would silently
// keep that skill in the boot directory regardless -- a wrong result that
// looks right. This package cannot enforce that the frontend's initial
// state is empty (that discipline lives in Palette.jsx, and in the fact
// that [binding.Binding] exposes no skills field at all for a form to seed
// from even by accident -- see the package doc). What this package DOES
// guarantee is the other half: Skills flows straight into
// [compose.Composition.Skills] with no merge, union or lookup of any kind
// applied to it anywhere in this method -- exactly what was typed, and
// nothing else.
type CompositionInput struct {
	// Target is the boot target -- a saved binding's name, the same value
	// [Service.Launch] takes as its own bare argument. Required.
	Target string `json:"target"`

	// Skills are added on top of whatever the target's profile cascade
	// already resolves to. See this type's own doc for the rule governing
	// how the palette must build this slice.
	Skills []string `json:"skills"`

	// Scope overrides the target's own scope. Empty leaves the target's
	// resolved scope in force -- no --scope is sent at all, the same
	// choice [launch] already makes for the plain Launch(name) path (see
	// that function's own doc for why an unset override must stay unset
	// rather than restate what Cairn already resolves on its own).
	Scope string `json:"scope"`

	// Sets are one-off --set slot=value overrides, applied in the order
	// given.
	Sets []SetInput `json:"sets"`

	// Parts are additional --with <part> values, layered over whatever the
	// target's own extends chain already resolves, in the order given.
	Parts []string `json:"parts"`
}

// LaunchComposition runs the palette's full compose form: input.Target
// plus whatever skills, scope override, --set values and --with parts the
// person added, through the same pipeline [Launch] uses -- [runComposition]
// -- so there is exactly one implementation of "build the argv, run cairn,
// resolve a cwd, spawn." Like [Launch], it returns only an error, safe to
// show a person directly (see this package's doc, "no session handle,
// ever").
//
// Unlike [Launch], LaunchComposition does not resolve input.Target through
// [binding.Store] first -- there is nothing to check it against beyond
// what a real `cairn boot` invocation already validates far more
// authoritatively than a second, necessarily-partial lookup here could
// (D8: nothing in this package's own compose path validates; Cairn's own
// stderr on a bad target is a debugging aid, exactly as it already is for
// every other failure this method's argv can produce). It only needs the
// active bundle root, which [bundle.RootStore.Resolve] gives directly.
func (s *Service) LaunchComposition(input CompositionInput) error {
	if input.Target == "" {
		return fmt.Errorf("launch: target is required")
	}

	bundleRoot, err := s.store.Resolve()
	if err != nil {
		return fmt.Errorf("launch: resolving bundle root: %w", err)
	}

	bootRoot, err := state.BootRoot()
	if err != nil {
		return fmt.Errorf("launch: resolving boot root: %w", err)
	}

	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		return fmt.Errorf("launch: cairn not found on PATH: %w", err)
	}

	comp := compositionFromInput(input, bundleRoot, bootRoot)

	return runComposition(context.Background(), comp, bootRoot, boot.ExecRunner(cairnPath), boot.SpawnITerm2)
}

// compositionFromInput builds the [compose.Composition] LaunchComposition
// hands to [runComposition] from the palette's own input plus the two
// values only [Service.LaunchComposition] is allowed to supply (bundleRoot,
// bootRoot -- see [CompositionInput]'s doc on why those are never part of
// the input itself). It is a pure, allocation-only mapping -- no lookup, no
// filesystem access, no network -- kept separate from
// [Service.LaunchComposition] specifically so launch_test.go can assert on
// exactly what a given CompositionInput turns into without a real bundle
// store, a real boot root or cairn on PATH, the same reason [runComposition]
// itself is kept separate from both Service methods.
//
// Every field copies straight across with no transformation but the
// [SetInput]-to-[compose.Set] conversion (a JSON-tag concern only -- see
// SetInput's own doc). In particular, Skills copies through unmodified:
// nothing here unions it with anything, looks anything up by it, or
// touches it at all beyond this direct assignment -- see the package doc's
// "skills are additive only" section, and TestCompositionFromInput_SkillsPassThroughUnmodified.
func compositionFromInput(input CompositionInput, bundleRoot, bootRoot string) compose.Composition {
	sets := make([]compose.Set, len(input.Sets))
	for i, set := range input.Sets {
		sets[i] = compose.Set{Slot: set.Slot, Value: set.Value}
	}

	return compose.Composition{
		Target:   input.Target,
		Bundle:   bundleRoot,
		BootRoot: bootRoot,
		Skills:   input.Skills,
		Scope:    input.Scope,
		Sets:     sets,
		Parts:    input.Parts,
	}
}

// resolveBinding opens a [binding.Store] over the active bundle via
// [binding.Open] -- the same constructor internal/binding.Service.open()
// itself now calls, so this package keeps no copy of its own of the
// bindings file's name (T24 closed that leak; see [binding.Open]'s doc) --
// and looks name up in it, returning both the resolved binding and the
// bundle root it came from, so Launch does not resolve the same root
// twice (compose.Composition.Bundle needs it too).
func (s *Service) resolveBinding(name string) (binding.Binding, string, error) {
	root, err := s.store.Resolve()
	if err != nil {
		return binding.Binding{}, "", fmt.Errorf("launch: resolving bundle root: %w", err)
	}
	b, err := binding.Open(root).Get(name)
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
// the minimal composition [Service.Launch] has always built (Target,
// Bundle and BootRoot only -- Skills/Scope/Sets/Parts stay at their zero
// value, matching Launch(name)'s own "just this binding, nothing added"
// contract) and hand it to [runComposition]. Kept separate from the
// Service method -- which also resolves the binding, the boot root and
// the cairn path, none of which a unit test should have to touch -- so a
// test can drive exactly this part with a fake runner and a fake spawn
// func instead of a real cairn binary and a real terminal.
//
// b.Name becomes Composition.Target, not b.Profile: `cairn boot <name>`
// resolves the binding itself, server-side, before this package's own
// resolveBinding call ever runs -- via Cairn's own catalog package (a
// directory of files read whole, per its own doc "the catalog is the
// store"), which as of this writing already reads a bindings/ directory of
// one file per binding directly off disk, not this package's single
// bindings file (see internal/binding's doc on "the interface is the
// contract, not the file format" for what this package reads instead, and
// why, and CW-20260904-0002 / T23 for migrating this package's own reader
// to match). Either way, that server-side resolution is precisely how b's
// own Profile and Scope get resolved -- so Composition.Scope is
// deliberately left at its zero value here rather than set from b.Scope.
// Setting it would send an explicit --scope that
// merely restates what Cairn already resolves on its own from the binding
// it just looked up by name. cmd/tachyon's own Config.Scope (T11) makes
// the identical choice: it leaves Scope empty by default, only ever
// populating it from an explicit CLI override that this package's single
// Launch(name) signature has no way to ask for.
func launch(ctx context.Context, b binding.Binding, bundleRoot, bootRoot string, runner boot.Runner, spawn spawnFunc) error {
	comp := compose.Composition{
		Target:   b.Name,
		Bundle:   bundleRoot,
		BootRoot: bootRoot,
	}
	return runComposition(ctx, comp, bootRoot, runner, spawn)
}

// runComposition is the one orchestration core behind both [launch] (bare
// Launch(name), a minimal Composition) and [Service.LaunchComposition]
// (the palette's full compose form): clear whatever might already be
// planted at comp.Target's boot directory, build the argv
// (internal/compose.Build), run cairn (internal/boot.Invoke), resolve a
// cwd from what it reported, prepend the harness binary
// [harnessBinary] maps Provider to, and spawn. Neither caller duplicates
// any of this -- see this package's own doc for why that matters for the
// skills-additive-only guarantee in particular: there being exactly one
// place that turns a Composition into a running terminal is what makes
// "nothing here computes a skills union" a property of the whole package,
// not just of whichever caller happened to be audited.
//
// comp.Target is always used as [boot.Key]'s seed, unmodified: both
// callers build comp.Target from a saved binding's name (see [launch]'s
// own doc and [Service.LaunchComposition]'s), so the same binding always
// resolves to the same stable boot directory regardless of what a
// particular launch also composed on top of it -- exactly the T10
// stable-directory contract this package has relied on since before this
// function had two callers.
func runComposition(ctx context.Context, comp compose.Composition, bootRoot string, runner boot.Runner, spawn spawnFunc) error {
	// Clear the target before cairn plants into it. cairn boot refuses an
	// already-occupied Current (bootdir.PlantFiles's ErrExists) -- without
	// this, only ever the very first launch of a given binding would
	// succeed. Prepare renames any existing current aside rather than
	// deleting it (T09), which is exactly the stable-directory contract
	// this whole package's Composition.Target = b.Name choice above relies
	// on: the same key T10 already proved reconciles with what cairn plants
	// under.
	if _, err := boot.Prepare(bootRoot, boot.Key(comp.Target)); err != nil {
		return fmt.Errorf("launch: preparing boot directory: %w", err)
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
