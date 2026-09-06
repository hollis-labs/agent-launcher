package launch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/testbundle"
)

// This is a white-box test (package launch, not launch_test) because it
// drives the unexported launch() and Service.resolveBinding() directly --
// the orchestration seams a fake boot.Runner and a fake spawn func plug
// into, per this task's own testing section: fakes for the runner and the
// spawn step, and a real t.TempDir() bundle with a hand-written bindings/
// directory for binding resolution, without ever running cairn or
// osascript for real.

// fakeRunner is a [boot.Runner] that ignores argv (beyond recording it)
// and hands back canned stdout/stderr/err -- the same pattern
// internal/boot/invoke_test.go's own fakeRunner uses.
type fakeRunner struct {
	gotArgv []string
	stdout  []byte
	stderr  []byte
	err     error
}

func (f *fakeRunner) run(ctx context.Context, argv []string) ([]byte, []byte, error) {
	f.gotArgv = argv
	return f.stdout, f.stderr, f.err
}

// spawnRecorder is a fake spawnFunc that records exactly what it was
// called with, so tests assert on the argv and cwd that reached the spawn
// step without ever invoking osascript.
type spawnRecorder struct {
	called bool
	argv   []string
	cwd    string
	env    []string
}

func (r *spawnRecorder) spawn(argv []string, cwd string, env []string) error {
	r.called = true
	r.argv = argv
	r.cwd = cwd
	r.env = env
	return nil
}

// --- fixtures --------------------------------------------------------------

// bootDirFixture mirrors internal/boot/invoke_test.go's own
// realBootReportFixture shape (a real captured `cairn boot --json`
// document) with cwd_preference: "boot_dir".
const bootDirFixture = `{
  "boot_dir": "/state/boot/eng-nanite/current",
  "provider": "claude",
  "scope": "/Users/chrispian/dev/hollis-labs/apps/nanite",
  "settings_path": "/state/boot/eng-nanite/current/.claude/settings.json",
  "cwd_preference": "boot_dir",
  "project_dir_arg": ["--add-dir", "{{.ProjectDir}}"]
}`

// projectDirFixture is the same shape with cwd_preference: "project_dir"
// and a non-nil scope -- the other value invoke.go's CwdPreference doc
// documents.
const projectDirFixture = `{
  "boot_dir": "/state/boot/eng-setup/current",
  "provider": "claude",
  "scope": "/Users/chrispian/dev/projects/agent-setup",
  "settings_path": "/state/boot/eng-setup/current/.claude/settings.json",
  "cwd_preference": "project_dir",
  "project_dir_arg": ["--add-dir", "{{.ProjectDir}}"]
}`

// projectDirNilScopeFixture is the combination Cairn's own contract should
// never produce -- cwd_preference: "project_dir" with scope: null -- which
// resolveCwd must still turn into a real, named error rather than a silent
// fallback.
const projectDirNilScopeFixture = `{
  "boot_dir": "/state/boot/broken/current",
  "provider": "claude",
  "scope": null,
  "settings_path": null,
  "cwd_preference": "project_dir",
  "project_dir_arg": null
}`

// unknownProviderFixture reports a provider this package's harnessBinary
// map has no entry for. "opencode" is deliberately real: cairn knows the
// name and refuses to render a layout for it, so this is the shape a
// launcher would actually meet rather than an invented word.
const unknownProviderFixture = `{
  "boot_dir": "/state/boot/opencode-thing/current",
  "provider": "opencode",
  "scope": null,
  "settings_path": null,
  "cwd_preference": "boot_dir",
  "project_dir_arg": null
}`

// codexFixture is the shape a real `cairn boot <target> --provider codex
// --json` prints, reduced to the keys this package acts on -- see
// internal/boot/invoke_test.go's realCodexBootReportFixture for the whole
// captured document. Its boot_dir points nowhere real; the tests that need
// a boot directory on disk build their own.
const codexFixture = `{
  "boot_dir": "/state/boot/codex-coord-agent-setup/current",
  "provider": "codex",
  "scope": "/Users/chrispian/dev/projects/agent-setup",
  "settings_path": "/state/boot/codex-coord-agent-setup/current/config.toml",
  "cwd_preference": "boot_dir",
  "project_dir_arg": ["--add-dir", "{{.ProjectDir}}"],
  "env_amendments": ["CODEX_HOME={{.BootDir}}"],
  "home_resource_paths": null
}`

// --- launch() ----------------------------------------------------------

func TestLaunch_CwdPreferenceBootDir(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "eng-nanite", Profile: "engineer", Scope: "/Users/chrispian/dev/hollis-labs/apps/nanite"}

	if err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !rec.called {
		t.Fatal("spawn was not called")
	}
	if rec.cwd != "/state/boot/eng-nanite/current" {
		t.Errorf("cwd = %q; want BootDir", rec.cwd)
	}
	want := []string{"claude", "--settings", "/state/boot/eng-nanite/current/.claude/settings.json"}
	if len(rec.argv) != len(want) {
		t.Fatalf("argv = %v; want %v", rec.argv, want)
	}
	for i := range want {
		if rec.argv[i] != want[i] {
			t.Fatalf("argv = %v; want %v", rec.argv, want)
		}
	}

	// Composition.Scope must stay unset: b.Scope is not forwarded as an
	// explicit --scope, because Target (the binding's own name) already
	// resolves it via internal/binding, not this file -- see launch()'s own doc comment.
	for _, a := range fr.gotArgv {
		if a == "--scope" {
			t.Fatalf("cairn argv %v contains --scope; Composition.Scope must stay unset", fr.gotArgv)
		}
	}
	// And the target passed to cairn is the binding's NAME, not its
	// profile.
	found := false
	for i, a := range fr.gotArgv {
		if a == "boot" && i+1 < len(fr.gotArgv) {
			if fr.gotArgv[i+1] != "eng-nanite" {
				t.Fatalf("cairn boot target = %q; want the binding's name %q", fr.gotArgv[i+1], "eng-nanite")
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("cairn argv %v has no boot <target>", fr.gotArgv)
	}
}

func TestLaunch_CwdPreferenceProjectDir(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(projectDirFixture)}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "eng-setup", Profile: "engineer", Scope: "/Users/chrispian/dev/projects/agent-setup"}

	if err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !rec.called {
		t.Fatal("spawn was not called")
	}
	if rec.cwd != "/Users/chrispian/dev/projects/agent-setup" {
		t.Errorf("cwd = %q; want the reported scope, dereferenced", rec.cwd)
	}
}

func TestLaunch_ProjectDirPreferenceWithNilScopeIsAnError(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(projectDirNilScopeFixture)}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "broken", Profile: "x", Scope: "/x"}

	err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn)
	if err == nil {
		t.Fatal("launch returned no error for cwd_preference project_dir with a nil scope")
	}
	if rec.called {
		t.Fatal("spawn was called despite an unresolvable cwd")
	}
}

