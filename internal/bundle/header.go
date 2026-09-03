package bundle

import (
	"bytes"
	"strings"
)

// Header is what a tree needs to label an artifact: the few top-level scalars
// at the head of a file's YAML frontmatter.
//
// It is a display projection and nothing else. It is not a document model, it
// does not round-trip, and no write path in Tachyon may be built from it —
// bytes in, bytes out (D5). It is also not a validation result: a file with no
// frontmatter, or with frontmatter this scan does not understand, yields a
// zero Header and no error (D8).
type Header struct {
	// Present reports whether a frontmatter block was found at all.
	Present bool
	// Unreadable reports that the file's bytes could not be read, so nothing
	// here was scanned from them. It exists so that "this file has no
	// frontmatter" and "this file could not be opened" are not the same zero
	// value: a row labelled from a Header that is merely absent is a row about
	// a file the user can go and edit, and a row labelled from an unreadable
	// one is not. [ScanHeader] never sets it — only reading a file does.
	Unreadable bool
	// ID, Name, Description and Extends are the frontmatter values of those
	// keys, empty when absent. Extends is Cairn's parent-profile reference; it
	// is reported verbatim and is not resolved here.
	ID          string
	Name        string
	Description string
	Extends     string
}

// headerKeys are the only keys ScanHeader looks for. Everything else in the
// frontmatter, spec in particular, is deliberately not read: this package does
// not display slots, skills or settings, so it cannot imply an order for them.
var headerKeys = map[string]func(*Header, string){
	"id":          func(h *Header, v string) { h.ID = v },
	"name":        func(h *Header, v string) { h.Name = v },
	"description": func(h *Header, v string) { h.Description = v },
	"extends":     func(h *Header, v string) { h.Extends = v },
}

// ScanHeader extracts [Header] from a file's bytes.
//
// It reads, and does not parse: it walks the lines of a leading "---" block
// looking for four unindented "key: value" scalars, and understands nothing
// else. Nested mappings, sequences, block scalars, anchors and multi-document
// streams are skipped rather than interpreted. That narrowness is the point —
// a scan that cannot represent the document cannot become a save path.
//
// The bytes are not modified and nothing derived from them is written back.
func ScanHeader(data []byte) Header {
	var h Header

	// A UTF-8 BOM is tolerated for the scan only; it stays in the file.
	body := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	lines := strings.Split(string(body), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t\r") != "---" {
		return h
	}
	h.Present = true

	for _, raw := range lines[1:] {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "---" || trimmed == "..." {
			break
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Only column-zero keys. Anything indented belongs to a nested
		// mapping this scan does not enter.
		if line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		set, wanted := headerKeys[key]
		if !wanted {
			continue
		}
		// First occurrence wins; a duplicate key is the user's problem, not
		// something to resolve here.
		if alreadySet(&h, key) {
			continue
		}
		set(&h, scalarValue(value))
	}
	return h
}

func alreadySet(h *Header, key string) bool {
	switch key {
	case "id":
		return h.ID != ""
	case "name":
		return h.Name != ""
	case "description":
		return h.Description != ""
	case "extends":
		return h.Extends != ""
	}
	return false
}

// splitKeyValue splits "key: value" when key is a plain unquoted scalar.
func splitKeyValue(line string) (key, value string, ok bool) {
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	key = line[:i]
	for j := 0; j < len(key); j++ {
		c := key[j]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if !isAlpha && !isDigit && c != '_' && c != '-' && c != '.' {
			return "", "", false
		}
	}
	rest := line[i+1:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		// "key:value" is not a mapping entry in YAML.
		return "", "", false
	}
	// Keys are matched exactly: YAML is case-sensitive, and "ID" is not "id".
	return key, strings.TrimSpace(rest), true
}

// scalarValue reduces a plain or quoted YAML scalar to display text. A value
// this scan cannot read as a scalar — a block scalar, a collection — comes
// back empty, which reads in a tree as "not shown", never as "empty in the
// file".
func scalarValue(v string) string {
	if v == "" {
		return ""
	}
	switch v[0] {
	case '\'':
		if s, ok := unquoteSingle(v); ok {
			return s
		}
		return ""
	case '"':
		if s, ok := unquoteDouble(v); ok {
			return s
		}
		return ""
	case '#':
		return ""
	case '|', '>', '&', '*', '[', '{':
		// Block scalars, anchors, aliases and flow collections are not
		// scalars this scan reads.
		return ""
	}
	// Strip a trailing comment, which in a plain scalar must be preceded by
	// whitespace.
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	if i := strings.Index(v, "\t#"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	switch v {
	case "null", "Null", "NULL", "~":
		return ""
	}
	return v
}

func unquoteSingle(v string) (string, bool) {
	if len(v) < 2 {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		if v[i] != '\'' {
			b.WriteByte(v[i])
			continue
		}
		if i+1 < len(v) && v[i+1] == '\'' {
			b.WriteByte('\'')
			i++
			continue
		}
		return b.String(), true // closing quote
	}
	return "", false // unterminated
}

func unquoteDouble(v string) (string, bool) {
	if len(v) < 2 {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		switch v[i] {
		case '\\':
			if i+1 >= len(v) {
				return "", false
			}
			i++
			switch v[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(v[i])
			}
		case '"':
			return b.String(), true
		default:
			b.WriteByte(v[i])
		}
	}
	return "", false
}
