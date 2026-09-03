package compose_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/state"
)

// buildCase is one table-driven Build test: a Composition and the exact argv
// it must produce.
type buildCase struct {
	name string
	comp compose.Composition
	want []string
}

// tachyonBootRoot is the boot root every case below uses: a value under
// Tachyon's own state directory, computed by internal/state — the single
// definition internal/shell.DefaultPrefsPath and
// internal/bundle.DefaultRootStore also build their own paths from — never
// Cairn's own default of dev/agent-os/runtime/boot under $HOME. See
// TestBootRootNeverImplicit, which is the test this value exists to feed.
func tachyonBootRoot(t *testing.T) string {
	t.Helper()
	root, err := state.BootRoot()
	if err != nil {
		t.Skipf("no user config dir on this machine: %v", err)
	}
	return root
}

// buildCases is the single canonical set of compositions exercised by both
// TestBuild (which checks the full argv each one produces) and
// TestBootRootNeverImplicit (which replays every one of them and checks only
// the --boot-root hazard). One shared table means the hazard test can never
// quietly drift out of sync with what TestBuild actually covers.
func buildCases(bootRoot string) []buildCase {
	return []buildCase{
		{
			name: "minimal composition: target, bundle and boot root only",
			comp: compose.Composition{
				Target:   "planner",
				Bundle:   "/Users/chrispian/dev/projects/agent-setup",
				BootRoot: bootRoot,
			},
			want: []string{
				"boot", "planner",
				"--profile", "/Users/chrispian/dev/projects/agent-setup",
				"--boot-root", bootRoot,
				"--session", "current",
				"--json",
			},
		},
		{
			name: "target may be a saved binding's name rather than a bare profile id",
			comp: compose.Composition{
				Target:   "my-saved-binding",
				Bundle:   "/Users/chrispian/dev/projects/agent-setup",
				BootRoot: bootRoot,
			},
			want: []string{
				"boot", "my-saved-binding",
				"--profile", "/Users/chrispian/dev/projects/agent-setup",
				"--boot-root", bootRoot,
				"--session", "current",
				"--json",
			},
		},
		{
			name: "skills render as one comma-joined --skill flag, not one flag per skill",
			comp: compose.Composition{
				Target:   "architect",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Skills:   []string{"go-testing", "code-review", "sql"},
			},
			want: []string{
				"boot", "architect",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--session", "current",
				"--skill", "go-testing,code-review,sql",
				"--json",
			},
		},
		{
			name: "a single skill still renders through --skill",
			comp: compose.Composition{
				Target:   "architect",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Skills:   []string{"go-testing"},
			},
			want: []string{
				"boot", "architect",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--session", "current",
				"--skill", "go-testing",
				"--json",
			},
		},
		{
			name: "additional parts render as one repeated --with flag per part, in order",
			comp: compose.Composition{
				Target:   "engineer",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Parts:    []string{"extra-context", "extra-tools"},
			},
			want: []string{
				"boot", "engineer",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--session", "current",
				"--with", "extra-context",
				"--with", "extra-tools",
				"--json",
			},
		},
		{
			name: "one-off directions render as one repeated --set slot=value flag, in order",
			comp: compose.Composition{
				Target:   "writer",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Sets: []compose.Set{
					{Slot: "tone", Value: "terse"},
					{Slot: "audience", Value: "internal"},
				},
			},
			want: []string{
				"boot", "writer",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--session", "current",
				"--set", "tone=terse",
				"--set", "audience=internal",
				"--json",
			},
		},
		{
			name: "project path renders as --scope",
			comp: compose.Composition{
				Target:   "reviewer",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Scope:    "/Users/chrispian/dev/projects/tachyon",
			},
			want: []string{
				"boot", "reviewer",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--session", "current",
				"--scope", "/Users/chrispian/dev/projects/tachyon",
				"--json",
			},
		},
		{
			name: "no project path means no --scope at all, not an empty one",
			comp: compose.Composition{
				Target:   "reviewer",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
			},
			want: []string{
				"boot", "reviewer",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--session", "current",
				"--json",
			},
		},
		{
			name: "every control at once, in the target contract's fixed order",
			comp: compose.Composition{
				Target:   "conductor",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Parts:    []string{"extra-part"},
				Skills:   []string{"skill-one", "skill-two"},
				Sets: []compose.Set{
					{Slot: "slot-one", Value: "value-one"},
				},
				Scope: "/scope/path",
			},
			want: []string{
				"boot", "conductor",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--session", "current",
				"--with", "extra-part",
				"--skill", "skill-one,skill-two",
				"--set", "slot-one=value-one",
				"--scope", "/scope/path",
				"--json",
			},
		},
	}
}

func TestBuild(t *testing.T) {
	bootRoot := tachyonBootRoot(t)
	for _, tc := range buildCases(bootRoot) {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compose.Build(tc.comp)
			if err != nil {
				t.Fatalf("Build: unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Build(%+v) =\n  %#v\nwant\n  %#v", tc.comp, got, tc.want)
			}
		})
	}
}

