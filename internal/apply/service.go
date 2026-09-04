package apply

import (
	"context"
	"time"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// applyTimeout bounds one whole [Service.Apply] call. Generous relative to
// what three local rsyncs actually take, the same "cannot hang forever"
// reasoning internal/shell's bootSweepTimeout applies to its own
// filesystem-and-subprocess operation.
const applyTimeout = 2 * time.Minute

// Options configures a [Service]. The zero value is real behavior: the
// real AGENTS_HOME ([ResolveAgentsHome]) and a real `make install-system`
// runner ([ExecRunner]). A test overrides AgentsHome and/or Runner so it
// never touches the real ~/.config/agents or shells out for real — the
// same shape internal/shell.Config's PrefsPath/BundleRootStorePath fields
// already use for the identical reason.
type Options struct {
	// AgentsHome overrides how the real AGENTS_HOME is resolved. Nil means
	// [ResolveAgentsHome]. A test sets this to point [Service] at a scratch
	// directory instead of the real ~/.config/agents.
	AgentsHome func() (string, error)
	// Runner overrides how `make install-system` is actually run. Nil
	// means [ExecRunner]. A test sets this to a fake so it never shells out
	// to the real make binary.
	Runner Runner
}

// Service is bound to the frontend as a Wails service: the manager's Apply
// action. Its exported methods are callable from JavaScript as
// "github.com/hollis-labs/tachyon/internal/apply.Service.<Method>".
//
// Like internal/manager.Service, internal/binding.Service and
// internal/launch.Service, it holds a [bundle.RootStore] rather than a
// fixed path: every call resolves the active bundle root fresh, so Apply
// always acts on whichever bundle the manager is currently showing.
type Service struct {
	store      bundle.RootStore
	agentsHome func() (string, error)
	runner     Runner
}

// NewService returns a Service that resolves the bundle root from store,
// AGENTS_HOME from opts.AgentsHome (or [ResolveAgentsHome]), and runs
// `make install-system` via opts.Runner (or [ExecRunner]).
func NewService(store bundle.RootStore, opts Options) *Service {
	agentsHome := opts.AgentsHome
	if agentsHome == nil {
		agentsHome = ResolveAgentsHome
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner()
	}
	return &Service{store: store, agentsHome: agentsHome, runner: runner}
}

func (s *Service) resolve() (bundleRoot, agentsHome string, err error) {
	bundleRoot, err = s.store.Resolve()
	if err != nil {
		return "", "", err
	}
	agentsHome, err = s.agentsHome()
	if err != nil {
		return "", "", err
	}
	return bundleRoot, agentsHome, nil
}

// Status reports how the active bundle and its installed layer currently
// differ — the enablement check the manager's Apply button reads, and the
// same [Summary] its confirmation dialog is built from (CW-20260904-0023:
// "reuse the same comparison's summary"). It is read-only; nothing here
// changes anything on disk. Called fresh every time — nothing here caches
// across calls, matching internal/manager.Service's own "nothing is
// cached" discipline, for the identical reason: this has to be right again
// after every Save, after every Apply, and whenever the manager regains
// focus, not just once.
func (s *Service) Status() (Summary, error) {
	bundleRoot, agentsHome, err := s.resolve()
	if err != nil {
		return Summary{}, err
	}
	return Compare(bundleRoot, agentsHome)
}

// Apply runs `make install-system` in the active bundle root, staging
// templates/, skills/ and prompts/ into AGENTS_HOME. This is the one
// method in the whole app that actually runs it — see the package doc's
// "staging is deliberate, not implicit" and this method's own callers in
// Manager.jsx's ApplyBar: reachable only from the explicit, user-clicked
// Apply button, after a confirmation.
//
// A non-zero exit from make — including a missing install-system target —
// surfaces as an *[InvokeError] carrying make's own output; nothing here
// falls back to a hand-rolled copy.
func (s *Service) Apply() (Result, error) {
	bundleRoot, agentsHome, err := s.resolve()
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()
	return Invoke(ctx, s.runner, bundleRoot, agentsHome)
}