func TestLaunch_UnrecognizedCwdPreferenceIsAnError(t *testing.T) {
	doc := `{"boot_dir":"/x/current","provider":"claude","scope":null,"settings_path":null,"cwd_preference":"something_else","project_dir_arg":null}`
	fr := &fakeRunner{stdout: []byte(doc)}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "x", Profile: "x", Scope: "/x"}

	err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn)
	if err == nil {
		t.Fatal("launch returned no error for an unrecognized cwd_preference")
	}
	if rec.called {
		t.Fatal("spawn was called despite an unrecognized cwd_preference")
	}
}

func TestLaunch_UnknownProviderIsAnError(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(unknownProviderFixture)}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "opencode-thing", Profile: "x", Scope: "/x"}

	err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn)
	if err == nil {
		t.Fatal("launch returned no error for a provider with no known harness binary")
	}
	if rec.called {
		t.Fatal("spawn was called despite an unmapped provider")
	}
}

func TestLaunch_InvokeErrorPropagatesAndSpawnNeverCalled(t *testing.T) {
	underlying := errors.New("exit status 1")
	fr := &fakeRunner{stderr: []byte(`cairn: profile "x" not found`), err: underlying}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "eng-nanite", Profile: "engineer", Scope: "/x"}

	err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn)
	if err == nil {
		t.Fatal("launch returned no error when cairn failed")
	}
	if rec.called {
		t.Fatal("spawn was called despite cairn failing")
	}
	var invokeErr *boot.InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("error is not a *boot.InvokeError: %T (%v)", err, err)
	}
}

func TestLaunch_ComposeBuildErrorPropagatesWithoutInvokingRunner(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "eng-nanite", Profile: "engineer", Scope: "/x"}

	// Empty bootRoot: boot.Prepare refuses it ("root is required") before
	// compose.Build ever runs (compose.Build would separately refuse via
	// compose.ErrNoBootRoot if it were reached) -- either way this must
	// never reach the runner.
	err := launch(context.Background(), b, "/bundle/root", "", fr.run, rec.spawn)
	if err == nil {
		t.Fatal("launch returned no error for a missing boot root")
	}
	if fr.gotArgv != nil {
		t.Fatalf("runner was invoked despite compose.Build failing: argv=%v", fr.gotArgv)
	}
	if rec.called {
		t.Fatal("spawn was called despite compose.Build failing")
	}
}

func TestLaunch_RelaunchingSameBindingMovesPreviousAsideInsteadOfFailing(t *testing.T) {
	bootRoot := t.TempDir()
	b := binding.Binding{Name: "eng-nanite", Profile: "engineer", Scope: "/Users/chrispian/dev/hollis-labs/apps/nanite"}

	fr1 := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec1 := &spawnRecorder{}
	if err := launch(context.Background(), b, "/bundle/root", bootRoot, fr1.run, rec1.spawn); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	if !rec1.called {
		t.Fatal("first launch: spawn was not called")
	}

	// Without boot.Prepare, this second call is exactly the case that used
	// to reach a real `cairn boot` and fail with "boot directory already
	// exists" -- fakeRunner can't reproduce cairn's own refusal (it always
	// succeeds), so what this test actually proves is the thing that makes
	// that refusal avoidable in the first place: Prepare clears
	// bootRoot/<key>/current on every call, unconditionally, before cairn
	// (real or fake) ever runs.
	current := filepath.Join(bootRoot, boot.Key(b.Name), boot.CurrentSegment)
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("seeding an existing current dir: %v", err)
	}

	fr2 := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec2 := &spawnRecorder{}
	if err := launch(context.Background(), b, "/bundle/root", bootRoot, fr2.run, rec2.spawn); err != nil {
		t.Fatalf("second launch (relaunch): %v", err)
	}
	if !rec2.called {
		t.Fatal("second launch: spawn was not called")
	}

	keyDir := filepath.Join(bootRoot, boot.Key(b.Name))
	entries, err := os.ReadDir(keyDir)
	if err != nil {
		t.Fatalf("reading %s: %v", keyDir, err)
	}
	prevCount := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), boot.PrevPrefix) {
			prevCount++
		}
	}
	if prevCount != 1 {
		t.Fatalf("found %d %s* entries under %s; want exactly 1", prevCount, boot.PrevPrefix, keyDir)
	}
	// Prepare's own postcondition: nothing exists at Current when it
	// returns without error (planting a fresh one is cairn's job, which
	// this fakeRunner never actually does) -- so its absence here is what
	// proves the second launch() call reached Prepare and it cleared the
	// seeded directory rather than erroring out on it.
	if _, err := os.Stat(current); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current (%s) still exists after relaunch (err=%v); want it cleared", current, err)
	}
}

// --- Service.resolveBinding ------------------------------------------------

// writeBindingFile writes bindings/<name>.yaml directly under bundleRoot —
// the same per-file shape [binding.Open] reads (CW-20260904-0002 / T23).
func writeBindingFile(t *testing.T, bundleRoot, name, contents string) {
	t.Helper()
	dir := filepath.Join(bundleRoot, "bindings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s.yaml: %v", name, err)
	}
}

func newRootStore(t *testing.T, bundleRoot string) bundle.RootStore {
	t.Helper()
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save(bundleRoot); err != nil {
		t.Fatalf("RootStore.Save: %v", err)
	}
	return store
}

func TestResolveBinding_FindsBindingAndReturnsBundleRoot(t *testing.T) {
	bundleDir := t.TempDir()
	writeBindingFile(t, bundleDir, "eng-nanite", "profile: engineer\nscope: /Users/chrispian/dev/hollis-labs/apps/nanite\n")

	store := newRootStore(t, bundleDir)
	svc := NewService(store)

	b, root, err := svc.resolveBinding("eng-nanite")
	if err != nil {
		t.Fatalf("resolveBinding: %v", err)
	}
	if b.Name != "eng-nanite" || b.Profile != "engineer" || b.Scope != "/Users/chrispian/dev/hollis-labs/apps/nanite" {
		t.Errorf("binding = %+v", b)
	}
	wantRoot, err := bundle.ExpandRoot(bundleDir)
	if err != nil {
		t.Fatalf("ExpandRoot: %v", err)
	}
	if root != wantRoot {
		t.Errorf("root = %q; want %q", root, wantRoot)
	}
}

func TestResolveBinding_NotFoundWrapsBindingErrNotFound(t *testing.T) {
	bundleDir := t.TempDir()
	writeBindingFile(t, bundleDir, "eng-nanite", "profile: engineer\nscope: /x\n")

	store := newRootStore(t, bundleDir)
	svc := NewService(store)

	_, _, err := svc.resolveBinding("does-not-exist")
	if err == nil {
		t.Fatal("resolveBinding returned no error for a missing binding")
	}
	if !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("error does not wrap binding.ErrNotFound: %v", err)
	}
}

// --- resolveCwd --------------------------------------------------------

func strPtr(s string) *string { return &s }

