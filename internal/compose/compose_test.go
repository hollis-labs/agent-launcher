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
				"--skill", "go-testing",
				"--json",
			},
		},
		{
			name: "prompts render as one comma-joined --prompt flag, not one flag per prompt",
			comp: compose.Composition{
				Target:   "architect",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Prompts:  []string{"report", "onboarding"},
			},
			want: []string{
				"boot", "architect",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--prompt", "report,onboarding",
				"--json",
			},
		},
		{
			name: "a single prompt still renders through --prompt",
			comp: compose.Composition{
				Target:   "architect",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Prompts:  []string{"report"},
			},
			want: []string{
				"boot", "architect",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--prompt", "report",
				"--json",
			},
		},
		{
			name: "skills and prompts together render as --skill then --prompt, both present",
			comp: compose.Composition{
				Target:   "architect",
				Bundle:   "/bundle/root",
				BootRoot: bootRoot,
				Skills:   []string{"go-testing"},
				Prompts:  []string{"report"},
			},
			want: []string{
				"boot", "architect",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--skill", "go-testing",
				"--prompt", "report",
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
				Prompts:  []string{"report", "onboarding"},
				Sets: []compose.Set{
					{Slot: "slot-one", Value: "value-one"},
				},
				Scope: "/scope/path",
			},
			want: []string{
				"boot", "conductor",
				"--profile", "/bundle/root",
				"--boot-root", bootRoot,
				"--with", "extra-part",
				"--skill", "skill-one,skill-two",
				"--prompt", "report,onboarding",
				"--set", "slot-one=value-one",
				"--scope", "/scope/path",
				"--json",
			},
		},
		{
			// The provider arrives as a launch profile in Parts, by path,
			// from outside the bundle. It renders as an ordinary --with,
			// which is the whole point: cairn resolves it through the same
			// cascade as any other profile and never learns it came from a
			// launcher.
			name: "launch profile supplies the provider, by path",
			comp: compose.Composition{
				Target:   "engineer",
				Bundle:   "/Users/chrispian/dev/projects/agent-setup",
				BootRoot: bootRoot,
				Parts:    []string{"/Users/chrispian/.config/tachyon/launch/codex.md"},
			},
			want: []string{
				"boot", "engineer",
				"--profile", "/Users/chrispian/dev/projects/agent-setup",
				"--boot-root", bootRoot,
				"--with", "/Users/chrispian/.config/tachyon/launch/codex.md",
				"--json",
			},
		},
		{
			name: "a launch profile alongside everything else a compose form can add",
			comp: compose.Composition{
				Target:   "orchestrator",
				Bundle:   "/Users/chrispian/dev/projects/agent-setup",
				BootRoot: bootRoot,
				Session:  "codex-agent-setup-abc123",
				Parts:    []string{"/Users/chrispian/.config/tachyon/launch/codex.md", "nanite-domain"},
				Skills:   []string{"surface-discovery"},
				Scope:    "/Users/chrispian/dev/projects/agent-setup",
			},
			want: []string{
				"boot", "orchestrator",
				"--profile", "/Users/chrispian/dev/projects/agent-setup",
				"--boot-root", bootRoot,
				"--session", "codex-agent-setup-abc123",
				"--with", "/Users/chrispian/.config/tachyon/launch/codex.md",
				"--with", "nanite-domain",
				"--skill", "surface-discovery",
				"--scope", "/Users/chrispian/dev/projects/agent-setup",
				"--json",
			},
		},
	}
}

// TestProviderFlagIsNeverEmitted pins the removal of --provider, which is a
// contract change rather than a tidy-up.
//
// The flag used to default to whatever the resolved profile declared. No
// profile in agent-setup declares a provider since 2026-09-10, so that
// default now resolves to nothing and cairn refuses the render. The provider
// arrives in a launch profile instead — an ordinary part in Parts — so it is
// still composed and still never inferred, one layer further out.
//
// If this flag ever comes back, it is a second source for a value a file
// already declares, and the two can disagree.
func TestProviderFlagIsNeverEmitted(t *testing.T) {
	bootRoot := tachyonBootRoot(t)
	for _, tc := range buildCases(bootRoot) {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compose.Build(tc.comp)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			for _, a := range got {
				if a == "--provider" {
					t.Fatalf("Build(%+v) = %v; emitted --provider", tc.comp, got)
				}
			}
		})
	}
}

// TestArgumentsCarriesTheLaunchProfileForShowToo: internal/preview builds
// `cairn show` argv through Arguments, and a preview resolving a different
// provider than the launch would show the wrong effective skills. The launch
// profile is what carries the provider now, so it has to reach show through
// the shared encoder rather than only Build.
func TestArgumentsCarriesTheLaunchProfileForShow(t *testing.T) {
	const launchProfile = "/Users/chrispian/.config/tachyon/launch/codex.md"
	got, err := compose.Arguments(compose.Composition{
		Target: "engineer",
		Bundle: "/Users/chrispian/dev/projects/agent-setup",
		Parts:  []string{launchProfile},
	})
	if err != nil {
		t.Fatalf("Arguments: %v", err)
	}
	want := []string{
		"engineer",
		"--profile", "/Users/chrispian/dev/projects/agent-setup",
		"--with", launchProfile,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Arguments = %#v; want %#v", got, want)
	}
}

// TestArgumentsNeverCarriesBootOnlyFlags: show must not receive --boot-root
// or --session, which are boot's alone. Build adds them around Arguments.
func TestArgumentsNeverCarriesBootOnlyFlags(t *testing.T) {
	got, err := compose.Arguments(compose.Composition{
		Target:   "engineer",
		Bundle:   "/bundle",
		BootRoot: "/state/boot",
		Session:  "some-session",
	})
	if err != nil {
		t.Fatalf("Arguments: %v", err)
	}
	for _, a := range got {
		if a == "--boot-root" || a == "--session" {
			t.Fatalf("Arguments = %#v; carries %q, which belongs to boot alone", got, a)
		}
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
	tachyonStateDir, err := state.StateDir()
	if err != nil {
		t.Skipf("no state dir on this machine: %v", err)
	}
	bootRoot, err := state.BootRoot()
	if err != nil {
		t.Skipf("no state dir on this machine: %v", err)
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

// TestSessionIsOmittedWhenEmpty: an empty Session means no flag, which lets
// Cairn choose its own segment. That is the right default for this package
// (it decides nothing on a caller's behalf) and the wrong one for Tachyon,
// which is why internal/launch always supplies one — see the next test.
func TestSessionIsOmittedWhenEmpty(t *testing.T) {
	got, err := compose.Build(compose.Composition{
		Target:   "engineer",
		Bundle:   "/bundle",
		BootRoot: "/state/boot",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, a := range got {
		if a == "--session" {
			t.Fatalf("Build = %v; emitted --session for an empty Session", got)
		}
	}
}

// TestSessionRendersVerbatim pins that this package neither derives nor
// validates the segment. Deriving one here would put the boot directory's
// identity in two places: internal/boot.SessionKey builds it from a
// composition's content, and this package only spells it onto a command line.
func TestSessionRendersVerbatim(t *testing.T) {
	const session = "codex-users-somebody-dev-projects-cairn-0123abcd"
	got, err := compose.Build(compose.Composition{
		Target:   "engineer",
		Bundle:   "/bundle",
		BootRoot: "/state/boot",
		Session:  session,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i, a := range got {
		if a == "--session" {
			if got[i+1] != session {
				t.Fatalf("--session = %q; want %q", got[i+1], session)
			}
			return
		}
	}
	t.Fatalf("Build = %v; no --session for a set Session", got)
}
