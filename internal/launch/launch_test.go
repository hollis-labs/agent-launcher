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

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
)

// This is a white-box test (package launch, not launch_test) because it
// drives the unexported runComposition() and compositionFromInput()
// directly -- the orchestration seams a fake boot.Runner and a fake spawn
// func plug into: fakes for the runner and the spawn step, without ever
// running cairn or osascript for real.

// bareComposition is the minimal composition the orchestration core needs,
// standing in for what Service.LaunchComposition builds from a palette
// selection. It is what the binding-shaped tests below used to get from
// launch(binding, ...), which retired with bindings.
func bareComposition(target, bundleRoot, bootRoot string) compose.Composition {
	return compose.Composition{
		Target:   target,
		Bundle:   bundleRoot,
		BootRoot: bootRoot,
		Session:  boot.DefaultSession,
	}
}

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
// map has no entry for. "antigravity" is deliberately real: a harness the
// runtimes know and Cairn does not yet render, so this is the shape a
// launcher could actually meet rather than an invented word.
const unknownProviderFixture = `{
  "boot_dir": "/state/boot/antigravity-thing/current",
  "provider": "antigravity",
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
	comp := bareComposition("eng-nanite", "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
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
	comp := bareComposition("eng-setup", "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
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
	comp := bareComposition("broken", "/bundle/root", t.TempDir())

	err := runComposition(context.Background(), comp, fr.run, rec.spawn)
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
	comp := bareComposition("x", "/bundle/root", t.TempDir())

	err := runComposition(context.Background(), comp, fr.run, rec.spawn)
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
	comp := bareComposition("antigravity-thing", "/bundle/root", t.TempDir())

	err := runComposition(context.Background(), comp, fr.run, rec.spawn)
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
	comp := bareComposition("eng-nanite", "/bundle/root", t.TempDir())

	err := runComposition(context.Background(), comp, fr.run, rec.spawn)
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
	// Empty bootRoot: boot.Prepare refuses it ("root is required") before
	// compose.Build ever runs (compose.Build would separately refuse via
	// compose.ErrNoBootRoot if it were reached) -- either way this must
	// never reach the runner.
	comp := bareComposition("engineer", "/bundle/root", "")

	err := runComposition(context.Background(), comp, fr.run, rec.spawn)
	if err == nil {
		t.Fatal("runComposition returned no error for a missing boot root")
	}
	if fr.gotArgv != nil {
		t.Fatalf("runner was invoked despite compose.Build failing: argv=%v", fr.gotArgv)
	}
	if rec.called {
		t.Fatal("spawn was called despite compose.Build failing")
	}
}

func TestRunComposition_RelaunchingMovesPreviousAsideInsteadOfFailing(t *testing.T) {
	bootRoot := t.TempDir()
	comp := bareComposition("engineer", "/bundle/root", bootRoot)

	fr1 := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec1 := &spawnRecorder{}
	if err := runComposition(context.Background(), comp, fr1.run, rec1.spawn); err != nil {
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
	// bootRoot/<key>/<session> on every call, unconditionally, before cairn
	// (real or fake) ever runs.
	current := filepath.Join(bootRoot, boot.Key(comp.Target), comp.Session)
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("seeding an existing boot dir: %v", err)
	}

	fr2 := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec2 := &spawnRecorder{}
	if err := runComposition(context.Background(), comp, fr2.run, rec2.spawn); err != nil {
		t.Fatalf("second launch (relaunch): %v", err)
	}
	if !rec2.called {
		t.Fatal("second launch: spawn was not called")
	}

	keyDir := filepath.Join(bootRoot, boot.Key(comp.Target))
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

// --- Service.resolveLaunchProfile ------------------------------------------

func newRootStore(t *testing.T, bundleRoot string) bundle.RootStore {
	t.Helper()
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save(bundleRoot); err != nil {
		t.Fatalf("RootStore.Save: %v", err)
	}
	return store
}

// writeLaunchProfile puts one launch profile in a scratch store.
func writeLaunchProfile(t *testing.T, dir, name, provider string) string {
	t.Helper()
	p, err := launchprofile.Open(dir).Create(name, []byte("---\nid: "+name+"\nprovider: "+provider+"\n---\n"))
	if err != nil {
		t.Fatalf("writing launch profile %q: %v", name, err)
	}
	return p.Path
}

func TestResolveLaunchProfile_ReturnsThePathCairnIsGiven(t *testing.T) {
	launchDir := t.TempDir()
	want := writeLaunchProfile(t, launchDir, "codex", "codex")

	svc := NewServiceWithLaunchDir(newRootStore(t, t.TempDir()), launchDir)

	got, err := svc.resolveLaunchProfile("codex")
	if err != nil {
		t.Fatalf("resolveLaunchProfile: %v", err)
	}
	if got != want {
		t.Fatalf("resolveLaunchProfile = %q; want the file's own path %q", got, want)
	}
}

// TestResolveLaunchProfile_EmptyNameResolvesToNothing is the no-launch-profile
// case reaching cairn as no --with at all, which cairn then refuses for want
// of a provider. That refusal is the intended shape; substituting a default
// here would be the launcher inferring a provider.
func TestResolveLaunchProfile_EmptyNameResolvesToNothing(t *testing.T) {
	svc := NewServiceWithLaunchDir(newRootStore(t, t.TempDir()), t.TempDir())
	got, err := svc.resolveLaunchProfile("")
	if err != nil {
		t.Fatalf("resolveLaunchProfile(\"\"): %v", err)
	}
	if got != "" {
		t.Fatalf("resolveLaunchProfile(\"\") = %q; want \"\"", got)
	}
}

// TestResolveLaunchProfile_MissingIsAnErrorNotAFallback: falling back to a
// default would launch a session under a posture the person did not pick,
// and the two outcomes are indistinguishable afterwards.
func TestResolveLaunchProfile_MissingIsAnErrorNotAFallback(t *testing.T) {
	launchDir := t.TempDir()
	writeLaunchProfile(t, launchDir, "default", "claude")

	svc := NewServiceWithLaunchDir(newRootStore(t, t.TempDir()), launchDir)

	got, err := svc.resolveLaunchProfile("does-not-exist")
	if err == nil {
		t.Fatalf("resolveLaunchProfile returned %q and no error for a missing profile", got)
	}
	if !errors.Is(err, launchprofile.ErrNotFound) {
		t.Errorf("error does not wrap launchprofile.ErrNotFound: %v", err)
	}
}

// TestResolveLaunchProfile_NameCannotEscapeTheStore is why the frontend
// sends a NAME and this resolves it, rather than the frontend sending a
// path: --with takes a file, and a path crossing the Wails boundary
// unchecked is a path to any file on the machine.
func TestResolveLaunchProfile_NameCannotEscapeTheStore(t *testing.T) {
	svc := NewServiceWithLaunchDir(newRootStore(t, t.TempDir()), t.TempDir())

	for _, bad := range []string{"../../../etc/passwd", "sub/dir", "..", "/absolute/path.md"} {
		if got, err := svc.resolveLaunchProfile(bad); err == nil {
			t.Errorf("resolveLaunchProfile(%q) = %q with no error", bad, got)
		}
	}
}

// TestLaunchProfileStoreIsNotRootedInTheBundle is the seam the whole design
// turns on: changing the active bundle changes what agents are available,
// never how they run.
func TestLaunchProfileStoreIsNotRootedInTheBundle(t *testing.T) {
	launchDir := t.TempDir()
	want := writeLaunchProfile(t, launchDir, "codex", "codex")

	bundleA, bundleB := t.TempDir(), t.TempDir()
	store := newRootStore(t, bundleA)
	svc := NewServiceWithLaunchDir(store, launchDir)

	first, err := svc.resolveLaunchProfile("codex")
	if err != nil {
		t.Fatalf("resolveLaunchProfile against bundle A: %v", err)
	}
	if err := store.Save(bundleB); err != nil {
		t.Fatalf("switching the active bundle: %v", err)
	}
	second, err := svc.resolveLaunchProfile("codex")
	if err != nil {
		t.Fatalf("resolveLaunchProfile against bundle B: %v", err)
	}
	if first != want || second != want {
		t.Fatalf("the launch profile moved with the bundle root: %q then %q, want %q both times", first, second, want)
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
	const launchPath = "/config/tachyon/launch/default.md"
	input := CompositionInput{
		Target:        "engineer",
		LaunchProfile: "default",
		Skills:        []string{"skill-one", "skill-two"},
		Prompts:       []string{"report", "onboarding"},
		Scope:         "/scope/path",
		Sets: []SetInput{
			{Slot: "slot-one", Value: "value-one"},
		},
		Parts: []string{"extra-part"},
	}

	comp := compositionFromInput(input, "/bundle/root", "/state/boot/root", launchPath)

	argv, err := compose.Build(comp)
	if err != nil {
		t.Fatalf("compose.Build: unexpected error: %v", err)
	}

	want := []string{
		"boot", "engineer",
		"--profile", "/bundle/root",
		// The boot root carries the project segment: the layout is
		// <boot-root>/<project>/<profile>/<launch profile>, and cairn plants
		// only the last two.
		"--boot-root", boot.ProjectRoot("/state/boot/root", "/scope/path"),
		"--session", boot.SessionKey("default"),
		// The launch profile first, then the one-off part: cairn folds
		// closest-wins in order, so a --with added for this launch must be
		// able to override what the stored profile declared.
		"--with", launchPath,
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

// TestCompositionFromInput_BareProfileIsACompleteTarget: a profile id is
// the whole target, and a composition with no launch profile still maps
// every one-time addition through. cairn will refuse this particular one
// for want of a provider, which is the intended shape and not this
// function's business — its job is the mapping.
func TestCompositionFromInput_BareProfileIsACompleteTarget(t *testing.T) {
	const launchPath = ""
	input := CompositionInput{
		Target:  "engineer",
		Parts:   []string{"writer", "reviewer"},
		Skills:  []string{"qstatus"},
		Prompts: []string{"report"},
		Sets:    []SetInput{{Slot: "role", Value: "marker"}},
		Scope:   "/literal/project/path",
	}

	comp := compositionFromInput(input, "/bundle/root", "/state/boot/root", launchPath)
	argv, err := compose.Build(comp)
	if err != nil {
		t.Fatalf("compose.Build: %v", err)
	}

	want := []string{
		"boot", "engineer",
		"--profile", "/bundle/root",
		"--boot-root", boot.ProjectRoot("/state/boot/root", "/literal/project/path"),
		"--session", boot.SessionKey(""),
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
// the orchestration core has always produced for a bare target. If this
// ever failed, it would mean compositionFromInput started synthesizing
// something Skills/Scope/Sets/Parts being empty should never produce --
// exactly the class of bug the fail-alone acceptance criterion exists to
// catch, caught here at the pure-mapping layer rather than only by
// eyeballing the frontend.
func TestCompositionFromInput_EmptyInputProducesTheSameMinimalArgvAsBareLaunch(t *testing.T) {
	input := CompositionInput{Target: "engineer"}
	comp := compositionFromInput(input, "/bundle/root", "/state/boot/root", "")

	argv, err := compose.Build(comp)
	if err != nil {
		t.Fatalf("compose.Build: unexpected error: %v", err)
	}

	want := []string{
		"boot", "engineer",
		"--profile", "/bundle/root",
		// No scope selected: the project segment is still spelled, so an
		// unscoped launch cannot land where a project named after a profile
		// could later collide with it.
		"--boot-root", boot.ProjectRoot("/state/boot/root", ""),
		"--session", boot.DefaultSession,
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
		comp := compositionFromInput(input, "/bundle", "/boot", "")
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
		comp := compositionFromInput(input, "/bundle", "/boot", "")
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
	comp := compositionFromInput(input, "/bundle/root", t.TempDir(), "")

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
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

// --- The fail-alone structural guard: nothing to seed a form from -------

// TestLaunchProfileCarriesNoSkillsFieldToSeedFrom is a permanent regression
// guard for the single acceptance criterion this whole design can be failed
// on alone: the palette's compose form must never pre-populate its skills
// control from a profile's already-resolved skills. cairn's --skill flag is
// additive only, so a control that looked pre-checked would let a person
// UNCHECK a skill and silently get it anyway -- a wrong result that looks
// right.
//
// This package cannot inspect Palette.jsx's own React state, but it CAN
// prove something stronger and machine-checkable: that
// [launchprofile.Profile] -- the only shape launchprofile.Service.List()
// ever hands the frontend -- carries no field, under any name or JSON tag,
// that could be read as "this profile's skills." Even a Palette.jsx that
// WANTED to seed from something has nothing to seed from.
//
// The subject changed with the design (it was binding.Binding) and the
// property did not. Note that a launch profile's FILE may well declare
// spec.skills -- the composer writes them. What must never happen is those
// reaching the frontend as a list a control can mirror.
func TestLaunchProfileCarriesNoSkillsFieldToSeedFrom(t *testing.T) {
	assertNoFieldMentioning(t, reflect.TypeOf(launchprofile.Profile{}), "skill")
}

// TestLaunchProfileCarriesNoPromptsFieldToSeedFrom is the exact mirror for
// prompts (CW-20260904-0006), for the identical reason: cairn's --prompt
// flag documents itself as "Additive only, for the reason --skill is".
func TestLaunchProfileCarriesNoPromptsFieldToSeedFrom(t *testing.T) {
	assertNoFieldMentioning(t, reflect.TypeOf(launchprofile.Profile{}), "prompt")
}

// assertNoFieldMentioning fails if any field name or json tag on typ
// contains word.
func assertNoFieldMentioning(t *testing.T, typ reflect.Type, word string) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if strings.Contains(strings.ToLower(f.Name), word) {
			t.Fatalf("%s.%s: field name contains %q -- a compose form could seed its %ss control from this", typ.Name(), f.Name, word, word)
		}
		if tag := f.Tag.Get("json"); strings.Contains(strings.ToLower(tag), word) {
			t.Fatalf("%s.%s: json tag %q contains %q -- a compose form could seed its %ss control from this", typ.Name(), f.Name, tag, word, word)
		}
	}
}

// --- Real end-to-end: a full composition through the real cairn binary --

// TestLaunchComposition_RealCairnRendersTheWholeComposition drives the whole
// launch through the real cairn binary: a bare agent profile as the target, a
// launch profile supplying the provider, an ordered part, an explicitly added
// skill, a prompt, a set override and a literal scope. Reading the planted
// files back proves this is a rendered composition rather than only an
// argv-shape assertion.
//
// It absorbed a second test that ran the same path against a saved binding.
// There is no such path any more, and the two had converged on the same
// assertions anyway.
//
// The launch profile is the load-bearing addition. No profile in agent-setup
// declares a provider, so without one cairn refuses the render outright --
// which makes this test the end-to-end proof that the seam works at all, not
// just that a composition maps to flags.
//
// It skips, with a message saying why, only when a prerequisite is genuinely
// absent: cairn not on PATH, or the bundle not present. A bundle that IS
// present but missing a fixture this test is keyed to FAILS instead -- that
// distinction is what caught agent-setup retiring bindings/ out from under
// Tachyon.
//
// Nothing here writes into agent-setup -- --profile only reads it, confirmed
// by this test's own before/after `git status --short` check. Reading the
// planted prompt's bytes is a TEST assertion confirming cairn's behavior, not
// Tachyon application code reading prompt content, which internal/compose's
// "no delivery" doc forbids.
func TestLaunchComposition_RealCairnRendersTheWholeComposition(t *testing.T) {
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
		setSlot       = "charter"
		setMarker     = "TACHYON_E2E_MARKER_4d8f2a"
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

	beforeStatus := gitStatusShort(t, bundleRoot)
	scratchRoot := t.TempDir()
	literalScope := t.TempDir()

	// The launch profile: the only thing in this whole composition that
	// declares a provider, and therefore the only reason cairn renders at all.
	launchDir := t.TempDir()
	launchPath := writeLaunchProfile(t, launchDir, "e2e", "claude")

	input := CompositionInput{
		Target:        target,
		LaunchProfile: "e2e",
		Parts:         []string{addedPart},
		Skills:        []string{addedSkill},
		Prompts:       []string{prompt},
		Sets:          []SetInput{{Slot: setSlot, Value: setMarker}},
		Scope:         literalScope,
	}
	comp := compositionFromInput(input, bundleRoot, scratchRoot, launchPath)

	rec := &spawnRecorder{}
	if err := runComposition(context.Background(), comp, boot.ExecRunner(cairnPath), rec.spawn); err != nil {
		t.Fatalf("runComposition with profile %q: %v", target, err)
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
	if len(promptBytes) == 0 {
		t.Errorf("planted prompt %s is empty", promptPath)
	}

	// The rendered instruction document exists and is not empty. Cairn
	// refuses a profile with a body and no instruction artifact, so an
	// AGENTS.md that failed to render is a failure this catches here.
	agentsPath := filepath.Join(bootDir, "AGENTS.md")
	agentsBytes, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("reading rendered %s: %v", agentsPath, err)
	}
	if len(agentsBytes) == 0 {
		t.Errorf("rendered %s is empty", agentsPath)
	}

	// --set's marker is deliberately NOT asserted in the document, and the
	// reason is a finding rather than a simplification.
	//
	// This assertion used to pass with setSlot = "role". agent-setup retired
	// spec.slots entirely on 2026-09-10 when profiles became templates in
	// their own right -- measured against the live catalog, NO profile
	// declares a slot any more. So --set names a slot that does not exist,
	// substitutes into nothing, and cairn exits 0 without a word: a control
	// in the palette that silently does nothing against this bundle.
	//
	// The flag is still cairn's and still valid, so it stays wired; a bundle
	// that declares a slot again would use it. What is gone is anything for
	// this test to observe. compose's own tests still pin that --set reaches
	// the argv, which is the half Tachyon owns. Tracked as Torque
	// CW-20260910-0080.
	_ = setSlot
	_ = setMarker
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

	// The target's OWN skills must still be present: the composition ADDED
	// on top of the resolved profile rather than replacing it.
	for _, own := range []string{"search-first", "surface-discovery"} {
		own := filepath.Join(bootDir, ".claude", "skills", own, "SKILL.md")
		if _, err := os.Stat(own); err != nil {
			t.Errorf("expected engineer.md's own skill at %s to still be present (additive, not replacing): %v", own, err)
		}
	}

	afterStatus := gitStatusShort(t, bundleRoot)
	if beforeStatus != afterStatus {
		t.Fatalf("git status --short on %s changed during this test:\nbefore: %q\nafter:  %q", bundleRoot, beforeStatus, afterStatus)
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
	comp := bareComposition("codex-coord-agent-setup", "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
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

// openCodeReport is the shape a real `cairn boot <target> --provider opencode
// --scope <dir> --json` printed on 2026-09-30 (cairn CW-20260930-0142), with
// its paths replaced.
func openCodeReport(bootDir, scope string) string {
	return `{
  "boot_dir": ` + quoteJSON(bootDir) + `,
  "provider": "opencode",
  "scope": ` + quoteJSON(scope) + `,
  "settings_path": null,
  "cwd_preference": "project_dir",
  "project_dir_arg": ["{{.ProjectDir}}"],
  "env_amendments": ["OPENCODE_CONFIG_DIR={{.BootDir}}"],
  "home_resource_paths": null
}`
}

