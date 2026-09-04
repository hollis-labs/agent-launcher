package binding

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// bindingExt is the one extension a binding file carries. Every method that
// turns a name into a path goes through [FileStore.bindingPath], so this is
// the only place it is spelled.
const bindingExt = ".yaml"

// FileStore is the real, live [Store]: one file per binding under Dir
// (bindings/ within a bundle root — see [Open]), aliases resolved from
// scopes.yaml beside it. It caches nothing — every call re-reads from
// disk — matching internal/bundle.Bundle and internal/manager.Service's
// identical choice, and for the same reason: the user (or Cairn, or a git
// checkout) can change the tree between calls, and there is no watcher to
// tell this package so.
type FileStore struct {
	// Dir is the bindings/ directory itself, not the bundle root — see
	// [Open], which is how everything outside this package is expected to
	// construct one. scopes.yaml is read from Dir's parent directory (see
	// aliases.go): a [FileStore] built directly, as tests do, should point
	// Dir at "<some bundle root>/bindings" for that to resolve correctly.
	Dir string
}

// NewFileStore returns a [Store] backed by the bindings/ directory at dir.
func NewFileStore(dir string) *FileStore { return &FileStore{Dir: dir} }

var _ Store = (*FileStore)(nil)

func (s *FileStore) scopesPath() string { return filepath.Join(filepath.Dir(s.Dir), scopesFileName) }
func (s *FileStore) bindingPath(name string) string {
	return filepath.Join(s.Dir, name+bindingExt)
}

// safeName reports whether name is safe to turn into a bindings/<name>.yaml
// path component. Create's own path always goes through [Binding.Validate]
// first, which checks the identical pattern; Get, Update and Delete take a
// bare name with no such gate above them, so each checks this directly
// before it ever touches the filesystem with it — see [nameRe]'s doc for
// why the pattern itself is what makes this safe.
func safeName(name string) bool { return nameRe.MatchString(name) }

// checkDir reports whether Dir exists and is a directory: nil when it is,
// [ErrBindingsDirMissing] (wrapped with the path) when Dir does not exist
// at all, and a different, plain wrapped error when something exists at
// Dir but is not a directory. Those last two are deliberately never
// conflated — "missing" and "unreadable" are two different states the
// three-way disambiguation this package's List/Get now support relies on
// keeping apart. See doc.go.
func (s *FileStore) checkDir() error {
	info, err := os.Stat(s.Dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrBindingsDirMissing, s.Dir)
		}
		return fmt.Errorf("binding: statting %s: %w", s.Dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("binding: %s exists but is not a directory", s.Dir)
	}
	return nil
}

