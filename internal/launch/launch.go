// Package launch turns a palette selection into a running terminal: an
// agent profile from the bundle, a launch profile from Tachyon's own store,
// a project to work in, and whatever the compose form added on top. It is
// small on purpose -- internal/compose (T08) already builds the cairn boot
// argv, internal/boot (T09/T10) already runs cairn and decodes its --json
// report, and internal/boot.SpawnITerm2 (T12, CW-20260903-0016) already
// knows how to open iTerm2 on an argv. This package's whole job is gluing
// those three together behind one Wails-bindable method,
// [Service.LaunchComposition].
//
// # Three things make a launch, and they come from three places
//
//	agent profile    the bundle          WHAT this is      cairn boot <target>
//	launch profile   ~/.config/tachyon   HOW it runs       --with <path>
//	project          Tachyon's own list  WHERE it works    --scope <path>
//
// That split is the 2026-09-10 ruling (Tesseract
// cairn_is_a_template_engine_not_an_authority, agent_setup_declares_no_runtime):
// agent-setup owns content and declares no runtime, cairn materializes a
// directory and knows its shape only where it must, and the launcher owns
// everything about a launch.
//
// A binding used to be all three at once, saved in the bundle, in a format
// cairn could not read. agent-setup retired all 34 of them and cairn dropped
// bindings and --save-as on top of that, so this package no longer resolves
// one, and internal/binding is gone. What replaced it is not a smaller
// binding: it is the same three facts, each owned by whoever actually knows
// it. See internal/launchprofile.
//
// # The provider is declared, never passed and never inferred
//
// No --provider flag is built anywhere in this package or in
// internal/compose. No profile in agent-setup declares a provider since
// 2026-09-10, so cairn's own default resolves to nothing and it refuses to
// render rather than writing one harness's files into another's directory.
// The provider comes from the launch profile's frontmatter, folded in
// through cairn's ordinary cascade.
//
// The consequence is worth stating plainly because it is the intended
// shape rather than a gap: a composition with no launch profile has no
// provider, and cairn refuses it. internal/launchprofile seeds a default so
// a first run has one.
//
// # No session handle, ever (D7)
//
// The exported method returns nothing but an error. This package stores
// nothing about what it started -- no PID, no *os.Process, no session id --
// and nothing in it can answer "is it still running." Tachyon is a
// launcher, not a console: a managed session with a list, attach and resume
// was considered and rejected (plan CW-20260518-0061, D7) because it pulls
// Tachyon back toward the daemon-backed stack this whole design
// deliberately separates from. See internal/boot.SpawnITerm2's own doc for
// the identical discipline one layer down: it starts osascript and does not
// wait for it either.
//
// # Skills, and prompts, are additive only (CW-20260903-0017's fail-alone
// criterion, extended to prompts by CW-20260904-0006)
//
// [CompositionInput.Skills] is exactly what [compose.Composition.Skills]
// becomes: skills to ADD on top of whatever the target's own profile
// cascade already resolves to, never a representation of what that cascade
// already carries. This package has no way to compute the latter and must
// never try -- see [CompositionInput]'s own doc for why, and
// internal/compose's package doc for the independent contributors (the
// profile cascade, any --with part including the launch profile itself)
// that make a Tachyon-side union not just redundant but actively wrong.
//
// [launchprofile.Profile] carries no skills or prompts field, so there is
// structurally nothing in what this package hands the frontend for a
// compose form's initial state to seed itself from even by accident -- the
// same absence [binding.Binding] used to provide, pinned by the same shape
// of guard in launch_test.go.
//
// [CompositionInput.Prompts] carries the identical property, for the
// identical reason -- cairn's own --prompt flag documents itself as
// "Additive only, for the reason --skill is".
package launch

import (
	"context"
	"fmt"

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/config"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
	"github.com/hollis-labs/tachyon/internal/state"
)

// harnessBinary maps a [boot.Result.Provider] value to the binary
// SpawnITerm2 actually execs: the two providers Cairn renders a boot
// directory for. This is a lookup table, not a plugin system -- adding
// Codex was a second entry here and a second case in [boot.HarnessArgv],
// with no new code path through this package, which is what Cairn's
// self-describing --json report was for.
//
// The value is a bare command name, resolved by the interactive shell
// iTerm2 opens rather than by this process. That is deliberate and unlike
// cairn, which internal/config resolves to an absolute path because a
// launchd-started Tachyon has no useful PATH of its own: the harness is not
// run by Tachyon at all, but by a login shell that has already sourced the
// operator's profile, so the name is what a person would type.
var harnessBinary = map[string]string{
	boot.ProviderClaude:   "claude",
	boot.ProviderCodex:    "codex",
	boot.ProviderOpenCode: "opencode",
}