func TestResolveCwd(t *testing.T) {
	cases := []struct {
		name    string
		result  boot.Result
		want    string
		wantErr bool
	}{
		{
			name:   "boot_dir",
			result: boot.Result{BootDir: "/a/current", CwdPreference: "boot_dir"},
			want:   "/a/current",
		},
		{
			name:   "project_dir with scope",
			result: boot.Result{BootDir: "/a/current", CwdPreference: "project_dir", Scope: strPtr("/scope/path")},
			want:   "/scope/path",
		},
		{
			name:    "project_dir with nil scope is an error",
			result:  boot.Result{BootDir: "/a/current", CwdPreference: "project_dir", Scope: nil},
			wantErr: true,
		},
		{
			name:    "unrecognized preference is an error",
			result:  boot.Result{BootDir: "/a/current", CwdPreference: "somewhere_else"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCwd(tc.result)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveCwd(%+v) returned no error", tc.result)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCwd(%+v): %v", tc.result, err)
			}
			if got != tc.want {
				t.Fatalf("resolveCwd(%+v) = %q; want %q", tc.result, got, tc.want)
			}
		})
	}
}

// --- compositionFromInput (CW-20260903-0017 / T13) ----------------------
//
// These exercise the pure CompositionInput -> compose.Composition mapping
// LaunchComposition hands to runComposition, without a real bundle store,
// a real boot root or cairn on PATH -- see compositionFromInput's own doc
// for why it is kept separate for exactly this purpose.

// TestCompositionFromInput_MapsEveryFieldToTheMatchingCairnFlag builds one
// CompositionInput exercising every field at once and checks the argv
// compose.Build produces from it names every value in the fixed order the
// target contract specifies (internal/compose's own package doc) -- the
// same "every control at once" shape compose_test.go's own TestBuild
// covers for compose.Composition directly, run here one layer up, from
// exactly the shape the frontend sends across the Wails boundary.
func TestCompositionFromInput_MapsEveryFieldToTheMatchingCairnFlag(t *testing.T) {
	input := CompositionInput{
		Target:  "eng-nanite",
		Skills:  []string{"skill-one", "skill-two"},
		Prompts: []string{"report", "onboarding"},
		Scope:   "/scope/path",
		Sets: []SetInput{
			{Slot: "slot-one", Value: "value-one"},
		},
		Parts: []string{"extra-part"},
	}

	comp := compositionFromInput(input, "/bundle/root", "/state/boot/root")

	argv, err := compose.Build(comp)
	if err != nil {
		t.Fatalf("compose.Build: unexpected error: %v", err)
	}

	want := []string{
		"boot", "eng-nanite",
		"--profile", "/bundle/root",
		"--boot-root", "/state/boot/root",
		"--session", "current",
		"--with", "extra-part",
		"--skill", "skill-one,skill-two",
		"--prompt", "report,onboarding",
		"--set", "slot-one=value-one",
		"--scope", "/scope/path",
		"--json",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv =\n  %#v\nwant\n  %#v", argv, want)
	}
}

// TestCompositionFromInput_BareProfileIsACompleteTarget is T33's narrow
// backend contract: the palette does not need to resolve or select a binding
// before launch. A profile id is already a valid Cairn boot target, and the
// mapping must preserve it as the positional target while carrying every
// one-time addition exactly as it does for a binding name.
func TestCompositionFromInput_BareProfileIsACompleteTarget(t *testing.T) {
	input := CompositionInput{
		Target:  "engineer",
		Parts:   []string{"writer", "reviewer"},
		Skills:  []string{"qstatus"},
		Prompts: []string{"report"},
		Sets:    []SetInput{{Slot: "role", Value: "marker"}},
		Scope:   "/literal/project/path",
	}

	comp := compositionFromInput(input, "/bundle/root", "/state/boot/root")
	argv, err := compose.Build(comp)
	if err != nil {
		t.Fatalf("compose.Build: %v", err)
	}

	want := []string{
		"boot", "engineer",
		"--profile", "/bundle/root",
		"--boot-root", "/state/boot/root",
		"--session", "current",
		"--with", "writer",
		"--with", "reviewer",
		"--skill", "qstatus",
		"--prompt", "report",
		"--set", "role=marker",
		"--scope", "/literal/project/path",
		"--json",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("bare-profile argv =\n  %#v\nwant\n  %#v", argv, want)
	}
}

// TestCompositionFromInput_EmptyInputProducesTheSameMinimalArgvAsBareLaunch
// is the additive-only guarantee's other half, stated as an argv-shape
// fact rather than a UI rule: a CompositionInput with every optional field
// left at its zero value -- exactly what Palette.jsx's initial compose-form
// state is (see Palette.jsx's own comment on this) -- must produce an argv
// with no --skill, --scope, --set or --with at all, identical in shape to
// what [launch] (the bare Launch(name) path) has always produced. If this
// ever failed, it would mean compositionFromInput started synthesizing
// something Skills/Scope/Sets/Parts being empty should never produce --
// exactly the class of bug the fail-alone acceptance criterion exists to
// catch, caught here at the pure-mapping layer rather than only by
// eyeballing the frontend.
func TestCompositionFromInput_EmptyInputProducesTheSameMinimalArgvAsBareLaunch(t *testing.T) {
	input := CompositionInput{Target: "eng-nanite"}
	comp := compositionFromInput(input, "/bundle/root", "/state/boot/root")

	argv, err := compose.Build(comp)
	if err != nil {
		t.Fatalf("compose.Build: unexpected error: %v", err)
	}

	want := []string{
		"boot", "eng-nanite",
		"--profile", "/bundle/root",
		"--boot-root", "/state/boot/root",
		"--session", "current",
		"--json",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv =\n  %#v\nwant\n  %#v\n(an empty CompositionInput must add nothing beyond the bare target)", argv, want)
	}
	for _, a := range argv {
		if a == "--skill" {
			t.Fatalf("argv %v contains --skill despite CompositionInput.Skills being empty -- exactly the fail-alone hazard", argv)
		}
		if a == "--prompt" {
			t.Fatalf("argv %v contains --prompt despite CompositionInput.Prompts being empty -- exactly the fail-alone hazard", argv)
		}
	}
}

// TestCompositionFromInput_SkillsPassThroughUnmodified proves
// compositionFromInput applies no union, dedup, sort or lookup to Skills:
// what the palette typed is exactly, and only, what reaches
// compose.Composition.Skills -- element for element, in the same order,
// with the same length. A second implementation that quietly merged in
// anything else (a profile's own resolved skills, for instance) would
// change this slice's contents without touching CompositionInput.Skills
// itself, which is exactly what this equality check would catch.
func TestCompositionFromInput_SkillsPassThroughUnmodified(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"solo"},
		{"zeta", "alpha", "middle"}, // deliberately unsorted -- compositionFromInput must not sort
	}
	for _, skills := range cases {
		input := CompositionInput{Target: "x", Skills: skills}
		comp := compositionFromInput(input, "/bundle", "/boot")
		if len(comp.Skills) != len(skills) {
			t.Fatalf("Skills %v became %v (different length)", skills, comp.Skills)
		}
		for i := range skills {
			if comp.Skills[i] != skills[i] {
				t.Fatalf("Skills %v became %v (differs at index %d)", skills, comp.Skills, i)
			}
		}
	}
}

