package launch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hollis-labs/go-agent-launch/agentlaunch"
	"github.com/hollis-labs/go-agent-launch/agentlaunch/launcher"
	"github.com/hollis-labs/go-agent-launch/agentlaunch/providerplant"
	"github.com/hollis-labs/go-agent-launch/agentlaunch/sessionshim"
)

// Ready is the success result of `launch`: the runnable command the
// Swift app spawns (in iTerm2). The engine does NOT exec the agent — it
// emits this for the app to run.
type Ready struct {
	Status  string            `json:"status"`
	Binary  string            `json:"binary"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Workdir string            `json:"workdir"`
}

// VarError is the structured var-failure result of `launch`. The Swift
// app shows a retry / proceed-with-cached / cancel dialog and re-invokes
// `launch` with --on-error-choice.
type VarError struct {
	Status  string   `json:"status"`
	Var     string   `json:"var"`
	Message string   `json:"message"`
	Options []string `json:"options"`
}

// onErrorOptions is the fixed option set offered on a var_error.
var onErrorOptions = []string{"retry", "proceed_cached", "cancel"}

// LaunchParams is the input to Launch.
type LaunchParams struct {
	// ID is the launch-bag id (from `list`).
	ID string
	// Overrides are the --input KEY=VAL pairs the app collected via the
	// describe form. They override the bag's declared input values.
	Overrides map[string]string
	// OnErrorChoice carries an app-mediated var_error decision on
	// re-invocation: "" (first call), "retry", or "proceed_cached".
	OnErrorChoice string
}

// Launch runs the go-agent-launch pipeline for one launch bag and
// returns the runnable command for the app to spawn.
//
// Pipeline (the go-agent-launch public seam, end to end):
//
//  1. Load the launch bag + its LaunchSpec from the local-first corpus.
//  2. Apply --input overrides, resolve the `runner` token to a
//     RuntimeBinding via agentlaunch.ResolveRuntimeBinding, render the
//     LaunchSpec template body into the boot prompt. A missing required
//     input or an unresolved template var surfaces as a *VarError
//     (exit 2) unless the app already chose proceed_cached.
//  3. Assemble the LaunchPlan via agentlaunch.PlanFromLaunch — the
//     supported LaunchSpec→LaunchPlan bridge — then launcher.Compile it.
//     The bridge carries RuntimeBinding.Permission onto
//     LaunchPlan.Provider.Permission automatically; the engine never
//     hand-threads permission.
//  4. providerplant.PrepareAndPlant — materializes the boot dir, plants
//     the provider boot files, rewires Env/Argv/Workdir.
//  5. sessionshim.ToSessionLaunch — resolves the runnable command.
//
// The launch Mode is DERIVED from the resolved runner (interactive
// runners → LaunchInteractive), never hardcoded — Tachyon is
// interactive-only (D-T2), and deriving it keeps that invariant in one
// place rather than scattered at the call site.
//
// On success a *Ready is returned and verr is nil. On a var failure
// verr is non-nil (the caller emits it and exits 2). A genuine fatal
// error is returned as the error (the caller emits and exits 1).
func Launch(cat *Catalog, p LaunchParams) (ready *Ready, verr *VarError, err error) {
	bag, ok := cat.Bag(p.ID)
	if !ok {
		return nil, nil, fmt.Errorf("unknown spec %q", p.ID)
	}

	// 1+2. Effective inputs = bag inputs ∪ --input overrides (override
	// wins). Reject an override key the LaunchSpec does not declare.
	inputs := effectiveInputs(cat.Spec, bag)
	for k, v := range p.Overrides {
		if !declaresInput(cat.Spec, k) {
			return nil, nil, fmt.Errorf("unknown --input key %q", k)
		}
		inputs[k] = v
	}

	// Expand work_dir to an absolute path BEFORE render: PlanFromLaunch
	// reads work_dir straight from the resolved-input map and Compile's
	// ResolvePlanPaths only runs filepath.Abs (it does not expand a
	// leading ~). Expanding here keeps the resolved-input map and the
	// assembled plan consistent.
	if raw := stringInput(inputs, LaunchInputWorkDir); raw != "" {
		abs, wdErr := expandPath(raw)
		if wdErr != nil {
			return nil, nil, wdErr
		}
		inputs[LaunchInputWorkDir] = abs
	}

	// Resolve the LaunchSpec's derived vars (the folded boot-profile
	// slots) before rendering. The corpus ships literal-source vars so
	// resolution is offline and authorizer-free; a degraded var (on_error
	// =warn) yields a fallback/empty value rather than a hard failure.
	resolver := agentlaunch.NewVarResolver(agentlaunch.VarResolverOptions{
		Authorizer: agentlaunch.AllowAllTrustAuthorizer{},
	})
	resolvedVars, vErr := resolver.ResolveAll(
		context.Background(),
		&cat.Spec.AssemblySpec.BootSpec,
		nil, // nil sinks => VarSinkPromptText for every var
		nil, // none of the corpus vars are required
	)
	if vErr != nil {
		return nil, nil, fmt.Errorf("resolve launch spec vars: %w", vErr)
	}
	varValues := make(map[string]any, len(resolvedVars))
	for name, rv := range resolvedVars {
		varValues[name] = rv.Value
	}

	// Render the LaunchSpec template through the interactive front-end:
	// a missing required input or an unresolved var is REPORTED (not a
	// hard error) so the engine can surface it as a var_error.
	rr, rerr := cat.Spec.Render(agentlaunch.RenderRequest{
		Inputs:   inputs,
		Vars:     varValues,
		FrontEnd: agentlaunch.FrontEndInteractive,
	})
	if rerr != nil {
		return nil, nil, fmt.Errorf("render launch spec: %w", rerr)
	}
	if len(rr.Missing) > 0 && p.OnErrorChoice != "proceed_cached" {
		// Surface the first unsatisfied input/var. proceed_cached lets the
		// app proceed with the degraded (empty-var) render; retry / a
		// fresh --input re-invocation re-enters here.
		first := rr.Missing[0]
		return nil, &VarError{
			Status:  "var_error",
			Var:     strings.TrimPrefix(strings.TrimPrefix(first, "inputs."), "vars."),
			Message: fmt.Sprintf("%s is unresolved; supply it via --input or proceed with cached values", first),
			Options: append([]string(nil), onErrorOptions...),
		}, nil
	}

	if stringInput(inputs, LaunchInputWorkDir) == "" {
		return nil, nil, fmt.Errorf("work_dir is empty")
	}

	// 3. Resolve the `runner` token to a RuntimeBinding through the
	// directory-registry seam, and DERIVE the launch Mode from it.
	// Tachyon is interactive-only (D-T2): every corpus runner is
	// interactive, so the derivation always yields LaunchInteractive —
	// but it is derived, not hardcoded, so a non-interactive runner could
	// not silently slip through.
	runnerTok := stringInput(inputs, LaunchInputRunner)
	binding, runErr := cat.ResolveRunner(runnerTok)
	if runErr != nil {
		return nil, nil, runErr
	}
	mode, modeErr := launchModeForRuntime(binding)
	if modeErr != nil {
		return nil, nil, modeErr
	}

	display := stringInput(inputs, "display_name")
	if display == "" {
		display = bag.Name
	}

	// Assemble the LaunchPlan via the supported LaunchSpec→LaunchPlan
	// bridge. The caller owns resolution (vars, runner→RuntimeBinding,
	// agent identity); PlanFromLaunch owns assembly. It also carries
	// RuntimeBinding.Permission onto LaunchPlan.Provider.Permission — the
	// engine never hand-threads permission.
	plan, planErr := agentlaunch.PlanFromLaunch(agentlaunch.PlanFromLaunchInput{
		Spec:    cat.Spec,
		Bag:     bag,
		Render:  rr,
		Runtime: binding,
		Agent:   agentlaunch.AgentSpec{ID: bag.Name, Name: display},
		Mode:    mode,
	})
	if planErr != nil {
		return nil, nil, fmt.Errorf("assemble launch plan: %w", planErr)
	}

	// PlanFromLaunch sets Workspace.Workdir but not Workspace.WorkspaceDir.
	// An interactive launch operates on the existing repo at work_dir, so
	// the session workspace IS the work dir — overlay it onto the bridge's
	// base plan (the bridge godoc explicitly allows the consumer to do so).
	// The preparer requires WorkspaceDir for shared/persistent modes.
	if plan.Workspace.WorkspaceDir == "" {
		plan.Workspace.WorkspaceDir = plan.Workspace.Workdir
	}

	ctx := context.Background()

	// 3. Compile.
	compiled, cErr := launcher.Compile(ctx, plan)
	if cErr != nil {
		return nil, nil, fmt.Errorf("compile: %w", cErr)
	}

	// 4. Prepare + Plant — materializes the boot dir.
	prepared, pErr := providerplant.PrepareAndPlant(ctx, compiled)
	if pErr != nil {
		return nil, nil, fmt.Errorf("prepare and plant: %w", pErr)
	}

	// 5. Resolve the runnable command.
	sl, sErr := sessionshim.ToSessionLaunch(prepared)
	if sErr != nil {
		return nil, nil, fmt.Errorf("to session launch: %w", sErr)
	}

	return &Ready{
		Status:  "ready",
		Binary:  sl.Binary,
		Args:    append([]string(nil), sl.Options.ExtraArgs...),
		Env:     envMap(sl.Options.Env),
		Workdir: sl.Options.Workdir,
	}, nil, nil
}

// effectiveInputs returns the bag's inputs merged onto the LaunchSpec's
// declared defaults — the base value bag a launch renders against.
func effectiveInputs(spec agentlaunch.LaunchSpec, bag agentlaunch.LaunchBag) map[string]any {
	out := make(map[string]any, len(spec.Inputs))
	for i := range spec.Inputs {
		if spec.Inputs[i].Default != nil {
			out[spec.Inputs[i].Name] = spec.Inputs[i].Default
		}
	}
	for k, v := range bag.Inputs {
		out[k] = v
	}
	return out
}

// declaresInput reports whether the LaunchSpec declares an input named
// key.
func declaresInput(spec agentlaunch.LaunchSpec, key string) bool {
	for i := range spec.Inputs {
		if spec.Inputs[i].Name == key {
			return true
		}
	}
	return false
}

// stringInput reads a string-shaped input value from the bag.
func stringInput(inputs map[string]any, name string) string {
	return toStr(inputs[name])
}

// expandPath expands a leading ~ to the user's home directory and
// returns the absolute path. Empty passes through.
func expandPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", p, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", p, err)
	}
	return abs, nil
}

// envMap converts the sorted "K=V" slice form back into a map for the
// JSON contract.
func envMap(kv []string) map[string]string {
	out := make(map[string]string, len(kv))
	for _, e := range kv {
		k, v, found := strings.Cut(e, "=")
		if found {
			out[k] = v
		}
	}
	return out
}
