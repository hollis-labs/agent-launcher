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

// FileStore is the real, live [Store]: the bundle's one bindings file
// ([fileName]) at Path, edited surgically. It caches nothing — every call re-reads the file from disk —
// matching internal/bundle.Bundle and internal/manager.Service's identical
// choice, and for the same reason: the user (or Cairn, or a git checkout)
// can change the file between calls, and there is no watcher to tell this
// package so.
type FileStore struct {
	// Path is the bindings file. It need not exist yet: List and Get
	// then behave as though it were empty, and Create writes it fresh.
	Path string
}

// NewFileStore returns a [Store] backed by the bindings file at path.
func NewFileStore(path string) *FileStore { return &FileStore{Path: path} }

var _ Store = (*FileStore)(nil)

// load reads and scans the file. A missing file is not an error — see
// [FileStore.Path] — everything else reading it is.
func (s *FileStore) load() (*document, []byte, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return &document{aliases: map[string]string{}, bindingsHeaderEnd: -1}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("binding: reading %s: %w", s.Path, err)
	}
	return parseDocument(data), data, nil
}

// List returns every binding the scan recognizes, sorted by name, Scope
// always resolved to a path.
func (s *FileStore) List() ([]Binding, error) {
	doc, _, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(doc.entries))
	for _, e := range doc.entries {
		out = append(out, doc.binding(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns one binding by name.
func (s *FileStore) Get(name string) (Binding, error) {
	doc, _, err := s.load()
	if err != nil {
		return Binding{}, err
	}
	e, ok := doc.find(name)
	if !ok {
		return Binding{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return doc.binding(e), nil
}

// Create adds a new binding, appended after the last existing entry in
// bindings: (or, if the file has no bindings: section yet — including a file
// that does not exist at all — by adding one). It never touches any other
// byte in the file.
func (s *FileStore) Create(b Binding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	doc, data, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := doc.find(b.Name); ok {
		return fmt.Errorf("%w: %q", ErrExists, b.Name)
	}
	if err := rejectAliasScope(doc, b.Scope); err != nil {
		return err
	}

	newLine := renderEntryLine(b)

	if doc.bindingsHeaderEnd < 0 {
		// No bindings: section exists — including a file that is entirely
		// absent. Add one, but touch nothing else: existing content (if any)
		// is carried through byte-for-byte before it.
		var out []byte
		out = append(out, data...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		if len(out) > 0 {
			out = append(out, '\n')
		}
		out = append(out, []byte("bindings:\n")...)
		out = append(out, newLine...)
		return atomicWrite(s.Path, out)
	}

	text := newLine
	if doc.insertNeedsLeadingNewline {
		text = "\n" + text
	}
	out := splice(data, []spliceSpan{{Start: doc.insertAt, End: doc.insertAt, Text: text}})
	return atomicWrite(s.Path, out)
}

// Update replaces an existing binding's Profile and/or Scope. Calling it with
// the binding's own current values is a no-op: nothing is written, not even
// the entry's own bytes — see the package doc.
//
// Only the specific value token(s) that actually changed are replaced;
// everything else on the line, and every other line in the file, is carried
// through unread and unwritten.
func (s *FileStore) Update(b Binding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	doc, data, err := s.load()
	if err != nil {
		return err
	}
	e, ok := doc.find(b.Name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, b.Name)
	}
	current := doc.binding(e)
	if current == b {
		return nil
	}

	var spans []spliceSpan
	if b.Profile != current.Profile {
		spans = append(spans, spliceSpan{Start: e.profile.start, End: e.profile.end, Text: renderScalar(b.Profile)})
	}
	if b.Scope != current.Scope {
		if err := rejectAliasScope(doc, b.Scope); err != nil {
			return err
		}
		spans = append(spans, spliceSpan{Start: e.scope.start, End: e.scope.end, Text: renderScalar(b.Scope)})
	}
	if len(spans) == 0 {
		// Name matched, Profile and Scope compare equal, but current != b
		// only if a future field differs — nothing to do today.
		return nil
	}

	out := splice(data, spans)
	return atomicWrite(s.Path, out)
}

// Delete removes a binding's entire line — indentation and trailing newline
// included — and nothing else. It does not remove a comment or blank line
// that happened to sit next to it: those may describe or separate other
// entries, and this package does not parse comments well enough to know.
func (s *FileStore) Delete(name string) error {
	doc, data, err := s.load()
	if err != nil {
		return err
	}
	e, ok := doc.find(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	out := splice(data, []spliceSpan{{Start: e.lineStart, End: e.lineEnd, Text: ""}})
	return atomicWrite(s.Path, out)
}

// rejectAliasScope refuses a Scope value that is actually one of the file's
// own scopes: keys — a caller (or a UI above this package) trying to save an
// alias name where a path belongs, which the package doc's fence forbids.
func rejectAliasScope(doc *document, scope string) error {
	if _, isAlias := doc.aliases[scope]; isAlias {
		return fmt.Errorf("binding: scope %q is a scopes: alias key, not a path — pass the literal path it resolves to instead", scope)
	}
	return nil
}

// renderEntryLine formats a brand-new bindings: entry in the same
// two-space-indented, single-line flow-map style every existing entry
// uses: "  <name>: { profile: <profile>, scope: <scope> }\n". It does not
// attempt the neighbors' column alignment padding — that is cosmetic polish
// on entries this package did not write, and reproducing it would mean
// parsing it, which the read path deliberately does not do.
func renderEntryLine(b Binding) string {
	return fmt.Sprintf("  %s: { profile: %s, scope: %s }\n", b.Name, renderScalar(b.Profile), renderScalar(b.Scope))
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
// never observes a half-written file. Mirrors internal/bundle's identical
// pattern in Bundle.Write and RootStore.Save; this package does not import
// bundle for it because the bindings file is not one of the six kinds bundle
// enumerates (see the package doc) — there is no [bundle.Ref] for it to
// resolve.
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
