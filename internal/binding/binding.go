package binding

import (
	"errors"
	"fmt"
	"regexp"
)

// Binding is one named {profile, scope} pair. Scope is always a path —
// absolute or "~/"-relative — never an alias name. See the package doc.
type Binding struct {
	// Name is the binding's id: the map key in bindings.yaml, and the
	// argument `cairn boot <name>` takes.
	Name string `json:"name"`
	// Profile is the boot target: a profile id, resolved the same way a bare
	// `cairn boot <profile>` would resolve it. This package does not check
	// that the profile exists (D8).
	Profile string `json:"profile"`
	// Scope is the default working directory, already resolved to a path if
	// the on-disk entry named an alias. Never an alias name.
	Scope string `json:"scope"`
}

// ErrNotFound is returned by Get, Update and Delete when no binding of that
// name exists. Test for it with errors.Is.
var ErrNotFound = errors.New("binding: not found")

// ErrExists is returned by Create when a binding of that name already
// exists. Test for it with errors.Is.
var ErrExists = errors.New("binding: already exists")

// nameRe is what a binding's Name must match: the same plain-scalar,
// unquoted style every existing key in bindings.yaml already uses. This is
// deliberately stricter than YAML allows, because a name accepted here is
// written back as a bare map key with no quoting or escaping.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Validate reports whether b can be written at all: a well-formed name, and
// non-empty Profile and Scope. It does not check that Profile names a real
// profile or that Scope exists on disk (D8) — only that the values are
// structurally safe to place in bindings.yaml's flow-map style.
func (b Binding) Validate() error {
	if !nameRe.MatchString(b.Name) {
		return fmt.Errorf("binding: invalid name %q: must match %s", b.Name, nameRe.String())
	}
	if b.Profile == "" {
		return errors.New("binding: profile is required")
	}
	if b.Scope == "" {
		return errors.New("binding: scope is required")
	}
	return nil
}

// Store is load, list, write — the whole surface anything above this package
// is allowed to program against. A [Binding] in, a [Binding] out; nothing
// above Store may know whether it is backed by one YAML file, one file per
// binding, or anything else. [FileStore] is the real, live implementation;
// [MemStore] is a second, deliberately trivial one, and contract_test.go
// proves both satisfy the same behavioral contract.
type Store interface {
	// List returns every binding, sorted by name. Scope is always a path.
	List() ([]Binding, error)
	// Get returns one binding by name, or [ErrNotFound].
	Get(name string) (Binding, error)
	// Create adds a new binding. It returns [ErrExists] if the name is
	// already taken, and rejects a Scope that names a live alias key rather
	// than a path (see [FileStore]'s documentation on that check).
	Create(b Binding) error
	// Update replaces an existing binding's Profile and Scope. The name is
	// fixed — Update does not rename; delete and re-create for that. It
	// returns [ErrNotFound] if the name does not exist.
	//
	// Calling Update with a binding's own current values is a no-op: nothing
	// is written, which for [FileStore] means the file is not touched at all.
	Update(b Binding) error
	// Delete removes a binding by name. It returns [ErrNotFound] if the name
	// does not exist.
	Delete(name string) error
}