// TestLaunch_OpenCodeSpawnsInTheScopeWithItsConfigDir is the OpenCode launch
// shape: cwd is the scope (cwd_preference project_dir), OPENCODE_CONFIG_DIR
// points at the boot directory Cairn planted, and the scope is passed as the
// interactive positional project. Like Codex, it is the same runComposition,
// with every difference read from the report.
func TestLaunch_OpenCodeSpawnsInTheScopeWithItsConfigDir(t *testing.T) {
	bootDir := "/state/boot/opencode-coder/current"
	scope := "/Users/chrispian/dev/projects/agent-setup"
	fr := &fakeRunner{stdout: []byte(openCodeReport(bootDir, scope))}
	rec := &spawnRecorder{}
	comp := bareComposition("opencode-coder", "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !rec.called {
		t.Fatal("spawn was not called")
	}
	if rec.cwd != scope {
		t.Errorf("cwd = %q; want the scope %q", rec.cwd, scope)
	}
	if want := []string{"opencode", scope}; !reflect.DeepEqual(rec.argv, want) {
		t.Errorf("argv = %v; want %v", rec.argv, want)
	}
	if want := []string{"OPENCODE_CONFIG_DIR=" + bootDir}; !reflect.DeepEqual(rec.env, want) {
		t.Errorf("env = %v; want %v", rec.env, want)
	}
	for _, tok := range rec.argv {
		if tok == "--dir" || tok == "--settings" {
			t.Errorf("interactive opencode argv carries %s: %v", tok, rec.argv)
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
	comp := bareComposition("eng-nanite", "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
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
func TestRunComposition_LaunchProfileReachesTheCairnArgvAsTheFirstWith(t *testing.T) {
	bootDir := "/state/boot/orchestrator/current"
	scope := "/Users/chrispian/dev/projects/agent-setup"
	fr := &fakeRunner{stdout: []byte(codexReport(bootDir, scope, "null"))}
	rec := &spawnRecorder{}

	const launchPath = "/config/tachyon/launch/codex.md"
	comp := compositionFromInput(CompositionInput{
		Target:        "orchestrator",
		LaunchProfile: "codex",
		Parts:         []string{"nanite-domain"},
	}, "/bundle/root", t.TempDir(), launchPath)

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
		t.Fatalf("runComposition: %v", err)
	}
	if !argvHasPair(fr.gotArgv, "--with", launchPath) {
		t.Fatalf("cairn argv %v does not carry the launch profile as --with", fr.gotArgv)
	}
	// Order is the contract, not an accident: the launch profile must come
	// first so a one-off part can override what it declared.
	first, second := -1, -1
	for i, a := range fr.gotArgv {
		if a != "--with" {
			continue
		}
		if fr.gotArgv[i+1] == launchPath {
			first = i
		}
		if fr.gotArgv[i+1] == "nanite-domain" {
			second = i
		}
	}
	if first < 0 || second < 0 || first > second {
		t.Fatalf("cairn argv %v does not put the launch profile before the one-off part", fr.gotArgv)
	}
}

// TestRunComposition_NoProviderFlagIsEverSent: --provider is gone. The
// provider is declared in the launch profile and folded in by cairn's own
// cascade, so a flag here would be a second source for one value.
func TestRunComposition_NoProviderFlagIsEverSent(t *testing.T) {
	fr := &fakeRunner{stdout: []byte(bootDirFixture)}
	rec := &spawnRecorder{}
	comp := compositionFromInput(
		CompositionInput{Target: "engineer", LaunchProfile: "default"},
		"/bundle/root", t.TempDir(), "/config/tachyon/launch/default.md",
	)

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
		t.Fatalf("runComposition: %v", err)
	}
	for _, a := range fr.gotArgv {
		if a == "--provider" {
			t.Fatalf("cairn argv %v carries --provider", fr.gotArgv)
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
	comp := bareComposition("codex-coord-agent-setup", "/bundle/root", t.TempDir())

	if err := runComposition(context.Background(), comp, fr.run, rec.spawn); err != nil {
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
	comp := bareComposition("codex-coord-agent-setup", "/bundle/root", t.TempDir())

	err := runComposition(context.Background(), comp, fr.run, rec.spawn)
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

// TestCompositionFromInput_LaunchProfilePathPassesThroughUnmodified: the
// path is carried, never derived. Nothing in the mapping reads Target — a
// profile named "codex-something" says nothing about a harness, and a
// launcher that read one out of a name would render the wrong layout the
// first time somebody named a profile after a project rather than a tool.
func TestCompositionFromInput_LaunchProfilePathPassesThroughUnmodified(t *testing.T) {
	for _, path := range []string{"", "/config/tachyon/launch/codex.md", "/elsewhere/x.md"} {
		comp := compositionFromInput(
			CompositionInput{Target: "codex-coord-agent-setup"},
			"/bundle/root", "/boot/root", path,
		)
		if path == "" {
			if len(comp.Parts) != 0 {
				t.Errorf("an empty launch path produced parts %v", comp.Parts)
			}
			continue
		}
		if len(comp.Parts) != 1 || comp.Parts[0] != path {
			t.Errorf("compositionFromInput(path %q).Parts = %v", path, comp.Parts)
		}
	}
}

// TestLaunchProfileCarriesNoProviderControlToSeedFrom is the structural
// half of "a provider is never inferred," matching the skills and prompts
// guards above.
//
// Its subject changed with the design and the property did not. It used to
// say binding.Binding had no provider field for a compose form's provider
// CONTROL to seed itself from. There is no provider control now — the
// provider is declared in a launch profile — so what must stay true is that
// compose.Composition has nowhere to put one at all: no field, no flag, no
// second source that could disagree with the file.
func TestLaunchProfileCarriesNoProviderControlToSeedFrom(t *testing.T) {
	typ := reflect.TypeOf(compose.Composition{})
	for i := 0; i < typ.NumField(); i++ {
		if strings.Contains(strings.ToLower(typ.Field(i).Name), "provider") {
			t.Fatalf("compose.Composition has a provider-ish field %q; the provider belongs to the launch profile alone", typ.Field(i).Name)
		}
	}
	typ = reflect.TypeOf(CompositionInput{})
	for i := 0; i < typ.NumField(); i++ {
		if strings.Contains(strings.ToLower(typ.Field(i).Name), "provider") {
			t.Fatalf("CompositionInput has a provider-ish field %q", typ.Field(i).Name)
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

// --- Targets ---------------------------------------------------------------

func writeProfile(t *testing.T, root, rel, frontmatter string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\n"+frontmatter+"---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// TestTargetsExcludesAbstractProfilesOnly pins both halves of the rule, and
// the second is the one worth a test: cairn refuses to boot an abstract
// profile, so offering base as a target produces a refusal a person cannot
// act on -- while a PART is an ordinary profile cairn will happily boot, so
// hiding one would be Tachyon inventing a distinction the catalog does not
// make.
func TestTargetsExcludesAbstractProfilesOnly(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "profiles/base.md", "id: base\nname: Base\nabstract: true\n")
	writeProfile(t, root, "profiles/engineer.md", "id: engineer\nname: Engineer\ndescription: Implements one task.\n")
	writeProfile(t, root, "profiles/architect.md", "id: architect\nname: Architect\n")
	writeProfile(t, root, "profiles/parts/nanite-domain.md", "id: nanite-domain\n")

	svc := NewService(newRootStore(t, root))
	got, err := svc.Targets()
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}

	ids := make([]string, 0, len(got))
	byID := map[string]Target{}
	for _, target := range got {
		ids = append(ids, target.ID)
		byID[target.ID] = target
	}
	want := []string{"architect", "engineer", "nanite-domain"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("Targets() = %v; want %v (abstract excluded, part included)", ids, want)
	}
	if got := byID["engineer"]; got.Name != "Engineer" || got.Description != "Implements one task." {
		t.Errorf("engineer target = %+v; want its frontmatter carried through for the row", got)
	}
}

// TestTargetsFollowsTheActiveBundle: the target list is the bundle's, and
// changing the bundle changes it. The launch store is the half that must NOT
// move -- see TestLaunchProfileStoreIsNotRootedInTheBundle.
func TestTargetsFollowsTheActiveBundle(t *testing.T) {
	bundleA, bundleB := t.TempDir(), t.TempDir()
	writeProfile(t, bundleA, "profiles/engineer.md", "id: engineer\n")
	writeProfile(t, bundleB, "profiles/writer.md", "id: writer\n")

	store := newRootStore(t, bundleA)
	svc := NewService(store)

	first, err := svc.Targets()
	if err != nil {
		t.Fatalf("Targets against bundle A: %v", err)
	}
	if len(first) != 1 || first[0].ID != "engineer" {
		t.Fatalf("bundle A targets = %+v", first)
	}
	if err := store.Save(bundleB); err != nil {
		t.Fatalf("switching the active bundle: %v", err)
	}
	second, err := svc.Targets()
	if err != nil {
		t.Fatalf("Targets against bundle B: %v", err)
	}
	if len(second) != 1 || second[0].ID != "writer" {
		t.Fatalf("bundle B targets = %+v", second)
	}
}
