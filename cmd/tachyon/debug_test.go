package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// TestRunNeverMovesCurrentAside is the acceptance guard requiring that
// running this command's core logic twice, against the same scratch boot
// root, leaves the filesystem unchanged: no .prev-* sibling appears,
// because [Run] never calls boot.Prepare — the one thing that would create
// one. A fake Runner stands in for cairn here so the property holds
// deterministically, with no dependency on cairn being on PATH.
// TestRunAgainstRealCairn (integration_test.go) proves the same property
// again, against the real binary, when one is available.
func TestRunNeverMovesCurrentAside(t *testing.T) {
	scratchRoot := t.TempDir()
	const target = "some-binding"
	key := boot.Key(target)
	currentDir := boot.CurrentPath(scratchRoot, key)

	// Seed a boot directory exactly like one a previous, real cairn boot
	// would have planted — the state Run is supposed to find and leave
	// alone, never move aside.
	if err := os.MkdirAll(filepath.Join(currentDir, ".claude"), 0o755); err != nil {
		t.Fatalf("seeding fake plant: %v", err)
	}
	marker := filepath.Join(currentDir, ".claude", "settings.json")
	if err := os.WriteFile(marker, []byte(`{"marker":true}`), 0o644); err != nil {
		t.Fatalf("seeding fake plant: %v", err)
	}

	scope := "/some/scope"
	fakeResult := boot.Result{
		BootDir:       currentDir,
		Provider:      "claude",
		Scope:         &scope,
		SettingsPath:  &marker,
		CwdPreference: "boot_dir",
		ProjectDirArg: []string{"--add-dir", "{{.ProjectDir}}"},
	}
	resultJSON, err := json.Marshal(fakeResult)
	if err != nil {
		t.Fatalf("marshal fake result: %v", err)
	}

	// The fake Runner never touches scratchRoot itself — Run's own code is
	// what this test holds to account, not a stand-in cairn's.
	runner := func(_ context.Context, _ []string) ([]byte, []byte, error) {
		return resultJSON, nil, nil
	}

	cfg := Config{
		Target:   target,
		Bundle:   "/bundle/root",
		BootRoot: scratchRoot,
	}

	before := snapshotTree(t, scratchRoot)

	report1, err := Run(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("Run (1st call): %v", err)
	}
	if report1.InvokeErr != nil {
		t.Fatalf("Run (1st call): unexpected InvokeErr: %v", report1.InvokeErr)
	}
	afterFirst := snapshotTree(t, scratchRoot)
	if !reflect.DeepEqual(before, afterFirst) {
		t.Fatalf("Run's first call changed the filesystem:\nbefore: %v\nafter:  %v", before, afterFirst)
	}

	report2, err := Run(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("Run (2nd call): %v", err)
	}
	if report2.InvokeErr != nil {
		t.Fatalf("Run (2nd call): unexpected InvokeErr: %v", report2.InvokeErr)
	}
	afterSecond := snapshotTree(t, scratchRoot)
	if !reflect.DeepEqual(afterFirst, afterSecond) {
		t.Fatalf("Run's second call changed the filesystem:\nafter 1st: %v\nafter 2nd: %v", afterFirst, afterSecond)
	}

	for _, p := range afterSecond {
		if strings.Contains(p, boot.PrevPrefix) {
			t.Errorf("found a %s* entry at %q — Run must never call boot.Prepare", boot.PrevPrefix, p)
		}
	}

	if report1.HarnessArgv == nil || !argvHasFlag(report1.HarnessArgv, "--settings") {
		t.Errorf("report1.HarnessArgv %v does not contain --settings", report1.HarnessArgv)
	}
}

// TestRunRefusesEmptyBootRoot proves Run passes cfg straight to
// compose.Build rather than defaulting anything itself — an empty BootRoot
// must fail exactly the way compose.Build's own ErrNoBootRoot does (D9),
// with no argv, no key and no ExpectedBootDir computed from an empty root.
func TestRunRefusesEmptyBootRoot(t *testing.T) {
	cfg := Config{Target: "planner", Bundle: "/bundle/root"}
	report, err := Run(context.Background(), cfg, func(context.Context, []string) ([]byte, []byte, error) {
		t.Fatal("runner must not be invoked when compose.Build itself refuses cfg")
		return nil, nil, nil
	})
	if err == nil {
		t.Fatalf("Run with empty BootRoot: error = nil, want non-nil")
	}
	if report.Argv != nil {
		t.Errorf("Run with empty BootRoot: report.Argv = %v, want nil", report.Argv)
	}
}

// snapshotTree returns every path under root, relative to root and sorted,
// so two snapshots can be compared with reflect.DeepEqual regardless of
// directory-read order.
func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(paths)
	return paths
}
