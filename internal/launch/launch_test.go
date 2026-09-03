package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
)

// This is a white-box test (package launch, not launch_test) because it
// drives the unexported launch() and Service.resolveBinding() directly --
// the orchestration seams a fake boot.Runner and a fake spawn func plug
// into, per this task's own testing section: fakes for the runner and the
// spawn step, and a real t.TempDir() bundle with a hand-written
// bindings.yaml for binding resolution, without ever running cairn or
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
}

func (r *spawnRecorder) spawn(argv []string, cwd string) error {
	r.called = true
	r.argv = argv
	r.cwd = cwd
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
// map has no entry for.
const unknownProviderFixture = `{
  "boot_dir": "/state/boot/codex-thing/current",
  "provider": "codex",
  "scope": null,
  "settings_path": null,
  "cwd_preference": "boot_dir",
  "project_dir_arg": null
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
	// resolves it via bindings.yaml -- see launch()'s own doc comment.
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
	b := binding.Binding{Name: "codex-thing", Profile: "x", Scope: "/x"}

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

func writeBindingsYAML(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "bindings.yaml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing bindings.yaml: %v", err)
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
	writeBindingsYAML(t, bundleDir, "bindings:\n  eng-nanite: { profile: engineer, scope: /Users/chrispian/dev/hollis-labs/apps/nanite }\n")

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
	writeBindingsYAML(t, bundleDir, "bindings:\n  eng-nanite: { profile: engineer, scope: /x }\n")

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
