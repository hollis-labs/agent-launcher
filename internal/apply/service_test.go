package apply_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/apply"
	"github.com/hollis-labs/tachyon/internal/bundle"
)

func newTestStore(t *testing.T, bundleRoot string) bundle.RootStore {
	t.Helper()
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save(bundleRoot); err != nil {
		t.Fatalf("saving root store: %v", err)
	}
	return store
}

// TestService_StatusUsesInjectedAgentsHome proves Service.Status() never
// touches the real ResolveAgentsHome default when Options.AgentsHome is
// set -- the seam every test in this file (and this whole task) relies on
// to keep every test pointed at scratch directories instead of the real
// ~/.config/agents.
func TestService_StatusUsesInjectedAgentsHome(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "a.md"), "content\n")

	store := newTestStore(t, bundleRoot)
	svc := apply.NewService(store, apply.Options{
		AgentsHome: func() (string, error) { return agentsHome, nil },
	})

	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.AgentsHome != agentsHome {
		t.Fatalf("Status().AgentsHome = %q; want the injected scratch dir %q, not the real default", status.AgentsHome, agentsHome)
	}
	if status.BundleRoot != bundleRoot {
		t.Errorf("Status().BundleRoot = %q; want %q", status.BundleRoot, bundleRoot)
	}
	if !status.Differs {
		t.Errorf("Status().Differs = false; want true (prompt exists only in the bundle)")
	}
}

// TestService_ApplyUsesInjectedRunnerAndAgentsHome proves Service.Apply()
// routes through the injected Runner and AgentsHome, not ExecRunner/
// ResolveAgentsHome -- a fake runner here, recording exactly what it was
// called with, is how this is proven without ever shelling out.
func TestService_ApplyUsesInjectedRunnerAndAgentsHome(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	var gotDir string
	var gotEnv []string
	fake := apply.Runner(func(ctx context.Context, dir string, env []string) ([]byte, []byte, error) {
		gotDir = dir
		gotEnv = env
		return []byte("ok\n"), nil, nil
	})

	store := newTestStore(t, bundleRoot)
	svc := apply.NewService(store, apply.Options{
		AgentsHome: func() (string, error) { return agentsHome, nil },
		Runner:     fake,
	})

	result, err := svc.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gotDir != bundleRoot {
		t.Errorf("runner called with dir=%q; want the bundle root %q", gotDir, bundleRoot)
	}
	wantEnv := "AGENTS_HOME=" + agentsHome
	found := false
	for _, e := range gotEnv {
		if e == wantEnv {
			found = true
		}
	}
	if !found {
		t.Errorf("runner env = %v; want it to contain %q", gotEnv, wantEnv)
	}
	if result.AgentsHome != agentsHome || result.BundleRoot != bundleRoot {
		t.Errorf("Result = %+v; want AgentsHome=%q BundleRoot=%q", result, agentsHome, bundleRoot)
	}
}

// TestService_ApplyFailurePropagatesInvokeError proves a failing injected
// runner surfaces as a real error from Service.Apply(), not a swallowed or
// generic one.
func TestService_ApplyFailurePropagatesInvokeError(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	fake := apply.Runner(func(ctx context.Context, dir string, env []string) ([]byte, []byte, error) {
		return nil, []byte("make: *** No rule to make target `install-system'.  Stop."), errors.New("exit status 2")
	})

	store := newTestStore(t, bundleRoot)
	svc := apply.NewService(store, apply.Options{
		AgentsHome: func() (string, error) { return agentsHome, nil },
		Runner:     fake,
	})

	_, err := svc.Apply()
	if err == nil {
		t.Fatal("Apply returned no error for a failing runner")
	}
	var invokeErr *apply.InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("error is not a *apply.InvokeError: %T (%v)", err, err)
	}
}

// TestService_StatusReEvaluatesEveryCall proves Status() is never cached:
// two calls with the AGENTS_HOME side changed in between must disagree.
// This is "genuinely re-evaluated whenever it matters", proven directly.
func TestService_StatusReEvaluatesEveryCall(t *testing.T) {
	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()
	mustWrite(t, filepath.Join(bundleRoot, "templates", "agents.md"), "content\n")

	store := newTestStore(t, bundleRoot)
	svc := apply.NewService(store, apply.Options{
		AgentsHome: func() (string, error) { return agentsHome, nil },
	})

	first, err := svc.Status()
	if err != nil {
		t.Fatalf("Status (first): %v", err)
	}
	if !first.Differs {
		t.Fatal("Status (first).Differs = false; want true")
	}

	// Stage it by hand (standing in for a real Apply having just run).
	mustWrite(t, filepath.Join(agentsHome, "templates", "agents.md"), "content\n")

	second, err := svc.Status()
	if err != nil {
		t.Fatalf("Status (second): %v", err)
	}
	if second.Differs {
		t.Fatal("Status (second).Differs = true; want false -- Status must read the filesystem fresh, not a cached first answer")
	}
}
