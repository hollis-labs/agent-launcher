package launch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hollis-labs/go-agent-launch/agentlaunch"
	"github.com/hollis-labs/go-agent-launch/agentlaunch/launcher"
)

// loadTestCatalog loads the embedded corpus or fails the test.
func loadTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(cat.Specs) == 0 {
		t.Fatal("LoadCatalog returned no specs")
	}
	t.Cleanup(cat.Close)
	return cat
}

// TestListContractShape checks every list Spec carries the FROZEN
// contract fields: id/name/project/role/summary/facets.
func TestListContractShape(t *testing.T) {
	cat := loadTestCatalog(t)
	for _, s := range cat.Specs {
		if s.ID == "" {
			t.Errorf("spec has empty id: %+v", s)
		}
		if s.Name == "" {
			t.Errorf("spec %q has empty name", s.ID)
		}
		if s.Facets == nil {
			t.Errorf("spec %q has nil facets", s.ID)
		}
	}
}

// TestFilterByFacet checks --facet filtering narrows the result set.
func TestFilterByFacet(t *testing.T) {
	cat := loadTestCatalog(t)
	tether := cat.Filter(map[string]string{"project": "tether"})
	if len(tether) == 0 {
		t.Fatal("expected at least one tether spec")
	}
	for _, s := range tether {
		if s.Project != "tether" {
			t.Errorf("facet filter leaked spec %q (project=%q)", s.ID, s.Project)
		}
	}
	none := cat.Filter(map[string]string{"project": "no-such-project"})
	if len(none) != 0 {
		t.Errorf("unmatched facet returned %d specs", len(none))
	}
}

// TestDescribeContractShape checks describe returns the FROZEN contract
// shape: id/name/inputs[name,type,required,default,description]/runners.
func TestDescribeContractShape(t *testing.T) {
	cat := loadTestCatalog(t)
	id := cat.Specs[0].ID
	desc, err := Describe(cat, id)
	if err != nil {
		t.Fatalf("Describe(%q): %v", id, err)
	}
	if desc.ID != id {
		t.Errorf("describe id = %q, want %q", desc.ID, id)
	}
	if len(desc.Inputs) == 0 {
		t.Error("describe returned no inputs")
	}
	if len(desc.Runners) == 0 {
		t.Error("describe returned no runners")
	}
	var sawWorkDir, sawRunner bool
	for _, in := range desc.Inputs {
		switch in.Name {
		case "work_dir":
			sawWorkDir = in.Required
		case "runner":
			sawRunner = in.Required
		}
	}
	if !sawWorkDir || !sawRunner {
		t.Error("work_dir and runner must be present and required")
	}
}

// TestDescribeUnknownSpec checks an unknown id is a hard error.
func TestDescribeUnknownSpec(t *testing.T) {
	cat := loadTestCatalog(t)
	if _, err := Describe(cat, "no-such-spec"); err == nil {
		t.Fatal("expected error for unknown spec")
	}
}

