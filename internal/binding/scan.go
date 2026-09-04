package binding

import (
	"fmt"
	"strings"
)

// line is one line of a file, as a byte-offset span into the original data
// (end includes the trailing '\n' when present) plus its own text.
type line struct {
	start, end int
	text       string
}

// splitLines splits data into lines without discarding anything: every byte
// of data is accounted for in exactly one line's [start, end) span, which is
// what lets every offset computed below be used directly against the
// original file bytes.
func splitLines(data []byte) []line {
	var lines []line
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, line{start: start, end: i + 1, text: string(data[start : i+1])})
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, line{start: start, end: len(data), text: string(data[start:])})
	}
	return lines
}

// fieldSpan is one "key: value" pair recognized in a binding file. start/end
// are absolute file offsets bounding the raw value token exactly as written
// (quotes included, if any) — what a surgical edit replaces.
type fieldSpan struct {
	name    string
	value   string // decoded
	start   int
	end     int
	present bool
}

// scanBindingFile scans one binding file's bytes — bindings/<name>.yaml,
// per bindings/README.md's own documented shape — for its top-level
// "profile:" and optional "scope:" scalar lines. Profile must appear exactly
// once; scope may appear once or be omitted, in which case its logical value
// is empty. Each present field is a bare, unindented "key:" line (a plain or
// quoted scalar value, possibly empty); anything else in the file — a leading comment block,
// blank lines, indentation, or any other top-level key this package does
// not recognize — is never read for meaning and never touched by an edit.
// See doc.go's "bytes in, bytes out" section.
//
// err is non-nil, naming why, when the file does not carry profile in this
// shape. That is deliberate: unlike the old shared file's per-line
// scan (which silently left an unrecognized line alone and moved on), one
// whole file is now one binding, so a file this scan cannot make sense of
// is surfaced as a real failure — see doc.go's "shape and existence only,
// now failed loud" section for why silently dropping it would be worse.
func scanBindingFile(data []byte) (profile, scope fieldSpan, err error) {
	var haveProfile, haveScope bool
	for _, ln := range splitLines(data) {
		trimmed := strings.TrimRight(ln.text, "\r\n")
		if trimmed == "" {
			continue
		}
		if trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '#' {
			continue // indented, or a comment: not a top-level key line
		}
		ci := strings.IndexByte(trimmed, ':')
		if ci <= 0 {
			continue
		}
		key := trimmed[:ci]
		if !isPlainKeyToken(key) {
			continue
		}
		if key != "profile" && key != "scope" {
			continue // some other top-level key: not this package's business (D8)
		}

		rest := trimmed[ci+1:]
		skip := len(rest) - len(strings.TrimLeft(rest, " \t"))
		valPart := rest[skip:]
		decoded, n, ok := scanScalarToken(valPart)
		if !ok {
			return fieldSpan{}, fieldSpan{}, fmt.Errorf("binding: line %q: %s: value is not a recognized scalar", trimmed, key)
		}
		start := ln.start + ci + 1 + skip
		fs := fieldSpan{name: key, value: decoded, start: start, end: start + n, present: true}

		switch key {
		case "profile":
			if haveProfile {
				return fieldSpan{}, fieldSpan{}, fmt.Errorf("binding: duplicate %q key", "profile")
			}
			profile, haveProfile = fs, true
		case "scope":
			if haveScope {
				return fieldSpan{}, fieldSpan{}, fmt.Errorf("binding: duplicate %q key", "scope")
			}
			scope, haveScope = fs, true
		}
	}
	if !haveProfile {
		return fieldSpan{}, fieldSpan{}, fmt.Errorf("binding: missing top-level profile: key")
	}
	return profile, scope, nil
}

// scanScalarToken parses the YAML scalar at the start of s — the rest of a
// "profile:" or "scope:" line after the colon and any leading horizontal
// whitespace, still carrying its own trailing whitespace (if any) up to
// (but not including) the line's own \r\n, already stripped by the caller.
//
// s == "" (nothing after the colon) is a valid, empty scalar — "profile:"
// with no value is a well-formed, if incomplete, binding file (see
// internal/skeleton's binding scaffold, which starts every new binding
// exactly this way). n is the length of the raw on-disk token this value
// occupies — what [FileStore.Update]'s splice replaces — which is not
// always len(s): a plain value's own trailing whitespace is not part of
// the token, and a quoted value's token ends at its closing quote even if
// more characters follow it in s.
func scanScalarToken(s string) (decoded string, n int, ok bool) {
	if s == "" {
		return "", 0, true
	}
	if s[0] == '\'' || s[0] == '"' {
		quote := s[0]
		i := 1
		for i < len(s) {
			switch {
			case quote == '\'' && s[i] == '\'':
				if i+1 < len(s) && s[i+1] == '\'' {
					i += 2
					continue
				}
				i++
				raw := s[:i]
				decoded, dok := decodeScalar(raw)
				return decoded, i, dok
			case quote == '"' && s[i] == '\\' && i+1 < len(s):
				i += 2
				continue
			case quote == '"' && s[i] == '"':
				i++
				raw := s[:i]
				decoded, dok := decodeScalar(raw)
				return decoded, i, dok
			default:
				i++
			}
		}
		return "", 0, false // unterminated quote
	}
	trimmed := strings.TrimRight(s, " \t")
	if trimmed == "" {
		return "", 0, true // whitespace only after the colon: treat as empty
	}
	decoded, dok := decodeScalar(trimmed)
	return decoded, len(trimmed), dok
}

// isPlainKeyToken reports whether s is a bare, unquoted YAML mapping key —
// the shape every key this package reads or writes (profile, scope, and
// scopes.yaml's own alias keys) already uses.
func isPlainKeyToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isPlainKeyByte(s[i]) {
			return false
		}
	}
	return true
}

func isPlainKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// decodeScalar reduces a scalar value token — plain or quoted — to its
// logical string. Deliberately narrow: a block scalar, a flow collection, an
// anchor or an alias does not appear as a binding's profile/scope or a
// scopes.yaml value in practice, and this returns ok=false for anything it
// does not recognize rather than guess at it.
func decodeScalar(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	switch raw[0] {
	case '\'':
		return unquoteSingle(raw)
	case '"':
		return unquoteDouble(raw)
	case '{', '[', '&', '*', '|', '>', '#':
		return "", false
	}
	if strings.ContainsAny(raw, "{}[]") {
		return "", false
	}
	return raw, true
}

func unquoteSingle(v string) (string, bool) {
	if len(v) < 2 || v[len(v)-1] != '\'' {
		return "", false
	}
	inner := v[1 : len(v)-1]
	return strings.ReplaceAll(inner, "''", "'"), true
}

func unquoteDouble(v string) (string, bool) {
	if len(v) < 2 || v[len(v)-1] != '"' {
		return "", false
	}
	inner := v[1 : len(v)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
			switch inner[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(inner[i])
			}
			continue
		}
		b.WriteByte(inner[i])
	}
	return b.String(), true
}