// TestCompositionFromInput_PromptsPassThroughUnmodified is
// TestCompositionFromInput_SkillsPassThroughUnmodified's exact mirror for
// Prompts (CW-20260904-0006): what the palette typed is exactly, and only,
// what reaches compose.Composition.Prompts -- no union, dedup, sort or
// lookup applied anywhere in this function.
func TestCompositionFromInput_PromptsPassThroughUnmodified(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"solo"},
		{"zeta", "alpha", "middle"}, // deliberately unsorted -- compositionFromInput must not sort
	}
	for _, prompts := range cases {
		input := CompositionInput{Target: "x", Prompts: prompts}
		comp := compositionFromInput(input, "/bundle", "/boot")
		if len(comp.Prompts) != len(prompts) {
			t.Fatalf("Prompts %v became %v (different length)", prompts, comp.Prompts)
		}
		for i := range prompts {
			if comp.Prompts[i] != prompts[i] {
				t.Fatalf("Prompts %v became %v (differs at index %d)", prompts, comp.Prompts, i)
			}
		}
	}
}

// --- LaunchComposition orchestration (fakes only) ------------------------

// TestRunComposition_FullCompositionReachesTheFakeRunnerVerbatim drives
// [runComposition] itself -- the shared core LaunchComposition funnels
// into, same as [launch] does -- with a composition built the same way
// [Service.LaunchComposition] builds one, confirming the fake runner
// receives the exact composed argv and the fake spawn receives the harness
// argv and cwd cairn's --json report named, end to end through the shared
// core, without a real cairn binary.
func TestRunComposition_FullCompositionReachesTheFakeRunnerVerbatim(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec := &spawnRecorder{}

	input := CompositionInput{
		Target:  "eng-nanite",
		Skills:  []string{"qstatus"},
		Prompts: []string{"report"},
		Sets:    []SetInput{{Slot: "role", Value: "marker"}},
		Parts:   []string{"writer"},
	}
	comp := compositionFromInput(input, "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, comp.BootRoot, fr.run, rec.spawn); err != nil {
		t.Fatalf("runComposition: %v", err)
	}
	if !rec.called {
		t.Fatal("spawn was not called")
	}

	wantArgv := []string{
		"boot", "eng-nanite",
		"--profile", "/bundle/root",
		"--boot-root", comp.BootRoot,
		"--session", "current",
		"--with", "writer",
		"--skill", "qstatus",
		"--prompt", "report",
		"--set", "role=marker",
		"--json",
	}
	if !reflect.DeepEqual(fr.gotArgv, wantArgv) {
		t.Fatalf("cairn argv =\n  %#v\nwant\n  %#v", fr.gotArgv, wantArgv)
	}
}

// --- The fail-alone structural guard: Binding carries no skills field ---