// Service is bound to the frontend as a Wails service: the palette's
// launch entry point. Its exported method is callable from JavaScript as
// "github.com/hollis-labs/tachyon/internal/launch.Service.LaunchComposition".
//
// Like internal/manager.Service it holds a [bundle.RootStore] rather than a
// fixed path: every call resolves the active bundle root fresh, so a launch
// reads whichever bundle the manager is currently showing.
//
// The launch-profile store is separate and is NOT rooted in the bundle,
// which is the seam this whole design turns on. Changing the active bundle
// changes what agents are available; it does not change how they run.
type Service struct {
	store bundle.RootStore

	// launchDir overrides where launch profiles are read from. Empty means
	// [state.LaunchDir]. Tests set it; the app does not.
	launchDir string
}

// NewService returns a Service reading agent profiles from whichever bundle
// store.Resolve() names, and launch profiles from [state.LaunchDir].
func NewService(store bundle.RootStore) *Service {
	return &Service{store: store}
}

// NewServiceWithLaunchDir is [NewService] with the launch-profile store
// pointed somewhere else, so a test never touches the real
// ~/.config/tachyon/launch.
func NewServiceWithLaunchDir(store bundle.RootStore, launchDir string) *Service {
	return &Service{store: store, launchDir: launchDir}
}

// launchProfiles opens the store this service reads launch profiles from.
func (s *Service) launchProfiles() (launchprofile.Store, error) {
	if s.launchDir != "" {
		return launchprofile.Open(s.launchDir), nil
	}
	dir, err := state.LaunchDir()
	if err != nil {
		return launchprofile.Store{}, fmt.Errorf("launch: %w", err)
	}
	return launchprofile.Open(dir), nil
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
	// Target is the boot target -- either a saved binding's name or a bare
	// profile id, both accepted directly by Cairn. Required.
	Target string `json:"target"`

	// Skills are added on top of whatever the target's profile cascade
	// already resolves to. See this type's own doc for the rule governing
	// how the palette must build this slice.
	Skills []string `json:"skills"`

	// Prompts are added on top of whatever the target's profile cascade
	// already declares under spec.prompts (CW-20260904-0006) — a name each,
	// resolved by Cairn against prompts/, never a prompt's own content (see
	// internal/compose's "no delivery" section). Governed by exactly the
	// same additive-only rule as Skills, for the identical reason: Cairn's
	// own --prompt flag is additive only ("Additive only, for the reason
	// --skill is" — its own --help text), so a form that pre-checked a
	// target's own prompts and let a person uncheck one would silently keep
	// it anyway.
	Prompts []string `json:"prompts"`

	// LaunchProfile is the name of a launch profile in Tachyon's own store
	// (internal/launchprofile) -- the file that says HOW this runs: the
	// provider, the sandbox posture, whatever settings the launcher owns.
	// It is resolved to a path and prepended to [compose.Composition.Parts]
	// as the first --with, so everything else the compose form adds layers
	// over it.
	//
	// Empty sends no launch profile at all, which means no provider, which
	// cairn refuses. That is deliberate rather than a gap -- see the
	// package doc -- and it is why internal/launchprofile seeds a default.
	// This package does not substitute one: a launcher that silently picked
	// a provider would be inferring exactly the thing three components just
	// agreed nobody infers.
	//
	// It is a NAME and never a path. The store resolves it, so nothing the
	// frontend sends can point --with at an arbitrary file.
	LaunchProfile string `json:"launchProfile"`

	// Scope is the directory this instance works in: the selected
	// project's path. Empty sends no --scope at all, which cairn accepts
	// and which is right for a profile that holds no scope of its own.
	//
	// It can only come from here. Cairn refuses `scope:` as frontmatter --
	// it is not one of the eight keys -- so a launch profile cannot carry
	// one, and there is nothing left for this to override: it is the only
	// source rather than a correction to another.
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

	cairnPath, err := config.ResolveCairnPath()
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}

	launchPath, err := s.resolveLaunchProfile(input.LaunchProfile)
	if err != nil {
		return err
	}

	comp := compositionFromInput(input, bundleRoot, bootRoot, launchPath)

	return runComposition(context.Background(), comp, boot.ExecRunner(cairnPath), boot.SpawnITerm2)
}

