package binding

import "strings"

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

// fieldSpan is one "name: value" pair inside a flow map's body. start/end are
// absolute file offsets bounding the raw value token exactly as written
// (quotes included, if any) — what a surgical edit replaces.
type fieldSpan struct {
	name  string
	value string // decoded
	start int
	end   int
}

// bindingEntry is one recognized line under bindings:.
type bindingEntry struct {
	name string
	// lineStart/lineEnd bound the whole line, indentation and trailing
	// newline included (or, for a final line with none, up to EOF) — the
	// span Delete removes.
	lineStart, lineEnd int
	profile, scope     fieldSpan // scope.value is RAW: not alias-resolved
}

// document is one parse of bindings.yaml.
type document struct {
	// aliases is the scopes: map, key -> literal value. Used only to resolve
	// a binding entry's raw scope at read time (see resolve.go) — never
	// exposed outside this package.
	aliases map[string]string

	// entries is every recognized bindings: line, in file order.
	entries []bindingEntry

	// bindingsHeaderEnd is the byte offset immediately after the "bindings:"
	// header line, or -1 if the file has no such line at all.
	bindingsHeaderEnd int
	// insertAt is where Create splices a new entry: the end of the last
	// existing entry's line, or bindingsHeaderEnd if there are none yet.
	insertAt int
	// insertNeedsLeadingNewline is true when insertAt does not already sit
	// just after a '\n' (the preceding line had none), so Create must add
	// one of its own rather than run its new line onto the previous one.
	insertNeedsLeadingNewline bool
}

// find returns the entry named name, if the scan recognized one.
func (d *document) find(name string) (bindingEntry, bool) {
	for _, e := range d.entries {
		if e.name == name {
			return e, true
		}
	}
	return bindingEntry{}, false
}

// binding converts a scanned entry to the [Binding] this package hands out,
// resolving its raw scope through aliases exactly once.
func (d *document) binding(e bindingEntry) Binding {
	return Binding{Name: e.name, Profile: e.profile.value, Scope: resolveScope(e.scope.value, d.aliases)}
}

// parseDocument scans bindings.yaml's bytes into a [document]. It is a
// narrow, line-oriented reader, not a YAML parser — see the package doc for
// why: a scan this poor at representing the document cannot become a save
// path by accident, and every write in this package goes through byte spans
// this scan located, never through re-encoding anything.
//
// Only two top-level sections are recognized, scopes: and bindings:, by a
// bare "key:" line at column zero with nothing else on it; any other
// column-zero line (a comment, or some other top-level key) ends whichever
// section preceded it. Nested lines are read according to whichever section
// is current; a nested line that does not match that section's expected
// shape is skipped rather than guessed at.
func parseDocument(data []byte) *document {
	d := &document{aliases: map[string]string{}, bindingsHeaderEnd: -1}
	section := ""
	for _, ln := range splitLines(data) {
		trimmed := strings.TrimRight(ln.text, "\r\n")
		if trimmed == "" {
			continue
		}
		if trimmed[0] != ' ' && trimmed[0] != '\t' {
			if trimmed[0] == '#' {
				continue
			}
			key, isHeader := topLevelKey(trimmed)
			switch {
			case isHeader && key == "scopes":
				section = "scopes"
			case isHeader && key == "bindings":
				section = "bindings"
				d.bindingsHeaderEnd = ln.end
			default:
				section = ""
			}
			continue
		}
		switch section {
		case "scopes":
			if k, v, ok := parseScopeLine(trimmed); ok {
				d.aliases[k] = v
			}
		case "bindings":
			if e, ok := parseBindingLine(ln, trimmed); ok {
				d.entries = append(d.entries, e)
			}
		}
	}

	d.insertAt = d.bindingsHeaderEnd
	if n := len(d.entries); n > 0 {
		d.insertAt = d.entries[n-1].lineEnd
	}
	if d.insertAt > 0 && d.insertAt <= len(data) {
		d.insertNeedsLeadingNewline = data[d.insertAt-1] != '\n'
	}
	return d
}

// topLevelKey reports whether trimmed is a bare "key:" line with nothing
// else on it — the only shape this scan treats as opening a section.
// "key: value" on one line is not a section header and ends whatever section
// preceded it, same as any other column-zero line.
func topLevelKey(trimmed string) (key string, ok bool) {
	ci := strings.IndexByte(trimmed, ':')
	if ci <= 0 {
		return "", false
	}
	k := trimmed[:ci]
	if !isPlainKeyToken(k) {
		return "", false
	}
	if strings.TrimSpace(trimmed[ci+1:]) != "" {
		return "", false
	}
	return k, true
}

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

// parseScopeLine parses a two-space-indented "key:   value" line inside
// scopes:. The value runs to end of line, trimmed. This scan does not
// unquote a scopes: value — none in the live file need it — so a quoted
// value there simply fails to match and is not added to the alias table,
// which only makes it fail to resolve rather than resolve wrong.
func parseScopeLine(trimmed string) (key, value string, ok bool) {
	if !strings.HasPrefix(trimmed, "  ") {
		return "", "", false
	}
	rest := trimmed[2:]
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '#' {
		return "", "", false
	}
	ci := strings.IndexByte(rest, ':')
	if ci <= 0 {
		return "", "", false
	}
	k := rest[:ci]
	if !isPlainKeyToken(k) {
		return "", "", false
	}
	v := strings.TrimSpace(rest[ci+1:])
	if v == "" {
		return "", "", false
	}
	return k, v, true
}