// TestBindingCarriesNoSkillsFieldToSeedFrom is a permanent regression guard
// for the single acceptance criterion this whole task can be failed on
// alone: the palette's compose form must never pre-populate its skills
// control from a binding's or profile's already-resolved skills. This
// package cannot inspect Palette.jsx's own React state, but it CAN prove
// something stronger and machine-checkable -- that [binding.Binding], the
// only shape internal/binding.Service.List() ever hands the frontend
// (see that package's own doc), carries no field, under any name or JSON
// tag, that could even be read as "this binding's skills." If a future
// change ever added one -- to surface a profile's resolved skills for some
// other reason -- this test fails immediately, forcing a conscious
// decision about the palette's compose-form seed rather than a silent one.
//
// This does not replace reading Palette.jsx by hand to confirm its own
// initial state (useState([]), never populated from anything else); it
// proves the stronger, structural half: even a Palette.jsx that WANTED to
// seed from something has nothing to seed from.
func TestBindingCarriesNoSkillsFieldToSeedFrom(t *testing.T) {
	typ := reflect.TypeOf(binding.Binding{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if strings.Contains(strings.ToLower(f.Name), "skill") {
			t.Fatalf("binding.Binding.%s: field name contains %q -- a compose form could seed its skills control from this", f.Name, "skill")
		}
		tag := f.Tag.Get("json")
		if strings.Contains(strings.ToLower(tag), "skill") {
			t.Fatalf("binding.Binding.%s: json tag %q contains %q -- a compose form could seed its skills control from this", f.Name, tag, "skill")
		}
	}
}

// TestBindingCarriesNoPromptsFieldToSeedFrom is
// TestBindingCarriesNoSkillsFieldToSeedFrom's exact mirror for prompts
// (CW-20260904-0006, acceptance criterion "write a test proving there's no
// code path that could pre-populate it from an existing binding/profile's
// prompts"): [binding.Binding] carries no field, under any name or JSON
// tag, that could be read as "this binding's prompts" -- so even a
// Palette.jsx that wanted to pre-populate its prompts control from a
// binding has structurally nothing to seed it from.
func TestBindingCarriesNoPromptsFieldToSeedFrom(t *testing.T) {
	typ := reflect.TypeOf(binding.Binding{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if strings.Contains(strings.ToLower(f.Name), "prompt") {
			t.Fatalf("binding.Binding.%s: field name contains %q -- a compose form could seed its prompts control from this", f.Name, "prompt")
		}
		tag := f.Tag.Get("json")
		if strings.Contains(strings.ToLower(tag), "prompt") {
			t.Fatalf("binding.Binding.%s: json tag %q contains %q -- a compose form could seed its prompts control from this", f.Name, tag, "prompt")
		}
	}
}

// --- Real end-to-end: a full composition through the real cairn binary --
//
// This test writes nothing outside a t.TempDir() -- see the "never
// ~/dev/agent-os" hazard internal/compose's own package doc guards
// against (D9).

// TestLaunchComposition_RealCairnRendersPartSkillAndSetIntoTheBootDirectory
// is this task's own required proof, run as a real integration test rather
// than asserted only from --help text: a full composition -- a target
// binding, an ADDED skill, an ADDED prompt (CW-20260904-0006), an ADDED
// --with part, and a --set override -- through the real cairn binary
// against the real, read-only ~/dev/projects/agent-setup bundle, into a
// scratch boot root this test owns and t.TempDir() cleans up. It then reads
// the actual rendered files cairn wrote and confirms all four landed:
//
//   - the added skill's own content file exists under the composed boot
//     directory's .claude/skills/, with the same content the installed
//     skill source carries (proving cairn rendered it, not merely that a
//     directory of that name exists);
//   - the added prompt lands at .claude/commands/boot/<name>.md, its
//     marker-free prose intact from prompts/<name>.md in the bundle and its
//     cairn:value markers substituted with this composition's real
//     instance values -- prompts turn out to be templated at plant time,
//     not copied raw (see the assertion's own comment) -- this is
//     CW-20260904-0006's own gate-verification requirement ("Verify
//     --prompt against the installed binary"), proven for real against the
//     binary actually installed on this machine rather than only against
//     its --help text;
//   - the added --with part's own skill (writer's "blg", which eng-nanite's
//     own profile -- engineer -- does not declare on its own) exists too,
//     proving the part reached rendering and was not silently folded away;
//   - the --set override's value is present in the rendered AGENTS.md,
//     proving --set's substitution reached the document Cairn wrote, not
//     merely that cairn exited 0.
//
// This is the filesystem-inspection half of "Hotkey -> compose -> Enter ->
// an iTerm2 session opens with the composed selection" the task record
// asks for: iTerm2 actually opening was already proven by T12
// (CW-20260903-0016); what this proves is that the COMPOSITION -- the part
// of this whole chain T13 actually adds -- reaches the boot directory
// correctly. runComposition is driven directly, with a fake spawn (so this
// test never opens a real terminal) and the real [boot.ExecRunner], the
// same "fake the two edges, keep the real middle" shape
// internal/boot/reconcile_test.go and cmd/tachyon/integration_test.go both
// use.
//
// It skips, with a message saying why, only when a prerequisite is
// genuinely absent from the machine running it: cairn not on PATH, the
// ~/dev/projects/agent-setup bundle itself not present, or the "writer" /
// "qstatus" / "report" fixtures this test's assertions are keyed to not
// existing in it today (bundle content can move; a stale test target is a
// reason to update this test, not a false failure). A bundle that IS
// present but unreadable by this build of Tachyon is a different condition
// and FAILS instead -- see [testbundle.Resolve]'s own doc, the same
// distinction internal/boot/reconcile_test.go relies on.
//
// Nothing here writes into agent-setup -- --profile only reads it,
// confirmed by this test's own before/after `git status --short` check on
// that repo, exactly as this task's own standing rules require. Reading
// the planted prompt's bytes below, to compare them against the bundle's
// own source, is a TEST assertion confirming cairn's real behavior -- not
// Tachyon application code reading prompt content, which is exactly what
// CW-20260904-0006 forbids; see internal/compose's "no delivery" doc.
func TestLaunchComposition_RealCairnRendersPartSkillPromptAndSetIntoTheBootDirectory(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping the real-composition end-to-end check: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	bundleRoot := filepath.Join(home, "dev", "projects", "agent-setup")

	bindings, skip, err := testbundle.Resolve(bundleRoot)
	if skip {
		t.Skipf("no bundle at %s (agent-setup not present on this machine), skipping the real-composition end-to-end check", bundleRoot)
	}
	if err != nil {
		t.Fatalf("bundle at %s is present but its bindings cannot be read -- this is exactly the break CW-20260904-0003 (T24) exists to catch loudly, not silence: %v", bundleRoot, err)
	}

	const target = "eng-nanite"
	found := false
	for _, b := range bindings {
		if b.Name == target {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bundle at %s is readable but has no binding named %q -- update this test's target, or the bundle", bundleRoot, target)
	}

	// git status --short before: confirm this run starts from a clean
	// checkout of the read-only bundle, so a dirty result afterward can
	// only be blamed on what this test itself just did.
	beforeStatus := gitStatusShort(t, bundleRoot)

	const addedSkill = "qstatus" // not in engineer.md's own `skills:` (search-first, surface-discovery)
	const addedPrompt = "report" // prompts/report.md in the live bundle, as of 2026-09-03
	const addedPart = "writer"   // contributes its own skill, "blg", which engineer.md does not declare
	const partOnlySkill = "blg"  // writer.md's own skills: [blg]
	const setSlot = "role"       // a slot engineer.md's own spec.slots declares
	const setMarker = "TACHYON_T13_INTEGRATION_MARKER_9f3a1c"

	scratchRoot := t.TempDir()

	input := CompositionInput{
		Target:  target,
		Skills:  []string{addedSkill},
		Prompts: []string{addedPrompt},
		Parts:   []string{addedPart},
		Sets:    []SetInput{{Slot: setSlot, Value: setMarker}},
	}
	comp := compositionFromInput(input, bundleRoot, scratchRoot)

	rec := &spawnRecorder{}
	runner := boot.ExecRunner(cairnPath)
	if err := runComposition(context.Background(), comp, scratchRoot, runner, rec.spawn); err != nil {
		t.Fatalf("runComposition against real cairn: %v", err)
	}
	if !rec.called {
		t.Fatal("spawn was never called -- the real cairn invocation did not reach the end of runComposition")
	}
	bootDir := rec.cwd // cwd_preference for eng-nanite is "boot_dir" as of 2026-09-03; see resolveCwd
	if info, statErr := os.Stat(bootDir); statErr != nil || !info.IsDir() {
		t.Fatalf("reported boot dir %q does not exist or is not a directory: statErr=%v", bootDir, statErr)
	}

	// 1) The ADDED skill's content landed under the rendered boot
	// directory, byte-identical to the installed skill source -- proving
	// cairn actually rendered it, not merely that --skill was accepted.
	renderedSkillPath := filepath.Join(bootDir, ".claude", "skills", addedSkill, "SKILL.md")
	renderedSkill, err := os.ReadFile(renderedSkillPath)
	if err != nil {
		t.Fatalf("expected the added skill %q to render at %s: %v", addedSkill, renderedSkillPath, err)
	}
	installedSkillPath := filepath.Join(home, ".config", "agents", "skills", addedSkill, "SKILL.md")
	if installedSkill, err := os.ReadFile(installedSkillPath); err == nil {
		if string(renderedSkill) != string(installedSkill) {
			t.Errorf("rendered skill %s content does not match the installed source at %s", renderedSkillPath, installedSkillPath)
		}
	} else {
		t.Logf("installed skill source at %s not readable (%v); skipping the byte-identical comparison, keeping the existence+non-empty check below", installedSkillPath, err)
	}
	if len(renderedSkill) == 0 {
		t.Errorf("rendered skill file %s is empty", renderedSkillPath)
	}

	// 2) The ADDED prompt landed exactly where Cairn's design record says a
	// prompt is planted -- .claude/commands/boot/<name>.md. This is
	// CW-20260904-0006's own required proof that --prompt works against
	// the real, installed binary, not just its --help text (which the
	// gate-verification step of that task already read). Tachyon itself
	// never reads this file's content anywhere in its own code -- see
	// internal/compose's "no delivery" doc -- this read is the test
	// confirming cairn's real behavior, not application logic.
	//
	// NOT byte-identical to the source: this run's very first attempt
	// asserted exact equality and failed, which is itself the finding
	// worth recording -- prompts/report.md's own <!-- cairn:value ... -->
	// markers (binding/profile/scope/session) get substituted at plant
	// time exactly like a template's do, matching prompts/README.md's own
	// words ("A prompt is a template ... substituted from the same slots
	// and instance values") more literally than this test first assumed.
	// So the proof below is: the file exists, is non-empty, keeps the
	// source's marker-free prose verbatim, and the substituted lines carry
	// this composition's real instance values -- the same "substitution
	// reached the document" shape assertion 4 below already uses for
	// --set, not a raw-copy assumption. Tachyon does not perform this
	// substitution and does not need to -- it only ever handed cairn a
	// name.
	plantedPromptPath := filepath.Join(bootDir, ".claude", "commands", "boot", addedPrompt+".md")
	plantedPrompt, err := os.ReadFile(plantedPromptPath)
	if err != nil {
		t.Fatalf("expected the added prompt %q to be planted at %s: %v", addedPrompt, plantedPromptPath, err)
	}
	if len(plantedPrompt) == 0 {
		t.Errorf("planted prompt %s is empty", plantedPromptPath)
	}
	sourcePromptPath := filepath.Join(bundleRoot, "prompts", addedPrompt+".md")
	sourcePrompt, err := os.ReadFile(sourcePromptPath)
	if err != nil {
		t.Fatalf("reading the bundle's own source prompt %s: %v", sourcePromptPath, err)
	}
	for _, line := range strings.Split(string(sourcePrompt), "\n") {
		if strings.Contains(line, "cairn:value") {
			continue // this line is expected to change -- it is what substitution means
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(string(plantedPrompt), line) {
			t.Errorf("planted prompt %s is missing a marker-free line the source carries verbatim: %q", plantedPromptPath, line)
		}
	}
	if !strings.Contains(string(plantedPrompt), "binding: "+target) {
		t.Errorf("planted prompt %s does not contain the substituted binding value %q -- substitution did not reach the document", plantedPromptPath, target)
	}
	if !strings.Contains(string(plantedPrompt), "session: current") {
		t.Errorf("planted prompt %s does not contain the substituted session value %q -- substitution did not reach the document", plantedPromptPath, "current")
	}

	// 3) The ADDED --with part's own contribution (a skill engineer.md does
	// not itself declare) also landed -- proving the part reached
	// rendering, not just the target's own cascade.
	partSkillPath := filepath.Join(bootDir, ".claude", "skills", partOnlySkill, "SKILL.md")
	if _, err := os.Stat(partSkillPath); err != nil {
		t.Errorf("expected --with %s's own skill %q to render at %s (proving the part reached rendering): %v", addedPart, partOnlySkill, partSkillPath, err)
	}

	// 4) The --set override's value is present in the rendered AGENTS.md --
	// proving the substitution reached the document cairn wrote.
	agentsPath := filepath.Join(bootDir, "AGENTS.md")
	agentsContent, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("reading rendered %s: %v", agentsPath, err)
	}
	if !strings.Contains(string(agentsContent), setMarker) {
		t.Errorf("rendered %s does not contain the --set %s=%s marker %q -- substitution did not reach the document", agentsPath, setSlot, setMarker, setMarker)
	}

	// The target's OWN default skills (search-first, surface-discovery)
	// must still be present too -- confirming the composition ADDED
	// addedSkill on top of the resolved profile rather than replacing it.
	for _, own := range []string{"search-first", "surface-discovery"} {
		p := filepath.Join(bootDir, ".claude", "skills", own, "SKILL.md")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected engineer.md's own skill %q to still be present at %s (additive, not replacing): %v", own, p, err)
		}
	}

	afterStatus := gitStatusShort(t, bundleRoot)
	if beforeStatus != afterStatus {
		t.Fatalf("git status --short on %s changed during this test:\nbefore: %q\nafter:  %q\n(this test must only ever read that bundle)", bundleRoot, beforeStatus, afterStatus)
	}
}

// TestLaunchComposition_RealCairnBareProfileRendersTheWholeComposition is
// T33's end-to-end distinction from the binding-based test above. Target is
// the bare profile id "engineer" -- no binding selection or resolution --
// while an ordered part, an explicitly added skill, a prompt, a set override
// and a literal scope all travel through the real Cairn binary. The test also
// proves the live catalog has no binding named "engineer", so a successful
// run cannot accidentally be exercising the old selected-binding path.
// Reading the planted files back proves this is a rendered composition rather
// than only an argv-shape assertion. The real bundle remains read-only; Cairn
// writes exclusively beneath scratchRoot.
func TestLaunchComposition_RealCairnBareProfileRendersTheWholeComposition(t *testing.T) {
	cairnPath, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn not on PATH, skipping bare-profile integration check: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	bundleRoot := filepath.Join(home, "dev", "projects", "agent-setup")
	if info, statErr := os.Stat(bundleRoot); errors.Is(statErr, os.ErrNotExist) {
		t.Skipf("no bundle at %s, skipping bare-profile integration check", bundleRoot)
	} else if statErr != nil || !info.IsDir() {
		t.Fatalf("bundle root %s is present but unreadable or not a directory: %v", bundleRoot, statErr)
	}

	const (
		target        = "engineer"
		addedPart     = "writer"
		partOnlySkill = "blg"
		addedSkill    = "qstatus"
		prompt        = "report"
		setSlot       = "role"
		setMarker     = "TACHYON_T33_BARE_PROFILE_MARKER_4d8f2a"
	)
	for _, required := range []string{
		filepath.Join(bundleRoot, "profiles", target+".md"),
		filepath.Join(bundleRoot, "profiles", addedPart+".md"),
		filepath.Join(bundleRoot, "prompts", prompt+".md"),
		// Skills live in the bundle, which is what --profile names and what
		// Cairn resolves --skill against. This used to read
		// ~/.config/agents/skills, Cairn's default bundle location when no
		// --profile is given -- a path this launch never uses and which no
		// longer exists on this machine, so the fixture check failed on a
		// bundle that was in fact complete.
		filepath.Join(bundleRoot, "skills", addedSkill, "SKILL.md"),
		filepath.Join(bundleRoot, "skills", partOnlySkill, "SKILL.md"),
	} {
		if _, statErr := os.Stat(required); statErr != nil {
			t.Fatalf("required live-bundle fixture %s is unavailable: %v", required, statErr)
		}
	}
	if unexpected, getErr := binding.Open(bundleRoot).Get(target); getErr == nil {
		t.Fatalf("live bundle unexpectedly has a binding named %q (%+v); this test would no longer prove a bare-profile launch", target, unexpected)
	} else if !errors.Is(getErr, binding.ErrNotFound) {
		t.Fatalf("checking that %q is not a saved binding: %v", target, getErr)
	}

	beforeStatus := gitStatusShort(t, bundleRoot)
	scratchRoot := t.TempDir()
	literalScope := t.TempDir()
	input := CompositionInput{
		Target:  target,
		Parts:   []string{addedPart},
		Skills:  []string{addedSkill},
		Prompts: []string{prompt},
		Sets:    []SetInput{{Slot: setSlot, Value: setMarker}},
		Scope:   literalScope,
	}
	comp := compositionFromInput(input, bundleRoot, scratchRoot)

	rec := &spawnRecorder{}
	if err := runComposition(context.Background(), comp, scratchRoot, boot.ExecRunner(cairnPath), rec.spawn); err != nil {
		t.Fatalf("runComposition with bare profile %q: %v", target, err)
	}
	if !rec.called {
		t.Fatal("spawn was not reached after the real bare-profile Cairn invocation")
	}
	bootDir := rec.cwd
	if info, statErr := os.Stat(bootDir); statErr != nil || !info.IsDir() {
		t.Fatalf("reported boot dir %q does not exist or is not a directory: %v", bootDir, statErr)
	}

	// Direct skill addition: engineer does not declare qstatus itself.
	addedSkillPath := filepath.Join(bootDir, ".claude", "skills", addedSkill, "SKILL.md")
	addedSkillBytes, err := os.ReadFile(addedSkillPath)
	if err != nil {
		t.Fatalf("added skill %q was not rendered at %s: %v", addedSkill, addedSkillPath, err)
	}
	if len(addedSkillBytes) == 0 {
		t.Errorf("added skill file %s is empty", addedSkillPath)
	}

	// Part contribution: writer contributes blg, which engineer does not.
	partSkillPath := filepath.Join(bootDir, ".claude", "skills", partOnlySkill, "SKILL.md")
	if _, err := os.Stat(partSkillPath); err != nil {
		t.Errorf("--with %s did not render its own skill %q at %s: %v", addedPart, partOnlySkill, partSkillPath, err)
	}

	// Prompt delivery is checked here under the bare target. report is also a
	// base-profile prompt in today's bundle, while the pure mapping test above
	// is what proves T33's explicit prompt control emits --prompt.
	promptPath := filepath.Join(bootDir, ".claude", "commands", "boot", prompt+".md")
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("prompt %q was not planted at %s: %v", prompt, promptPath, err)
	}
	if !strings.Contains(string(promptBytes), "binding: "+target) {
		t.Errorf("planted prompt %s does not carry bare target %q", promptPath, target)
	}

	// Set and scope both reached Cairn's rendered instance values.
	agentsPath := filepath.Join(bootDir, "AGENTS.md")
	agentsBytes, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("reading rendered %s: %v", agentsPath, err)
	}
	if !strings.Contains(string(agentsBytes), setMarker) {
		t.Errorf("rendered %s does not contain --set marker %q", agentsPath, setMarker)
	}
	// macOS resolves /var through its /private/var symlink while Cairn
	// canonicalizes scope. The argv test above proves Tachyon forwards the
	// literal unchanged; this assertion follows Cairn's rendered form.
	renderedScope, err := filepath.EvalSymlinks(literalScope)
	if err != nil {
		t.Fatalf("resolving scratch scope %s: %v", literalScope, err)
	}
	if !strings.Contains(string(promptBytes), "scope: "+renderedScope) {
		t.Errorf("planted prompt %s does not contain rendered scope %q", promptPath, renderedScope)
	}

	afterStatus := gitStatusShort(t, bundleRoot)
	if beforeStatus != afterStatus {
		t.Fatalf("git status --short on %s changed during bare-profile test:\nbefore: %q\nafter:  %q", bundleRoot, beforeStatus, afterStatus)
	}
}

