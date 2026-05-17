// Package launch wires the tachyon-engine subcommands onto the
// go-agent-launch public seam. It is the SOLE consumer of
// go-agent-launch in the Tachyon codebase.
package launch

import (
	"fmt"
	"sort"

	"github.com/hollis-labs/go-agent-launch/agentlaunch"
)

// runner.go bridges the S4.4 `runner` LaunchSpec input — an open token
// (claude-code, claude-pty, claude-tui, codex-cli, codex-app-server,
// opencode) — to a go-agent-launch RuntimeBinding.
//
// Tachyon is INTERACTIVE-ONLY (decision D-T2): every runner it advertises
// is an interactive runner. There is deliberately NO claude-stream /
// streaming-stdio runner — Torque owns autonomous/headless launches; the
// non-interactive claude path that could hang on an approval prompt is
// not reachable from Tachyon at all.
//
// The bridge is NOT a hand-rolled table any more. The corpus ships one
// runtime-binding contract per runner token under providers/ (see
// internal/corpus/providers/*.yaml); LoadCatalog ingests that subdir with
// the go-agent-launch file-backed registrar, and ResolveRunner resolves
// the token through agentlaunch.ResolveRuntimeBinding — the same
// directory-registry seam an online directory service would expose.
// Going through ResolveRuntimeBinding also means RuntimeBinding.Permission
// is carried onto LaunchPlan.Provider.Permission automatically by
// PlanFromLaunch; the engine never hand-threads permission.

// ResolveRunner resolves a `runner` input token to its RuntimeBinding by
// querying the catalog's runtime-binding registrar. An unknown token is a
// hard error — silently defaulting would launch the wrong provider.
func (c *Catalog) ResolveRunner(token string) (agentlaunch.RuntimeBinding, error) {
	if token == "" {
		return agentlaunch.RuntimeBinding{}, fmt.Errorf("runner token is empty")
	}
	binding, err := agentlaunch.ResolveRuntimeBinding(c.registrar, c.registrarDesc, token)
	if err != nil {
		return agentlaunch.RuntimeBinding{}, fmt.Errorf("unknown runner %q (known: %v): %w", token, c.KnownRunners(), err)
	}
	return binding, nil
}

// KnownRunners returns every runner token the corpus registers a
// runtime-binding for, sorted. This is the `runners` array in the
// `describe` contract. Every entry is an interactive runner (D-T2).
func (c *Catalog) KnownRunners() []string {
	out := make([]string, 0, len(c.runners))
	out = append(out, c.runners...)
	sort.Strings(out)
	return out
}

// launchModeForRuntime derives the launch Mode from a resolved runtime
// binding. Tachyon is interactive-only (D-T2): every runner the corpus
// ships is an interactive runner, so every launch is LaunchInteractive.
// The Mode is DERIVED here rather than hardcoded at the call site so the
// invariant is enforced in one place and a future non-interactive runner
// could not silently inherit the wrong mode.
func launchModeForRuntime(binding agentlaunch.RuntimeBinding) (agentlaunch.LaunchMode, error) {
	switch binding.RuntimeKind {
	case agentlaunch.RuntimeSubprocess, agentlaunch.RuntimePTY, agentlaunch.RuntimeJsonRpcStdio:
		// The three interactive runtime shapes Tachyon ships. A human is
		// always attached; the session is long-lived and attach-enabled.
		return agentlaunch.LaunchInteractive, nil
	case agentlaunch.RuntimeStreamingStdio:
		// Streaming-stdio is the autonomous/headless shape Torque owns.
		// Tachyon ships no such runner; reaching here means the corpus
		// regressed the interactive-only decision (D-T2).
		return "", fmt.Errorf("runtime %q is non-interactive; Tachyon is interactive-only (D-T2)", binding.RuntimeKind)
	default:
		return "", fmt.Errorf("unknown runtime kind %q", binding.RuntimeKind)
	}
}
