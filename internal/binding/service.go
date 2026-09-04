package binding

import (
	"path/filepath"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// fileName is this package's one bindings file's name within a bundle root
// — not bundle.dirBindings, the bindings/ directory internal/bundle
// enumerates and this package deliberately never touches (see the package
// doc). It is the single place in Tachyon that spells out the on-disk
// name; everything else, in this package or any other, gets a [Store] from
// [Open] rather than building this path itself (T24 — CW-20260904-0003 —
// closed the one caller, internal/launch, that used to keep its own copy).
const fileName = "bindings.yaml"

// Open returns a [Store] over the active bindings file under bundleRoot,
// without the CRUD surface [Service] binds to JS (List/Create/Update/
// Delete). It is the one way anything outside this package may read or
// write bindings directly: [Service.open] itself now calls it too, so
// there is exactly one place, [fileName], that constructs this path.
func Open(bundleRoot string) Store {
	return NewFileStore(filepath.Join(bundleRoot, fileName))
}

// Service is bound to the frontend as a Wails service: the palette's surface
// to the active bundle's bindings. Its exported methods are callable from
// JavaScript as
// "github.com/hollis-labs/tachyon/internal/binding.Service.<Method>".
//
// Like internal/manager.Service, it holds a [bundle.RootStore] rather than a
// fixed path or an already-open [Store]: every call resolves the active
// bundle root fresh and opens a new [FileStore] over it, so the same bundle
// root the manager reads is the one bindings are read from, and a root
// change (or an external edit to the bindings file) is visible on the very
// next call.
type Service struct {
	store bundle.RootStore
}

// NewService returns a Service that reads and writes the active bundle's
// bindings file, in whichever bundle store.Resolve() names.
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

// List returns every binding in the active bundle, sorted by name, Scope
// always resolved to a path.
func (s *Service) List() ([]Binding, error) {
	st, err := s.open()
	if err != nil {
		return nil, err
	}
	return st.List()
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