// gitStatusShort runs `git -C dir status --short` and returns its trimmed
// output, failing the test if git itself could not be run. Used only to
// assert the read-only bundle is unchanged by this test, before and after.
func gitStatusShort(t *testing.T, dir string) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH, cannot verify %s is unchanged: %v", dir, err)
	}
	cmd := exec.Command(gitPath, "-C", dir, "status", "--short")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s status --short: %v\n%s", dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// --- codex (CW-20260906-0001) ---------------------------------------------

// codexReport renders the Codex boot report for a given boot directory and
// scope, matching the real captured document's keys (see
// internal/boot/invoke_test.go's realCodexBootReportFixture). Templating the
// paths is what lets a test point the report at directories that actually
// exist on disk, which the home-resource steps need.
func codexReport(bootDir, scope string, homeResources string) string {
	return `{
  "boot_dir": ` + quoteJSON(bootDir) + `,
  "provider": "codex",
  "scope": ` + quoteJSON(scope) + `,
  "settings_path": ` + quoteJSON(filepath.Join(bootDir, "config.toml")) + `,
  "cwd_preference": "boot_dir",
  "project_dir_arg": ["--add-dir", "{{.ProjectDir}}"],
  "env_amendments": ["CODEX_HOME={{.BootDir}}"],
  "home_resource_paths": ` + homeResources + `
}`
}

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestLaunch_CodexSpawnsFromTheBootDirWithItsHomeAndScope is the whole Codex
// launch shape in one assertion set, and it is exactly the manual recipe
// Cairn's own examples/README.md documents: cwd is the boot directory (so
// Codex discovers the AGENTS.md, config.toml and .agents/skills planted
// there), CODEX_HOME points at that same directory, and the real project is
// granted with --add-dir. Nothing about it is a second code path -- the same
// runComposition that launches Claude produced it.
func TestLaunch_CodexSpawnsFromTheBootDirWithItsHomeAndScope(t *testing.T) {
	bootDir := "/state/boot/codex-coord-agent-setup/current"
	scope := "/Users/chrispian/dev/projects/agent-setup"
	fr := &fakeRunner{stdout: []byte(codexReport(bootDir, scope, "null"))}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "codex-coord-agent-setup", Profile: "orchestrator", Scope: scope}

	if err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !rec.called {
		t.Fatal("spawn was not called")
	}
	if rec.cwd != bootDir {
		t.Errorf("cwd = %q; want the boot directory %q", rec.cwd, bootDir)
	}
	wantArgv := []string{"codex", "--add-dir", scope}
	if !reflect.DeepEqual(rec.argv, wantArgv) {
		t.Errorf("argv = %v; want %v", rec.argv, wantArgv)
	}
	wantEnv := []string{"CODEX_HOME=" + bootDir}
	if !reflect.DeepEqual(rec.env, wantEnv) {
		t.Errorf("env = %v; want %v", rec.env, wantEnv)
	}
	// The exec-only probe flag must never reach an interactive launch.
	for _, tok := range rec.argv {
		if strings.Contains(tok, "--skip-git-repo-check") {
			t.Errorf("interactive codex argv carries the exec-only flag: %v", rec.argv)
		}
	}
	// Claude's flag is Claude's.
	for _, tok := range rec.argv {
		if tok == "--settings" {
			t.Errorf("codex argv carries claude's --settings flag: %v", rec.argv)
		}
	}
}

