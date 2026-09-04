package binding

import (
	"errors"
	"path"
	"path/filepath"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// dirName is bindings/ — this package's directory of one file per binding,
// within a bundle root. It is the same name internal/bundle's own
// (unexported) dirBindings enumerates for its read-only artifact-tree
// listing, spelled independently there on purpose: internal/bundle's copy
// never interprets a binding's content (profile/scope, alias resolution),
// it only lists filenames. This package's [Open] is what everything that
// DOES interpret binding content — CRUD, alias resolution, [Service],
// internal/launch — is required to go through, so this is the one place
// that spells the name for that purpose (T24 — CW-20260904-0003 — closed
// the one caller, internal/launch, that used to keep its own copy; see
// [Open]'s doc).
const dirName = "bindings"

// Open returns a [Store] over the active bindings/ directory under
// bundleRoot, without the CRUD surface [Service] binds to JS (List/Create/
// Update/Delete). It is the one way anything outside this package may read
// or write bindings directly: [Service.open] itself calls it too, and
// internal/launch resolves a binding through it rather than keeping a copy
// of [dirName] or the per-file naming convention of its own.
func Open(bundleRoot string) Store {
	return NewFileStore(filepath.Join(bundleRoot, dirName))
}

// BindingRelPath returns the bundle-relative, slash-separated path a
// binding named id lands at under bindings/ — "bindings/<id>.yaml".
// Exported so internal/skeleton's scaffold can point a new binding at the
// right place without re-deriving [dirName] or [bindingExt] itself, the
// same "exactly one place knows the storage format" discipline [Open]
// keeps for reads and writes.
func BindingRelPath(id string) string {
	return path.Join(dirName, id+bindingExt)
}

// Service is bound to the frontend as a Wails service: the palette's
// surface to the active bundle's bindings. Its exported methods are
// callable from JavaScript as
// "github.com/hollis-labs/tachyon/internal/binding.Service.<Method>".
//
// Like internal/manager.Service, it holds a [bundle.RootStore] rather than a
// fixed path or an already-open [Store]: every call resolves the active
// bundle root fresh and opens a new [FileStore] over it, so the same bundle
// root the manager reads is the one bindings are read from, and a root
// change (or an external edit to the bundle's bindings/ directory) is
// visible on the very next call.
type Service struct {
	store bundle.RootStore
}

// NewService returns a Service that reads and writes the active bundle's
// bindings, in whichever bundle store.Resolve() names.
func NewService(store bundle.RootStore) *Service {
	return &Service{store: store}
}

func (s *Service) open() (Store, error) {
	root, err := s.store.Resolve()
	if err != nil {
		return nil, err
	}
	return Open(root), nil
}

// ListResult is what [Service.List] returns: the three states the task
// record's own acceptance table requires the palette be able to tell
// apart, since a bare []Binding (or a bare error) cannot distinguish
// "genuinely no bindings yet" from "the bundle root looks wrong" from "the
// bindings are there but broken." State is one of:
//
//   - "ok": Bindings is the real list (possibly empty because there are
//     genuinely none yet — a bundle whose bindings/ exists but has no
//     entries in it).
//   - "missing": bindings/ does not exist under Path at all — the bundle
//     root is wrong, unset, or simply has never had a binding created in
//     it through Cairn. Bindings is nil.
//   - "unreadable": bindings/ exists but could not be fully read — Detail
//     names what went wrong (a file that is not a directory where
//     bindings/ should be, or a specific *.yaml file this package's scan
//     could not parse). Bindings is nil.
//
// Path is always populated, so a caller can name it regardless of State.
type ListResult struct {
	Bindings []Binding `json:"bindings"`
	State    string    `json:"state"`
	Path     string    `json:"path"`
	Detail   string    `json:"detail,omitempty"`
}

// List returns every binding in the active bundle, sorted by name, Scope
// always resolved to a path — wrapped in a [ListResult] that also reports
// which of the three states above produced it. The returned error is
// non-nil only when the active bundle root itself could not even be
// resolved (e.g. no bundle root configured and no default reachable) —
// every case where a bundle root resolved but its bindings/ directory did
// not (or could not be read) is reported through ListResult.State instead,
// with err == nil, so the frontend does not have to parse an error string
// to tell the three states apart. See doc.go.
func (s *Service) List() (ListResult, error) {
	root, err := s.store.Resolve()
	if err != nil {
		return ListResult{}, err
	}
	dir := filepath.Join(root, dirName)
	list, err := Open(root).List()
	switch {
	case err == nil:
		return ListResult{Bindings: list, State: "ok", Path: dir}, nil
	case errors.Is(err, ErrBindingsDirMissing):
		return ListResult{State: "missing", Path: dir, Detail: err.Error()}, nil
	default:
		return ListResult{State: "unreadable", Path: dir, Detail: err.Error()}, nil
	}
}

// Create adds a new binding and returns it as stored.
func (s *Service) Create(b Binding) (Binding, error) {
	st, err := s.open()
	if err != nil {
		return Binding{}, err
	}
	if err := st.Create(b); err != nil {
		return Binding{}, err
	}
	return st.Get(b.Name)
}

// Update replaces an existing binding's profile and scope, and returns it as
// stored.
func (s *Service) Update(b Binding) (Binding, error) {
	st, err := s.open()
	if err != nil {
		return Binding{}, err
	}
	if err := st.Update(b); err != nil {
		return Binding{}, err
	}
	return st.Get(b.Name)
}

// Delete removes a binding by name.
func (s *Service) Delete(name string) error {
	st, err := s.open()
	if err != nil {
		return err
	}
	return st.Delete(name)
}