// List returns every binding bindings/ recognizes, sorted by name, Scope
// always resolved to a path.
//
// It fails — rather than silently omitting the offending entry — if Dir
// does not exist ([ErrBindingsDirMissing]), if something other than a
// directory occupies Dir, if scopes.yaml exists but cannot be parsed, or if
// any *.yaml file directly under Dir is not in the shape [scanBindingFile]
// recognizes. See doc.go's "shape and existence only, now failed loud"
// section for why one bad file fails the whole call instead of just being
// left out of the result.
func (s *FileStore) List() ([]Binding, error) {
	if err := s.checkDir(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("binding: reading %s: %w", s.Dir, err)
	}
	aliases, err := s.loadAliases()
	if err != nil {
		return nil, err
	}

	out := make([]Binding, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			continue // a stray subdirectory: not a binding, skip quietly
		}
		if !strings.HasSuffix(name, bindingExt) {
			continue // e.g. README.md — not a binding file
		}
		id := strings.TrimSuffix(name, bindingExt)
		path := filepath.Join(s.Dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("binding: reading %s: %w", path, err)
		}
		profile, scope, perr := scanBindingFile(data)
		if perr != nil {
			return nil, fmt.Errorf("binding: %s: %w", path, perr)
		}
		out = append(out, Binding{Name: id, Profile: profile.value, Scope: resolveScope(scope.value, aliases)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns one binding by name.
//
// Unlike List, Get only ever reads bindings/<name>.yaml itself (plus
// scopes.yaml, to resolve its scope): a different, unrelated file under
// bindings/ being unreadable does not stop Get from returning a binding
// that is itself perfectly fine. See doc.go.
func (s *FileStore) Get(name string) (Binding, error) {
	if !safeName(name) {
		return Binding{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	path := s.bindingPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if derr := s.checkDir(); derr != nil {
				return Binding{}, derr
			}
			return Binding{}, fmt.Errorf("%w: %q", ErrNotFound, name)
		}
		return Binding{}, fmt.Errorf("binding: reading %s: %w", path, err)
	}
	profile, scope, perr := scanBindingFile(data)
	if perr != nil {
		return Binding{}, fmt.Errorf("binding: %s: %w", path, perr)
	}
	aliases, aerr := s.loadAliases()
	if aerr != nil {
		return Binding{}, aerr
	}
	return Binding{Name: name, Profile: profile.value, Scope: resolveScope(scope.value, aliases)}, nil
}

// Create adds a new binding as a brand-new file, bindings/<name>.yaml,
// written whole — there is no existing content to preserve for a create
// (D5's one exception; see doc.go). Dir (and, transitively, the bundle
// root, if it did not already have a bindings/ directory) is created as
// needed: a bundle that has never saved a binding through this package has
// no bindings/ at all, and Create's job on that bundle is to make one for
// the first time.
func (s *FileStore) Create(b Binding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	aliases, err := s.loadAliases()
	if err != nil {
		return err
	}
	if err := rejectAliasScope(aliases, b.Scope); err != nil {
		return err
	}

	path := s.bindingPath(b.Name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %q", ErrExists, b.Name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("binding: checking %s: %w", path, err)
	}

	return atomicWrite(path, []byte(renderBindingFile(b)))
}

// Update replaces an existing binding's Profile and/or Scope. Calling it
// with the binding's own current values is a no-op: nothing is written, not
// even the file's own bytes.
//
// Only the specific value token(s) that actually changed are replaced,
// via [splice]; everything else in the file — a leading comment block
// included — is carried through unread and unwritten. See doc.go.
func (s *FileStore) Update(b Binding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if !safeName(b.Name) {
		return fmt.Errorf("%w: %q", ErrNotFound, b.Name)
	}
	path := s.bindingPath(b.Name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if derr := s.checkDir(); derr != nil {
				return derr
			}
			return fmt.Errorf("%w: %q", ErrNotFound, b.Name)
		}
		return fmt.Errorf("binding: reading %s: %w", path, err)
	}
	profile, scope, perr := scanBindingFile(data)
	if perr != nil {
		return fmt.Errorf("binding: %s: %w", path, perr)
	}
	aliases, aerr := s.loadAliases()
	if aerr != nil {
		return aerr
	}
	current := Binding{Name: b.Name, Profile: profile.value, Scope: resolveScope(scope.value, aliases)}
	if current == b {
		return nil
	}

	var spans []spliceSpan
	if b.Profile != current.Profile {
		spans = append(spans, spliceSpan{Start: profile.start, End: profile.end, Text: renderScalar(b.Profile)})
	}
	if b.Scope != current.Scope {
		if err := rejectAliasScope(aliases, b.Scope); err != nil {
			return err
		}
		spans = append(spans, spliceSpan{Start: scope.start, End: scope.end, Text: renderScalar(b.Scope)})
	}
	if len(spans) == 0 {
		return nil
	}

	out := splice(data, spans)
	return atomicWrite(path, out)
}

// Delete removes a binding's whole file. Nothing else under Dir is
// touched — a Create is a whole-file write and a Delete is its exact
// mirror, a whole-file removal.
func (s *FileStore) Delete(name string) error {
	if !safeName(name) {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	path := s.bindingPath(name)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if derr := s.checkDir(); derr != nil {
				return derr
			}
			return fmt.Errorf("%w: %q", ErrNotFound, name)
		}
		return fmt.Errorf("binding: checking %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("binding: removing %s: %w", path, err)
	}
	return nil
}

// renderBindingFile renders a brand-new binding file's entire content:
// "profile: <profile>\nscope: <scope>\n", nothing else — matching
// bindings/README.md's own documented shape and every hand-written binding
// file that carries no extra fields.
func renderBindingFile(b Binding) string {
	return fmt.Sprintf("profile: %s\nscope: %s\n", renderScalar(b.Profile), renderScalar(b.Scope))
}

// renderScalar renders v as a plain YAML scalar when that is safe, and as a
// single-quoted scalar (embedded quotes doubled, YAML's own escaping rule)
// otherwise.
func renderScalar(v string) string {
	if isSafePlainScalar(v) {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func isSafePlainScalar(v string) bool {
	if v == "" || strings.TrimSpace(v) != v {
		return false
	}
	switch v {
	case "null", "Null", "NULL", "~", "true", "false", "True", "False", "yes", "no", "Yes", "No":
		return false // would not read back as this literal string
	}
	for _, r := range v {
		switch r {
		case ',', '{', '}', '[', ']', '#', '\'', '"', ':', '\n', '\t':
			return false
		}
	}
	return true
}

// atomicWrite replaces path's contents with data: a temp file in the same
// directory, then an os.Rename, so a crash or a concurrent read mid-write
// never observes a half-written file. Its directory is created first if
// needed — this is what lets [FileStore.Create] bring bindings/ (and a
// never-before-used bundle root's very first binding) into existence.
// Mirrors internal/bundle's identical pattern in Bundle.Write and
// RootStore.Save.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("binding: creating %s: %w", dir, err)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".tachyon-binding-*.tmp")
	if err != nil {
		return fmt.Errorf("binding: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("binding: writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("binding: closing %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("binding: setting mode on %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("binding: replacing %s: %w", path, err)
	}
	ok = true
	return nil
}