// resolveLaunchProfile turns a launch profile's NAME into the path
// [compose.Composition.Parts] carries, or "" when the input named none.
//
// Resolving through the store rather than trusting a path from the frontend
// is what keeps --with pointed inside Tachyon's own directory: the store
// validates the name (internal/launchprofile.ValidateName) before it joins,
// so no value crossing the Wails boundary can name a file outside it.
//
// A named profile that does not exist is an error and never a silent
// fallback to the default. Falling back would launch a session under a
// posture the person did not pick, and the two failures look identical
// afterwards.
func (s *Service) resolveLaunchProfile(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	st, err := s.launchProfiles()
	if err != nil {
		return "", err
	}
	p, err := st.Get(name)
	if err != nil {
		return "", fmt.Errorf("launch: resolving launch profile %q: %w", name, err)
	}
	return p.Path, nil
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
// SetInput's own doc). In particular, Skills and Prompts each copy through
// unmodified: nothing here unions either with anything, looks anything up
// by them, or touches them at all beyond this direct assignment -- see the
// package doc's "additive only" section, and
// TestCompositionFromInput_SkillsPassThroughUnmodified /
// TestCompositionFromInput_PromptsPassThroughUnmodified.
func compositionFromInput(input CompositionInput, bundleRoot, bootRoot, launchPath string) compose.Composition {
	sets := make([]compose.Set, len(input.Sets))
	for i, set := range input.Sets {
		sets[i] = compose.Set{Slot: set.Slot, Value: set.Value}
	}

	parts := PartsWith(launchPath, input.Parts)

	// The boot directory is <boot-root>/<project>/<profile>/<launch profile>,
	// one segment per axis, and cairn plants the middle one itself: its own
	// layout is <boot-root>/<target>/<session>, and the target IS the agent
	// profile. So the project segment has to be folded into what cairn is
	// given as its boot root, and the launch profile becomes its session.
	//
	// Grouping by project rather than by role is what makes the tree
	// browsable: a person looks for "what is running on cairn", not "every
	// scope engineer has ever been booted at".
	//
	// All three segments are deterministic, which is the property that
	// matters: the same selection resolves to the same directory forever, so
	// a harness accrues one ~/.claude.json trust entry per composition
	// rather than one per launch. See boot.DefaultSession.
	return compose.Composition{
		Target:   input.Target,
		Bundle:   bundleRoot,
		BootRoot: boot.ProjectRoot(bootRoot, input.Scope),
		Session:  boot.SessionKey(input.LaunchProfile),
		Skills:   input.Skills,
		Prompts:  input.Prompts,
		Scope:    input.Scope,
		Sets:     sets,
		Parts:    parts,
	}
}

// spawnFunc matches [boot.SpawnITerm2]'s signature -- the seam
// launch_test.go substitutes a fake for, so tests assert on the argv, cwd
// and environment that reach the spawn step without ever invoking
// osascript.
type spawnFunc func(argv []string, cwd string, env []string) error

// runComposition is the orchestration core behind [Service.LaunchComposition]:
// clear whatever might already be planted at this composition's boot
// directory, build the argv
// (internal/compose.Build), run cairn (internal/boot.Invoke), then act on
// the report it printed -- resolve a cwd, build the provider's own flags
// (internal/boot.HarnessArgv), prepend the harness binary [harnessBinary]
// maps Provider to, provide the operator-owned resources Cairn named but
// deliberately did not render (internal/boot.PrepareHomeResources), expand
// the provider's environment amendments (internal/boot.Environment) -- and
// spawn. Neither caller duplicates any of this -- see this package's own doc
// for why that matters for the skills-additive-only guarantee in particular:
// there being exactly one place that turns a Composition into a running
// terminal is what makes "nothing here computes a skills union" a property
// of the whole package, not just of whichever caller happened to be audited.
//
// # Every provider difference is data, read from one report
//
// There is no branch on Provider anywhere in this function. A Codex launch
// and a Claude launch run the identical sequence; what differs is what
// Cairn's report says -- which flags the harness takes, which environment
// it needs, which operator-owned resources must be in place first -- and
// each of those is a call into internal/boot with the report as its whole
// input. That is why adding Codex added no code path here, and it is the
// property to preserve: a `switch result.Provider` in this function would
// be the first place a third provider costs more than a table entry.
//
// # Nothing is spawned until everything is ready
//
// Every failure below returns before spawn, so a launch that cannot be
// completed opens no terminal at all rather than one missing its
// credentials, its hooks or its environment. The boot directory Cairn just
// planted is left where it is: the next launch of the same target moves it
// aside (see boot.Prepare) rather than colliding with it.
//
// # The boot directory's identity is three segments, one per axis
//
//	<boot-root>/<project>/<profile>/<launch profile>
//
// comp.BootRoot already carries the project segment (see
// [compositionFromInput]); comp.Target is [boot.Key]'s seed and is cairn's
// own middle segment; comp.Session is the leaf.
//
// Only the middle one used to vary. A saved binding WAS the target, so
// cairn's <boot-root>/<target>/<session> layout gave every binding its own
// directory and the leaf could safely be a constant. With launch profiles
// the target is always the bare agent profile, so `engineer` under two
// launch profiles, or in two projects, is one target several times over --
// and a fixed leaf would plant all of them in one directory, replanted out
// from under whichever session got there first, with a settings document
// granting the wrong scope.
//
// What has not changed is that every segment is STABLE for a given
// composition, which is the T10 contract: the same selection resolves to the
// same directory forever, so a harness accrues one ~/.claude.json trust
// entry per composition rather than one per launch.
func runComposition(ctx context.Context, comp compose.Composition, runner boot.Runner, spawn spawnFunc) error {
	// Clear the target before cairn plants into it. cairn boot refuses an
	// already-occupied Current (bootdir.PlantFiles's ErrExists) -- without
	// this, only ever the very first launch of a given composition would
	// succeed. Prepare renames any existing directory aside rather than
	// deleting it (T09), which is exactly the stable-directory contract
	// this package relies on: the same segments T10 already proved
	// reconcile with what cairn plants under.
	// comp.BootRoot already carries the project segment, so this prepares
	// exactly the directory cairn is about to plant into: comp.BootRoot is
	// what becomes --boot-root, and cairn appends <target>/<session> to it.
	if _, err := boot.Prepare(comp.BootRoot, boot.Key(comp.Target), comp.Session); err != nil {
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
	flags, err := boot.HarnessArgv(result)
	if err != nil {
		return err
	}
	fullArgv := append([]string{binary}, flags...)

	// The source home is resolved from the environment as it stands, before
	// anything computes the amended one below. Doing it the other way round
	// -- reading CODEX_HOME after deciding it should be the boot directory
	// -- links every operator-owned resource to itself. boot.PrepareHomeResources
	// refuses that outcome as well, but the ordering here is what makes it
	// never arise.
	homeKey, err := boot.HomeRedirectKey(result)
	if err != nil {
		return err
	}
	sourceHome, err := boot.ResolveHome(homeKey)
	if err != nil {
		return err
	}
	if _, err := boot.PrepareHomeResources(result, sourceHome); err != nil {
		return err
	}

	env, err := boot.Environment(result)
	if err != nil {
		return err
	}

	return spawn(fullArgv, cwd, env)
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

// PartsWith puts a launch profile's path in front of a composition's own
// --with parts.
//
// The launch profile goes FIRST, so everything the compose form adds layers
// OVER it rather than under it. cairn folds parts closest-wins in the order
// given, so a one-off --with meant to override the launch profile's provider
// or settings has to come after it — and a person who adds a part for one
// launch expects their addition to win over a stored default.
//
// It is exported because internal/preview must build the same list: a
// preview that resolved a different set of parts than the launch would be a
// preview of a different composition, which is the one thing a preview must
// never be. An empty launchPath contributes nothing.
func PartsWith(launchPath string, parts []string) []string {
	out := make([]string, 0, len(parts)+1)
	if launchPath != "" {
		out = append(out, launchPath)
	}
	return append(out, parts...)
}

// Target is one agent profile the palette can launch: what the session IS,
// as distinct from the launch profile that says how it runs.
type Target struct {
	// ID is the boot target — the positional argument to `cairn boot`.
	ID string `json:"id"`
	// Name and Description are the profile's own frontmatter, for the row.
	// Empty when the profile declares none.
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Targets lists the agent profiles in the active bundle that can be booted,
// sorted by id.
//
// Abstract profiles are excluded. Cairn refuses to boot one — base is
// "extended rather than booted" — so offering it produces a refusal a person
// cannot act on, and cairn's own `list` makes the same split.
//
// Parts are NOT excluded, which is deliberate and is cairn's rule rather
// than a convenience: "a part is an ordinary profile, so anything composable
// is also bootable and inspectable on its own." A launcher that hid them
// would be inventing a distinction the catalog does not make.
//
// Nothing here reads a provider, and there is nothing to read: no profile in
// agent-setup declares one. What makes a target launchable is the launch
// profile chosen beside it.
func (s *Service) Targets() ([]Target, error) {
	root, err := s.store.Resolve()
	if err != nil {
		return nil, fmt.Errorf("launch: resolving bundle root: %w", err)
	}
	b, err := bundle.Open(root)
	if err != nil {
		return nil, fmt.Errorf("launch: opening bundle %s: %w", root, err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		return nil, fmt.Errorf("launch: reading profiles from %s: %w", root, err)
	}
	out := make([]Target, 0, len(profiles))
	for _, p := range profiles {
		if p.Header.Abstract {
			continue
		}
		out = append(out, Target{
			ID:          string(p.ID),
			Name:        p.Header.Name,
			Description: p.Header.Description,
		})
	}
	return out, nil
}