// TestLaunchReady checks the launch happy path: PlanFromLaunch -> Compile
// -> PrepareAndPlant -> ToSessionLaunch yields a Ready with status=ready
// and a map env.
func TestLaunchReady(t *testing.T) {
	cat := loadTestCatalog(t)
	ready, verr, err := Launch(cat, LaunchParams{ID: "tether-claude"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if verr != nil {
		t.Fatalf("unexpected var_error: %+v", verr)
	}
	if ready == nil || ready.Status != "ready" {
		t.Fatalf("ready = %+v", ready)
	}
	if ready.Binary == "" {
		t.Error("ready.Binary is empty")
	}
	if ready.Env == nil {
		t.Error("ready.Env must be a (possibly empty) map, not nil")
	}
}

// TestLaunchUnknownInputKey checks an --input key the LaunchSpec does not
// declare is a hard error (no silent drop).
func TestLaunchUnknownInputKey(t *testing.T) {
	cat := loadTestCatalog(t)
	_, _, err := Launch(cat, LaunchParams{
		ID:        "tether-claude",
		Overrides: map[string]string{"not_a_real_input": "x"},
	})
	if err == nil {
		t.Fatal("expected error for undeclared --input key")
	}
}

// TestLaunchUnknownSpec checks an unknown spec id is a hard error.
func TestLaunchUnknownSpec(t *testing.T) {
	cat := loadTestCatalog(t)
	if _, _, err := Launch(cat, LaunchParams{ID: "no-such-spec"}); err == nil {
		t.Fatal("expected error for unknown spec")
	}
}

// TestResolveRunner checks the runner-token bridge and its error path.
// Every token the corpus advertises must resolve to a RuntimeBinding.
func TestResolveRunner(t *testing.T) {
	cat := loadTestCatalog(t)
	for _, tok := range cat.KnownRunners() {
		if _, err := cat.ResolveRunner(tok); err != nil {
			t.Errorf("ResolveRunner(%q): %v", tok, err)
		}
	}
	if _, err := cat.ResolveRunner("bogus-runner"); err == nil {
		t.Fatal("expected error for unknown runner token")
	}
}

// TestInteractiveOnly guards the item-2 decision (D-T2): every runner
// `describe` advertises must resolve to an interactive LaunchMode, and NO
// runner may be a non-interactive (streaming-stdio) claude runner.
// Tachyon ships no claude-stream — this test fails loudly if a streaming
// runtime-binding is ever re-introduced into the corpus.
func TestInteractiveOnly(t *testing.T) {
	cat := loadTestCatalog(t)
	runners := cat.KnownRunners()
	if len(runners) == 0 {
		t.Fatal("corpus advertises no runners")
	}
	for _, tok := range runners {
		if tok == "claude-stream" {
			t.Errorf("claude-stream runner is advertised — Tachyon is interactive-only (D-T2)")
		}
		binding, err := cat.ResolveRunner(tok)
		if err != nil {
			t.Fatalf("ResolveRunner(%q): %v", tok, err)
		}
		mode, err := launchModeForRuntime(binding)
		if err != nil {
			t.Errorf("runner %q does not resolve to an interactive mode: %v", tok, err)
			continue
		}
		if mode != agentlaunch.LaunchInteractive {
			t.Errorf("runner %q resolved to mode %q, want %q", tok, mode, agentlaunch.LaunchInteractive)
		}
		// No claude runner may carry a non-interactive runtime shape.
		if binding.Provider == "claude" && binding.RuntimeKind == agentlaunch.RuntimeStreamingStdio {
			t.Errorf("claude runner %q is non-interactive (streaming-stdio) — forbidden by D-T2", tok)
		}
	}
}

// TestDescribeAdvertisesNoClaudeStream checks the `describe` runners
// array never includes claude-stream — the engine⇄app seam must not
// surface a non-interactive runner.
func TestDescribeAdvertisesNoClaudeStream(t *testing.T) {
	cat := loadTestCatalog(t)
	desc, err := Describe(cat, "tether-claude")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	for _, r := range desc.Runners {
		if r == "claude-stream" {
			t.Fatalf("describe advertised claude-stream runner: %v", desc.Runners)
		}
	}
}

// TestHeadlessClaudeRejected is the negative test for the v0.3.x
// fail-fast gate. Tachyon ships no headless/background bags, but a
// deliberately-constructed background claude launch with no permission
// posture must still be rejected by the go-agent-launch Compile gate
// with ErrHeadlessClaudeNeedsPermission — proving the engine surfaces
// the gate rather than producing a launch guaranteed to hang.
func TestHeadlessClaudeRejected(t *testing.T) {
	plan := agentlaunch.LaunchPlan{
		Project: agentlaunch.ProjectSpec{ID: "headless-probe"},
		Agent:   agentlaunch.AgentSpec{ID: "headless-probe", Name: "headless-probe"},
		Provider: agentlaunch.ProviderSpec{
			ID: "claude",
			// Permission deliberately empty — this is what trips the gate.
		},
		Runtime: agentlaunch.RuntimeSubprocess,
		Workspace: agentlaunch.WorkspaceSpec{
			Mode:    agentlaunch.WorkspaceShared,
			Workdir: t.TempDir(),
		},
		BootProfile: agentlaunch.BootProfileRef{
			Inline: &agentlaunch.BootProfileInline{
				BootContent: "headless probe",
				BootMode:    agentlaunch.BootModePlanted,
			},
		},
		// Background == non-interactive: no human to answer an approval
		// prompt. This is the headless stance the gate guards against.
		Mode: agentlaunch.LaunchBackground,
	}
	_, err := launcher.Compile(context.Background(), plan)
	if err == nil {
		t.Fatal("expected Compile to reject a headless claude launch")
	}
	if !errors.Is(err, launcher.ErrHeadlessClaudeNeedsPermission) {
		t.Fatalf("Compile error = %v, want ErrHeadlessClaudeNeedsPermission", err)
	}
}

// TestVarErrorOptions checks the var_error option set matches the FROZEN
// contract triple.
func TestVarErrorOptions(t *testing.T) {
	want := "retry,proceed_cached,cancel"
	if got := strings.Join(onErrorOptions, ","); got != want {
		t.Fatalf("onErrorOptions = %q, want %q", got, want)
	}
}