// TestLaunch_ClaudeGainedNothing is the regression guard this whole
// increment is fenced by: a Claude launch must reach spawn with exactly the
// argv and cwd it always did, and with no environment at all -- the
// difference between the two providers is entirely in what Cairn's report
// says, and Claude's says nothing new.
func TestLaunch_ClaudeGainedNothing(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "eng-nanite", Profile: "engineer", Scope: "/Users/chrispian/dev/hollis-labs/apps/nanite"}

	if err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn); err != nil {
		t.Fatalf("launch: %v", err)
	}
	wantArgv := []string{"claude", "--settings", "/state/boot/eng-nanite/current/.claude/settings.json"}
	if !reflect.DeepEqual(rec.argv, wantArgv) {
		t.Fatalf("argv = %v; want %v", rec.argv, wantArgv)
	}
	if rec.cwd != "/state/boot/eng-nanite/current" {
		t.Errorf("cwd = %q; want the boot directory", rec.cwd)
	}
	if rec.env != nil {
		t.Fatalf("env = %v; want nil -- a claude launch adds nothing to the terminal's environment", rec.env)
	}
}

// TestRunComposition_ProviderReachesTheCairnArgv: the compose form's
// provider control has to arrive at cairn as --provider, or the boot
// directory is rendered for the wrong harness and everything downstream is
// consistent with the wrong answer.
func TestRunComposition_ProviderReachesTheCairnArgv(t *testing.T) {
	bootDir := "/state/boot/orchestrator/current"
	scope := "/Users/chrispian/dev/projects/agent-setup"
	fr := &fakeRunner{stdout: []byte(codexReport(bootDir, scope, "null"))}
	rec := &spawnRecorder{}

	comp := compositionFromInput(CompositionInput{
		Target:   "orchestrator",
		Provider: "codex",
		Parts:    []string{"codex-cli"},
	}, "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, comp.BootRoot, fr.run, rec.spawn); err != nil {
		t.Fatalf("runComposition: %v", err)
	}
	if !argvHasPair(fr.gotArgv, "--provider", "codex") {
		t.Fatalf("cairn argv %v does not carry --provider codex", fr.gotArgv)
	}
}

