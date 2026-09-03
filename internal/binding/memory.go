package binding

import (
	"fmt"
	"sort"
	"sync"
)

// MemStore is a trivial, in-memory [Store]: a name-keyed map behind a mutex,
// nothing else — no file, no format, no alias resolution (there is nothing
// to resolve; a caller of this package only ever hands it a path). It exists
// to prove the [Store] interface boundary is real: contract_test.go runs one
// shared behavioral test suite against this and against [FileStore], which
// is what "swapping the storage format is a one-file change" means in
// practice — swap which Store a caller constructs — rather than merely
// asserting it. It is not a production storage option.
type MemStore struct {
	mu   sync.Mutex
	data map[string]Binding
}

// NewMemStore returns a MemStore seeded with the given bindings (copied, not
// aliased — later changes to seed do not affect the store). A nil or empty
// seed starts empty.
func NewMemStore(seed []Binding) *MemStore {
	m := &MemStore{data: make(map[string]Binding, len(seed))}
	for _, b := range seed {
		m.data[b.Name] = b
	}
	return m
}

var _ Store = (*MemStore)(nil)

func (m *MemStore) List() ([]Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Binding, 0, len(m.data))
	for _, b := range m.data {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemStore) Get(name string) (Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[name]
	if !ok {
		return Binding{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return b, nil
}

func (m *MemStore) Create(b Binding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[b.Name]; ok {
		return fmt.Errorf("%w: %q", ErrExists, b.Name)
	}
	m.data[b.Name] = b
	return nil
}

func (m *MemStore) Update(b Binding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[b.Name]; !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, b.Name)
	}
	m.data[b.Name] = b
	return nil
}

func (m *MemStore) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[name]; !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	delete(m.data, name)
	return nil
}
