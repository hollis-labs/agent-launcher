package launchprofile

import (
	"errors"
	"fmt"

	"github.com/hollis-labs/tachyon/internal/state"
)

// Service is the frontend-facing surface: the launch-profile store, bound
// as a Wails service.
type Service struct {
	// dir overrides where profiles live. Empty means [state.LaunchDir].
	// Tests set it; the app does not.
	dir string
}

// NewService returns a Service over the default launch directory.
func NewService() *Service { return &Service{} }

// NewServiceAt returns a Service over dir. Tests use it so a run never
// touches the real ~/.config/tachyon/launch.
func NewServiceAt(dir string) *Service { return &Service{dir: dir} }

// store resolves the directory this service reads.
func (s *Service) store() (Store, error) {
	if s.dir != "" {
		return Open(s.dir), nil
	}
	dir, err := state.LaunchDir()
	if err != nil {
		return Store{}, fmt.Errorf("launchprofile: %w", err)
	}
	return Open(dir), nil
}

// ListResult is what [Service.List] returns: the three states the palette
// must be able to tell apart, since a bare []Profile cannot distinguish
// "genuinely none written yet" from "this build cannot read the directory."
// State is one of:
//
//   - "ok": Profiles is the real list, possibly empty because the
//     directory exists and holds none.
//   - "missing": the launch directory does not exist. Profiles is nil.
//     This is first run, and it is a normal state rather than a fault.
//   - "unreadable": the directory exists but could not be read — Detail
//     names what went wrong.
//
// Path is always populated, so a caller can name it in any state.
//
// This mirrors the shape internal/binding's own ListResult had, and for the
// reason recorded there: three distinguishable states existed and the UI
// rendered one unconditional sentence over all of them, which had Chrispian
// asking whether he was SUPPOSED to have bindings when the real answer was
// "this build can't read them." Same three states here, one of which is
// now genuinely the common one — a fresh machine has written no launch
// profile — so the copy for "missing" must invite rather than alarm.
type ListResult struct {
	Profiles []Profile `json:"profiles"`
	State    string    `json:"state"`
	Path     string    `json:"path"`
	Detail   string    `json:"detail,omitempty"`
}

// List returns every launch profile, sorted by name, wrapped in a
// [ListResult] that reports which of the three states produced it.
//
// The error return is non-nil only when the launch directory could not be
// resolved at all. Every case where it resolved but could not be read is
// reported through State, so the frontend never parses an error string to
// tell the three apart.
func (s *Service) List() (ListResult, error) {
	st, err := s.store()
	if err != nil {
		return ListResult{}, err
	}
	list, err := st.List()
	switch {
	case err == nil:
		return ListResult{Profiles: list, State: "ok", Path: st.Dir}, nil
	case errors.Is(err, ErrDirMissing):
		return ListResult{State: "missing", Path: st.Dir, Detail: err.Error()}, nil
	default:
		return ListResult{State: "unreadable", Path: st.Dir, Detail: err.Error()}, nil
	}
}

// Get returns one launch profile.
func (s *Service) Get(name string) (Profile, error) {
	st, err := s.store()
	if err != nil {
		return Profile{}, err
	}
	return st.Get(name)
}

// Read returns one launch profile's raw text, for the editor.
func (s *Service) Read(name string) (string, error) {
	st, err := s.store()
	if err != nil {
		return "", err
	}
	data, err := st.Read(name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Save writes a launch profile's text, creating it when absent.
func (s *Service) Save(name, content string) (Profile, error) {
	st, err := s.store()
	if err != nil {
		return Profile{}, err
	}
	return st.Save(name, []byte(content))
}

// Create writes a new launch profile from the scaffold, refusing a name
// that is already taken.
func (s *Service) Create(name, provider string) (Profile, error) {
	st, err := s.store()
	if err != nil {
		return Profile{}, err
	}
	return st.Create(name, Scaffold(name, provider))
}

// Dir reports where launch profiles live, so the manager can name it.
func (s *Service) Dir() (string, error) {
	st, err := s.store()
	if err != nil {
		return "", err
	}
	return st.Dir, nil
}
