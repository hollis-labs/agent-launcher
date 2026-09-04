package binding_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
)

// TestStoreContract is the acceptance proof that "swapping the storage
// format is a one-file change": one behavioral test suite, runContract,
// executed against two independent [binding.Store] implementations — the
// real bindings/-directory-backed [binding.FileStore] and the trivial
// in-memory [binding.MemStore]. Nothing in runContract knows or cares which
// one it was handed; only this function's two subtests differ, in exactly
// one line each — the constructor. That is the whole claim demonstrated,
// not merely asserted in a comment.
//
// The FileStore subtest's one setup line creates its bindings/ directory
// before handing it to runContract, unlike this test's old, single-shared-
// file-backed version (which relied on a missing file behaving exactly
// like an empty one). That is a deliberate, narrow adaptation to what "a
// fresh store" means for this [binding.Store] now that a genuinely absent
// bindings/ directory is its own, different, meaningful state — see
// [binding.ErrBindingsDirMissing] and doc.go's "the directory's own
// existence is real information" — not a change to runContract's own
// assertions, which are untouched: a *present, empty* directory is still
// required to behave exactly like [binding.MemStore]'s own fresh, empty
// map, which is what this subtest now sets up explicitly instead of
// leaving implicit.
func TestStoreContract(t *testing.T) {
	t.Run("FileStore", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bindings")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
		runContract(t, binding.NewFileStore(dir))
	})
	t.Run("MemStore", func(t *testing.T) {
		runContract(t, binding.NewMemStore(nil))
	})
}

// runContract exercises List, Get, Create, Update and Delete against a fresh,
// empty store of unknown concrete type.
func runContract(t *testing.T, s binding.Store) {
	t.Helper()

	// A fresh store starts empty.
	got, err := s.List()
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List (empty) = %+v; want empty", got)
	}
	if _, err := s.Get("nope"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Get on empty store error = %v; want ErrNotFound", err)
	}

	// Create.
	a := binding.Binding{Name: "alpha", Profile: "engineer", Scope: "~/dev/alpha"}
	b := binding.Binding{Name: "beta", Profile: "architect", Scope: "~/dev/beta"}
	if err := s.Create(a); err != nil {
		t.Fatalf("Create(alpha): %v", err)
	}
	if err := s.Create(b); err != nil {
		t.Fatalf("Create(beta): %v", err)
	}
	if err := s.Create(a); !errors.Is(err, binding.ErrExists) {
		t.Errorf("Create(alpha) again error = %v; want ErrExists", err)
	}

	// List — sorted by name, scope already a path in both implementations.
	got, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []binding.Binding{a, b}
	if len(got) != len(want) {
		t.Fatalf("List = %+v; want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List()[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}

	// Get.
	gotA, err := s.Get("alpha")
	if err != nil {
		t.Fatalf("Get(alpha): %v", err)
	}
	if gotA != a {
		t.Errorf("Get(alpha) = %+v; want %+v", gotA, a)
	}

	// Update.
	edited := binding.Binding{Name: "alpha", Profile: "director", Scope: "~/dev/alpha-2"}
	if err := s.Update(edited); err != nil {
		t.Fatalf("Update(alpha): %v", err)
	}
	gotA, err = s.Get("alpha")
	if err != nil {
		t.Fatalf("Get(alpha) after Update: %v", err)
	}
	if gotA != edited {
		t.Errorf("Get(alpha) after Update = %+v; want %+v", gotA, edited)
	}
	if err := s.Update(binding.Binding{Name: "ghost", Profile: "x", Scope: "~/dev/x"}); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Update(ghost) error = %v; want ErrNotFound", err)
	}

	// Delete.
	if err := s.Delete("beta"); err != nil {
		t.Fatalf("Delete(beta): %v", err)
	}
	if _, err := s.Get("beta"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Get(beta) after Delete = %v; want ErrNotFound", err)
	}
	if err := s.Delete("beta"); !errors.Is(err, binding.ErrNotFound) {
		t.Errorf("Delete(beta) again error = %v; want ErrNotFound", err)
	}

	got, err = s.List()
	if err != nil {
		t.Fatalf("List (final): %v", err)
	}
	if len(got) != 1 || got[0] != edited {
		t.Fatalf("List (final) = %+v; want [%+v]", got, edited)
	}
}
