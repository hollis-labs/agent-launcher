package binding

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// scopesFileName is scopes.yaml — the alias map, a single file sitting
// beside bindings/ at the bundle root, not inside it. Its own header
// comment in the live bundle says it is "on its way out"; until then this
// is the one place this package goes to resolve an alias. See resolve.go
// and doc.go's "scope is always a path" section.
const scopesFileName = "scopes.yaml"

// loadAliases reads and parses scopes.yaml for [FileStore]. A missing file
// is not an error — scopes.yaml is optional, and its own absence just means
// every binding's raw scope is already a literal path with nothing to
// resolve (see [parseScopesFile]'s doc for the file's shape).
func (s *FileStore) loadAliases() (map[string]string, error) {
	path := s.scopesPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("binding: reading %s: %w", path, err)
	}
	return parseScopesFile(data), nil
}

// parseScopesFile parses scopes.yaml's own documented shape: flat,
// unindented "key:   value" lines, one alias per line, no section header
// and no flow maps — a much smaller shape than a binding file's own, let
// alone the old shared file's nested scopes: section. A line this narrow
// scan does not recognize (blank, a comment, or any other shape) is simply
// not added to the alias map: this package never writes scopes.yaml, so
// there is no round-trip to protect here, only enough of a read to resolve
// a scope token — see resolve.go.
func parseScopesFile(data []byte) map[string]string {
	aliases := map[string]string{}
	for _, ln := range splitLines(data) {
		trimmed := strings.TrimRight(ln.text, "\r\n")
		if trimmed == "" || trimmed[0] == '#' || trimmed[0] == ' ' || trimmed[0] == '\t' {
			continue
		}
		ci := strings.IndexByte(trimmed, ':')
		if ci <= 0 {
			continue
		}
		key := trimmed[:ci]
		if !isPlainKeyToken(key) {
			continue
		}
		value := strings.TrimSpace(trimmed[ci+1:])
		if value == "" {
			continue
		}
		aliases[key] = value
	}
	return aliases
}

// rejectAliasScope refuses a Scope value that is actually one of
// scopes.yaml's own keys — a caller (or a UI above this package) trying to
// save an alias name where a path belongs, which the package doc's fence
// forbids.
func rejectAliasScope(aliases map[string]string, scope string) error {
	if _, isAlias := aliases[scope]; isAlias {
		return fmt.Errorf("binding: scope %q is a scopes.yaml alias key, not a path — pass the literal path it resolves to instead", scope)
	}
	return nil
}