// TestRunComposition_NoProviderSelectedSendsNoFlag: the ordinary case. An
// empty control is not "claude", it is "whatever the resolved profile
// cascade declares" -- which is how the palette's direct Enter-on-a-binding
// path lands on Codex for a binding whose content says so.
func TestRunComposition_NoProviderSelectedSendsNoFlag(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec := &spawnRecorder{}
	comp := compositionFromInput(CompositionInput{Target: "eng-nanite"}, "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, comp.BootRoot, fr.run, rec.spawn); err != nil {
		t.Fatalf("runComposition: %v", err)
	}
	for _, a := range fr.gotArgv {
		if a == "--provider" {
			t.Fatalf("cairn argv %v carries --provider for an unset control", fr.gotArgv)
		}
	}
}

// TestLaunch_CodexLinksHomeResourcesBeforeSpawning covers the step Cairn
// deliberately leaves to a launcher: pointing CODEX_HOME at a disposable
// boot directory is only usable if the operator's own auth and hooks are
// reachable from it. They must be there before the terminal opens, and they
// must be links rather than copies.
func TestLaunch_CodexLinksHomeResourcesBeforeSpawning(t *testing.T) {
	root := t.TempDir()
	sourceHome := filepath.Join(root, "codex-home")
	bootDir := filepath.Join(root, "boot")
	if err := os.MkdirAll(filepath.Join(sourceHome, "hooks"), 0o755); err != nil {
		t.Fatalf("creating the source home: %v", err)
	}
	for _, name := range []string{"auth.json", "hooks.json"} {
		if err := os.WriteFile(filepath.Join(sourceHome, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(bootDir, 0o755); err != nil {
		t.Fatalf("creating the boot directory: %v", err)
	}
	t.Setenv("CODEX_HOME", sourceHome)

	scope := t.TempDir()
	fr := &fakeRunner{stdout: []byte(codexReport(bootDir, scope, `["auth.json", "hooks.json", "hooks"]`))}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "codex-coord-agent-setup", Profile: "orchestrator", Scope: scope}

	if err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !rec.called {
		t.Fatal("spawn was not called")
	}
	for _, name := range []string{"auth.json", "hooks.json", "hooks"} {
		info, err := os.Lstat(filepath.Join(bootDir, name))
		if err != nil {
			t.Fatalf("%s was not provided in the boot directory: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a link; live operator state must never be copied into a boot directory", name)
		}
	}
	// And the environment that reaches the terminal points Codex at that
	// directory, which is what makes the links matter at all.
	if !reflect.DeepEqual(rec.env, []string{"CODEX_HOME=" + bootDir}) {
		t.Fatalf("env = %v; want CODEX_HOME pointing at the boot directory", rec.env)
	}
}

// TestLaunch_MissingHomeResourceRefusesAndSpawnsNothing is the decision this
// increment made explicitly: rather than open a session whose hooks silently
// do not run, the launch fails and says which path is missing. Nothing is
// spawned, so there is no half-working terminal to notice later.
func TestLaunch_MissingHomeResourceRefusesAndSpawnsNothing(t *testing.T) {
	root := t.TempDir()
	sourceHome := filepath.Join(root, "codex-home")
	bootDir := filepath.Join(root, "boot")
	if err := os.MkdirAll(sourceHome, 0o755); err != nil {
		t.Fatalf("creating the source home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceHome, "auth.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("creating auth.json: %v", err)
	}
	if err := os.MkdirAll(bootDir, 0o755); err != nil {
		t.Fatalf("creating the boot directory: %v", err)
	}
	t.Setenv("CODEX_HOME", sourceHome)

	scope := t.TempDir()
	fr := &fakeRunner{stdout: []byte(codexReport(bootDir, scope, `["auth.json", "hooks.json"]`))}
	rec := &spawnRecorder{}
	b := binding.Binding{Name: "codex-coord-agent-setup", Profile: "orchestrator", Scope: scope}

	err := launch(context.Background(), b, "/bundle/root", t.TempDir(), fr.run, rec.spawn)
	if err == nil {
		t.Fatal("launch opened a terminal without a resource cairn said the provider needs")
	}
	if !errors.Is(err, boot.ErrHomeResource) {
		t.Errorf("error does not wrap boot.ErrHomeResource: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(sourceHome, "hooks.json")) {
		t.Errorf("refusal does not name the missing path: %v", err)
	}
	if rec.called {
		t.Fatal("spawn was called despite an unmet home resource")
	}
}

// TestCompositionFromInput_ProviderPassesThroughUnmodified: the provider is
// carried, never derived. Nothing in the mapping reads Target.
func TestCompositionFromInput_ProviderPassesThroughUnmodified(t *testing.T) {
	for _, provider := range []string{"", "codex", "claude", "opencode", "cluade"} {
		comp := compositionFromInput(
			CompositionInput{Target: "codex-coord-agent-setup", Provider: provider},
			"/bundle/root", "/boot/root",
		)
		if comp.Provider != provider {
			t.Errorf("compositionFromInput(provider %q).Provider = %q", provider, comp.Provider)
		}
	}
}

// TestBindingCarriesNoProviderFieldToSeedFrom is the structural half of "a
// provider is never inferred," matching the skills and prompts guards
// above: there is nothing in what internal/binding hands the frontend for a
// compose form's provider control to seed itself from, so the control
// starts empty because it has to, not because the JSX remembers to.
func TestBindingCarriesNoProviderFieldToSeedFrom(t *testing.T) {
	typ := reflect.TypeOf(binding.Binding{})
	for i := 0; i < typ.NumField(); i++ {
		if strings.Contains(strings.ToLower(typ.Field(i).Name), "provider") {
			t.Fatalf("binding.Binding has a provider-ish field %q for a compose form to seed from", typ.Field(i).Name)
		}
	}
}

// argvHasPair reports whether argv carries flag immediately followed by
// value -- checked as two adjacent elements, never as a substring of a
// joined string.
func argvHasPair(argv []string, flag, value string) bool {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) && argv[i+1] == value {
			return true
		}
	}
	return false
}
