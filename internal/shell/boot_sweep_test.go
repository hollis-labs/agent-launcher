package shell

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/state"
)

// This file proves the wiring CW-20260903-0019 adds -- [Shell.SweepBootDirectories]
// (the automatic sweep [Shell.wireBootSweep] runs once at startup) and
// [Service.SweepBootDirectories] (the manual action) -- actually resolves
// the real boot root and runs the real lsof-backed guard end to end,
// entirely against a scratch state directory each test builds and owns
// via [state.DirEnv] (TACHYON_STATE_DIR). Neither test here, nor anything
// else in this package or this repository, is ever pointed at the real
// ~/Library/Application Support/Tachyon/boot -- see [state.DirEnv]'s own
// doc comment for why that env var override exists and is the sanctioned
// way to redirect it in a test.

// newTestShell returns a *Shell with just enough set to call
// SweepBootDirectories: it reads only s.log among Shell's fields (a Warn
// log line if lsof can't be found via LookPath), so a bare struct literal
// is enough here -- no window, no app, no tray, nothing this package's
// other tests need real Wails machinery for.
func newTestShell() *Shell {
	return &Shell{log: slog.Default()}
}

func requireLsofForWiringTest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not found on PATH; skipping real-lsof wiring test")
	}
}

// TestShell_SweepBootDirectories_RealBootRootRealLsof proves
// Shell.SweepBootDirectories resolves state.BootRoot() and a real lsof
// binary, and that boot.Sweep genuinely removes a genuinely free .prev-*
// candidate planted directly under that resolved root -- the same
// end-to-end path [Shell.wireBootSweep] runs once at app start, exercised
// here against a scratch TACHYON_STATE_DIR rather than the real one.
func TestShell_SweepBootDirectories_RealBootRootRealLsof(t *testing.T) {
	requireLsofForWiringTest(t)

	scratch := t.TempDir()
	t.Setenv(state.DirEnv, scratch)

	root, err := state.BootRoot()
	if err != nil {
		t.Fatalf("state.BootRoot: %v", err)
	}
	if !filepath.IsAbs(root) || !strings.HasPrefix(root, scratch) {
		t.Fatalf("state.BootRoot() = %q; want an absolute path under the scratch dir %q", root, scratch)
	}

	// The real layout: <boot-root>/<project>/<profile>/.prev-*. A fixture
	// one level shallower would be invisible to the sweep, and this test
	// would pass by finding nothing to do — see internal/boot's
	// TestSweepFindsWhatPrepareMovedAside for the same trap one layer down.
	free := filepath.Join(root, boot.ProjectKey("/work/nanite"), "engineer", boot.PrevPrefix+"20260904T000000.000000000Z")
	if err := os.MkdirAll(free, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", free, err)
	}

	s := newTestShell()
	report, err := s.SweepBootDirectories(context.Background())
	if err != nil {
		t.Fatalf("SweepBootDirectories: %v", err)
	}
	if !report.GuardOK {
		t.Fatalf("report.GuardOK = false; want true (real lsof, real positive control against this test process's own cwd): %s", report.GuardDetail)
	}
	if _, statErr := os.Stat(free); !os.IsNotExist(statErr) {
		t.Fatalf("expected %s to have been removed by the real sweep; stat err = %v", free, statErr)
	}
	found := false
	for _, p := range report.Swept {
		if p == free {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s in report.Swept; report = %+v", free, report)
	}
}

// TestService_SweepBootDirectories_DelegatesToRealSweep proves the manual
// action a person clicks in the manager actually performs the real sweep
// -- not a second, parallel implementation -- by observing the same
// externally visible effect (a genuinely free candidate under the
// resolved boot root gets removed) through Service.SweepBootDirectories
// alone.
func TestService_SweepBootDirectories_DelegatesToRealSweep(t *testing.T) {
	requireLsofForWiringTest(t)

	scratch := t.TempDir()
	t.Setenv(state.DirEnv, scratch)

	root, err := state.BootRoot()
	if err != nil {
		t.Fatalf("state.BootRoot: %v", err)
	}
	free := filepath.Join(root, boot.ProjectKey("/work/tachyon"), "planner", boot.PrevPrefix+"20260904T010000.000000000Z")
	if err := os.MkdirAll(free, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", free, err)
	}

	svc := &Service{shell: newTestShell()}
	report, err := svc.SweepBootDirectories()
	if err != nil {
		t.Fatalf("Service.SweepBootDirectories: %v", err)
	}
	if !report.GuardOK {
		t.Fatalf("report.GuardOK = false; want true: %s", report.GuardDetail)
	}
	if _, statErr := os.Stat(free); !os.IsNotExist(statErr) {
		t.Fatalf("expected %s to have been removed via the manual action's delegation to the real sweep; stat err = %v", free, statErr)
	}
}