// parseBindingLine recognizes one bindings: entry: two-space indent, a plain
// key, a colon, and a "{ profile: x, scope: y }" flow map — order
// independent, values optionally quoted, an optional trailing comma. ln.start
// locates the line in the file, so every span in the returned entry is an
// absolute file offset.
//
// A line that is not exactly this shape — a comment, or anything hand-edited
// into a different form — returns ok=false: it is not listed and not
// editable through this package (see the package doc's "shape and existence
// only").
func parseBindingLine(ln line, trimmed string) (e bindingEntry, ok bool) {
	if !strings.HasPrefix(trimmed, "  ") {
		return bindingEntry{}, false
	}
	rest := trimmed[2:]
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '#' {
		return bindingEntry{}, false
	}
	ci := strings.IndexByte(rest, ':')
	if ci <= 0 {
		return bindingEntry{}, false
	}
	key := rest[:ci]
	if !isPlainKeyToken(key) {
		return bindingEntry{}, false
	}

	afterColon := rest[ci+1:]
	braceRel := strings.IndexByte(afterColon, '{')
	if braceRel < 0 || strings.TrimSpace(afterColon[:braceRel]) != "" {
		return bindingEntry{}, false
	}
	closeRel := strings.LastIndexByte(afterColon, '}')
	if closeRel < braceRel || strings.TrimSpace(afterColon[closeRel+1:]) != "" {
		return bindingEntry{}, false
	}

	// Offsets within `trimmed`; trimmed shares its prefix with ln.text (only
	// a trailing \r\n was removed), so these are also offsets within ln.text.
	bodyStartInLine := 2 + ci + 1 + braceRel + 1
	bodyEndInLine := 2 + ci + 1 + closeRel
	body := trimmed[bodyStartInLine:bodyEndInLine]

	fields, ok := parseFlowBody(body)
	if !ok {
		return bindingEntry{}, false
	}

	base := ln.start + bodyStartInLine
	var profile, scope *fieldSpan
	for _, f := range fields {
		f.start += base
		f.end += base
		switch f.name {
		case "profile":
			if profile != nil {
				return bindingEntry{}, false // duplicate field: not this shape
			}
			fc := f
			profile = &fc
		case "scope":
			if scope != nil {
				return bindingEntry{}, false
			}
			fc := f
			scope = &fc
		default:
			return bindingEntry{}, false // an unrecognized field
		}
	}
	if profile == nil || scope == nil {
		return bindingEntry{}, false
	}

	return bindingEntry{
		name:      key,
		lineStart: ln.start,
		lineEnd:   ln.end,
		profile:   *profile,
		scope:     *scope,
	}, true
}

// parseFlowBody parses the inside of a flow map — "profile: x, scope: y",
// order-independent, optional trailing comma, plain or quoted values — into
// its fields. Each field's span is relative to body itself; the caller
// converts to absolute file offsets. ok is false if body contains anything
// this narrow scan does not recognize, which is the signal to leave the
// entry alone rather than risk a corrupting edit.
func parseFlowBody(body string) (fields []fieldSpan, ok bool) {
	i, n := 0, len(body)
	for i < n {
		for i < n && (body[i] == ' ' || body[i] == '\t' || body[i] == ',') {
			i++
		}
		if i >= n {
			break
		}
		nameStart := i
		for i < n && isPlainKeyByte(body[i]) {
			i++
		}
		if i == nameStart {
			return nil, false
		}
		name := body[nameStart:i]
		for i < n && (body[i] == ' ' || body[i] == '\t') {
			i++
		}
		if i >= n || body[i] != ':' {
			return nil, false
		}
		i++
		for i < n && (body[i] == ' ' || body[i] == '\t') {
			i++
		}
		valStart := i
		if i < n && (body[i] == '\'' || body[i] == '"') {
			quote := body[i]
			i++
			closed := false
			for i < n {
				switch {
				case quote == '\'' && body[i] == '\'':
					if i+1 < n && body[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					closed = true
				case quote == '"' && body[i] == '\\' && i+1 < n:
					i += 2
					continue
				case quote == '"' && body[i] == '"':
					i++
					closed = true
				default:
					i++
					continue
				}
				break
			}
			if !closed {
				return nil, false
			}
		} else {
			for i < n && body[i] != ',' {
				i++
			}
		}
		valEnd := i
		raw := body[valStart:valEnd]
		rawTrimmed := strings.TrimRight(raw, " \t")
		valEnd = valStart + len(rawTrimmed)
		decoded, dok := decodeScalar(rawTrimmed)
		if !dok {
			return nil, false
		}
		fields = append(fields, fieldSpan{name: name, value: decoded, start: valStart, end: valEnd})
	}
	return fields, true
}

// decodeScalar reduces a flow-map value token — plain or quoted — to its
// logical string. Deliberately narrow: a block scalar, a flow collection, an
// anchor or an alias does not appear inside a {profile, scope} entry in
// practice, and this returns ok=false for anything it does not recognize
// rather than guess at it.
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