// TestBootRootNeverImplicit is the hazard guard the task record calls out by
// name (D9): --boot-root must be present on every single generated
// invocation, with a value under Tachyon's own state directory — never
// Cairn's default of dev/agent-os/runtime/boot under $HOME, which lands
// inside a real, dirty git working tree.
//
// It replays every composition buildCases defines — the same set TestBuild
// checks in full — rather than a hand-picked subset, so this guard cannot
// silently stop covering a shape TestBuild was extended to cover.
func TestBootRootNeverImplicit(t *testing.T) {
	tachyonStateDir, err := state.Root()
	if err != nil {
		t.Skipf("no user config dir on this machine: %v", err)
	}
	bootRoot, err := state.BootRoot()
	if err != nil {
		t.Skipf("no user config dir on this machine: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	// Cairn's own default (bootdir.DefaultRootRel = "dev/agent-os/runtime/boot"),
	// expanded the same way Cairn expands it, purely so this test can prove
	// the generated value is NOT this — never executed, never touched.
	cairnDefaultBootRoot := filepath.Join(home, "dev", "agent-os", "runtime", "boot")

	for _, tc := range buildCases(bootRoot) {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compose.Build(tc.comp)
			if err != nil {
				t.Fatalf("Build: unexpected error: %v", err)
			}

			idx := -1
			for i, a := range got {
				if a == "--boot-root" {
					idx = i
					break
				}
			}
			if idx == -1 {
				t.Fatalf("argv %v does not contain --boot-root at all", got)
			}
			if idx+1 >= len(got) {
				t.Fatalf("argv %v has --boot-root as its last element, with no value", got)
			}
			value := got[idx+1]

			if !strings.HasPrefix(value, tachyonStateDir) {
				t.Errorf("--boot-root value %q is not under Tachyon's state directory %q", value, tachyonStateDir)
			}
			if value == cairnDefaultBootRoot {
				t.Errorf("--boot-root value %q equals Cairn's own default boot root under ~/dev — exactly the D9 hazard", value)
			}
			if strings.Contains(value, filepath.Join("dev", "agent-os")) {
				t.Errorf("--boot-root value %q falls inside ~/dev/agent-os, a live git working tree — exactly the D9 hazard", value)
			}
		})
	}
}

func TestBuildRefusesEmptyTarget(t *testing.T) {
	_, err := compose.Build(compose.Composition{
		Bundle:   "/bundle/root",
		BootRoot: "/state/dir/boot",
	})
	if !errors.Is(err, compose.ErrNoTarget) {
		t.Fatalf("Build with empty Target: error = %v; want ErrNoTarget", err)
	}
}

func TestBuildRefusesEmptyBundle(t *testing.T) {
	_, err := compose.Build(compose.Composition{
		Target:   "planner",
		BootRoot: "/state/dir/boot",
	})
	if !errors.Is(err, compose.ErrNoBundle) {
		t.Fatalf("Build with empty Bundle: error = %v; want ErrNoBundle", err)
	}
}

// TestBuildRefusesEmptyBootRoot is the other half of the D9 guard: Build
// must refuse to produce an argv at all when no boot root was given, rather
// than silently omitting --boot-root and letting whatever runs the argv fall
// back to CAIRN_BOOT_ROOT (or Cairn's own default, when that is unset too).
func TestBuildRefusesEmptyBootRoot(t *testing.T) {
	got, err := compose.Build(compose.Composition{
		Target: "planner",
		Bundle: "/bundle/root",
	})
	if !errors.Is(err, compose.ErrNoBootRoot) {
		t.Fatalf("Build with empty BootRoot: error = %v; want ErrNoBootRoot", err)
	}
	if got != nil {
		t.Errorf("Build with empty BootRoot returned argv %v; want nil", got)
	}
}

// TestBuildNeverEmitsForbiddenFlags is the standing rule for this whole
// batch, checked directly rather than only by absence of code that would
// emit it: nothing this package generates ever names --db, or the word
// "sqlite" in any flag or value, for any composition in the shared table —
// that store is being removed elsewhere in this rebuild.
func TestBuildNeverEmitsForbiddenFlags(t *testing.T) {
	bootRoot := tachyonBootRoot(t)
	for _, tc := range buildCases(bootRoot) {
		got, err := compose.Build(tc.comp)
		if err != nil {
			t.Fatalf("%s: Build: unexpected error: %v", tc.name, err)
		}
		for _, a := range got {
			if a == "--db" {
				t.Errorf("%s: argv %v contains --db", tc.name, got)
			}
			if strings.Contains(strings.ToLower(a), "sqlite") {
				t.Errorf("%s: argv %v contains a sqlite reference: %q", tc.name, got, a)
			}
		}
	}
}
