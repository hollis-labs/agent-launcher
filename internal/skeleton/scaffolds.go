package skeleton

import (
	"fmt"
	"path"
	"strings"

	"github.com/hollis-labs/tachyon/internal/binding"
)

// --- relPath functions: where each kind lands, mirroring internal/bundle's
// own (unexported) directory layout constants. Duplicating four short path
// joins here — rather than exporting bundle's dirProfiles/dirTemplates/etc. —
// keeps this package's only coupling to bundle's public surface (Open, Kind,
// Ref, Kinds), which is what let this whole package be written and tested
// without touching a single line of internal/bundle. bundle.KindBinding's
// relPath is [binding.BindingRelPath] itself, not a fifth function here —
// see [registry]'s own doc for why. ---

func profileRelPath(id string) string   { return path.Join("profiles", id+".md") }
func roleProseRelPath(id string) string { return path.Join("templates", "roles", id+".md") }
func templateRelPath(id string) string  { return path.Join("templates", id+".md") }
func skillRelPath(id string) string     { return path.Join("skills", id, "SKILL.md") }

// bindingRefID derives the ID a new binding's [bundle.Ref] carries: its
// filename, extension included — path.Base of the same
// [binding.BindingRelPath] call [entry.relPath] already made to place the
// file, so the extension is spelled in exactly the one place
// (internal/binding) that owns it, not duplicated here as a literal
// ".yaml". This matches bundle.BindingID's own documented convention
// ("the format is not pinned and the extension is part of the name") that
// every other kind's ID deliberately does not follow — see [entry.refID].
func bindingRefID(id string) string { return path.Base(binding.BindingRelPath(id)) }

// profileScaffold renders profiles/<id>.md.
//
// Every one of this bundle's 9 live profiles carries exactly these five
// frontmatter scalars — id, extends, name, description, provider — before an
// optional spec (verified against profiles/architect.md, engineer.md,
// orchestrator.md, planner.md, writer.md, reviewer.md, director.md,
// conductor.md and base.md itself; every one of the 9 has all five). This
// scaffold's spec starts empty, with a commented example of the two things a
// new role profile most commonly adds: a "role" slot pointing at
// templates/roles/<id>.md, and a skills list.
//
// base.md is majority explanatory comments — it is the abstract floor every
// profile extends, not a template for an ordinary one (CW-20260903-0010 task
// body). An ordinary new profile stays minimal: this is closer in shape to
// profiles/planner.md or profiles/writer.md than to base.md.
func profileScaffold(spec Spec) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("id: " + spec.ID + "\n")
	b.WriteString("extends: base\n")
	b.WriteString(scalarLine("name", displayName(spec)))
	b.WriteString(scalarLine("description", spec.Description))
	b.WriteString("provider: claude\n")
	b.WriteString("spec: {}\n")
	b.WriteString("# Every profile inherits base's templates, standing prose, install\n")
	b.WriteString("# skills and settings (profiles/base.md) and adds only what makes it\n")
	b.WriteString("# this role. See profiles/architect.md, profiles/engineer.md and the\n")
	b.WriteString("# rest of this bundle's profiles for real examples. Nothing here is\n")
	b.WriteString("# validated (D8) -- get the shape right by matching one of them.\n")
	b.WriteString("#\n")
	b.WriteString("# Replace `spec: {}` above with something like:\n")
	b.WriteString("#\n")
	b.WriteString("# spec:\n")
	b.WriteString("#   slots:\n")
	b.WriteString("#     - name: role\n")
	b.WriteString(fmt.Sprintf(
		"#       source: { kind: static_file, static_file: { path: ~/.config/agents/templates/roles/%s.md } }\n",
		spec.ID))
	b.WriteString("#   skills: [skill-one, skill-two]\n")
	b.WriteString("---\n")
	return []byte(b.String())
}

// roleProseScaffold renders templates/roles/<id>.md.
//
// A bare prose file, since — per CW-20260903-0010's task body — that is all
// a role prose file is: no frontmatter, no markers. See
// templates/roles/architect.md and templates/roles/writer.md in this bundle
// for the real shape: a "# Title" heading, then a handful of short
// paragraphs about what this role decides and does not.
//
// The one thing worth saying that isn't visible in those two files is what
// this file IS: the content a profile's "role" slot pulls in through a
// static_file source, and a separate artifact kind from the profile that
// shares its basename (the conflation trap internal/bundle's package doc and
// CW-20260903-0009 both name). That note is written as an HTML comment so it
// stays invisible once rendered and does not read as part of the role's own
// voice.
func roleProseScaffold(spec Spec) []byte {
	var b strings.Builder
	b.WriteString("# " + displayName(spec) + "\n")
	b.WriteString("\n")
	b.WriteString("<!-- Prose only -- no frontmatter, no markers. This file becomes the\n")
	b.WriteString("     \"role\" slot's content for whichever profile's spec.slots points a\n")
	b.WriteString("     role slot's static_file at this path (profiles/architect.md shows the\n")
	b.WriteString("     pattern). It is a separate artifact from any profile sharing this same\n")
	b.WriteString("     basename -- see internal/bundle's package doc on the two \"role\"s.\n")
	b.WriteString("     Say what this role decides that no other role decides, and what it\n")
	b.WriteString("     does not do. Delete this comment once real prose replaces it. -->\n")
	return []byte(b.String())
}

// templateScaffold renders templates/<id>.md.
//
// This is where CW-20260903-0010 requires two rules to be stated as
// comments, because D8 means nothing checks them and a typo degrades
// silently (see below). Both were verified directly against
// github.com/chrispian/cairn/bootdir/template.go, not just against this
// bundle's own templates/agents.md, before being written here:
//
//   - ValueNames() in that file returns exactly
//     []string{"binding", "model", "profile", "provider", "scope", "session"}
//     -- the six names cairn:value fills from. fills() reports false for
//     anything else, Substitute renders the empty string for it, and Unfilled
//     is what puts a warning on stderr for it -- the boot itself still exits
//     0 (Substitute returns no error for an unknown value name; this
//     changed under CW-20260902-0009, which is why the comment says so).
//   - Substitute's own doc comment states the line rule verbatim: "A line
//     that held nothing but markers and whitespace ... is removed entirely
//     -- its newline with it, ... A marker that shares its line with content
//     is the other case: only the marker goes, and the line stays exactly as
//     it was written." templates/agents.md in this bundle is the real file
//     that rule was independently re-verified against: line 14,
//     "- scope: <!-- cairn:value scope -->", is exactly the shared-line
//     example this scaffold repeats.
//
// The explanatory block below is itself an HTML comment, and was checked
// against cairn's own markerPattern regexp (`<!--\s*cairn:(.*?)-->`) to
// confirm it cannot itself be misread as a marker: the pattern requires
// "cairn:" immediately after "<!--" and optional whitespace, and this
// block's first line is prose, not whitespace-then-"cairn:", so the regex
// never starts a match there. Only the two markers after it — cairn:slot
// example and cairn:value scope — match.
func templateScaffold(spec Spec) []byte {
	_ = spec // no field of Spec is used by this scaffold today
	var b strings.Builder
	b.WriteString("<!--\n")
	b.WriteString("  New template scaffold. A template is a document containing cairn:slot\n")
	b.WriteString("  and cairn:value markers -- see templates/agents.md in this bundle for\n")
	b.WriteString("  the real one Cairn assembles AGENTS.md from.\n")
	b.WriteString("\n")
	b.WriteString("  Two rules, unchecked by anything (D8) -- this comment is where they get\n")
	b.WriteString("  said, because nothing else says them:\n")
	b.WriteString("\n")
	b.WriteString("  1. `cairn:value <name>` fills from exactly SIX instance facts:\n")
	b.WriteString("       binding, model, profile, provider, scope, session\n")
	b.WriteString("     Any other name -- a typo included -- renders nothing, and cairn\n")
	b.WriteString("     warns on stderr, but the boot still exits 0 (as of CW-20260902-0009).\n")
	b.WriteString("     Read the name back against this list; nothing else will catch it.\n")
	b.WriteString("\n")
	b.WriteString("  2. What happens to a marker's own line when it substitutes nothing:\n")
	b.WriteString("       - A marker ALONE on its line takes the whole line with it, newline\n")
	b.WriteString("         included. Put optional markers on adjacent lines, with no blank\n")
	b.WriteString("         line between them, if you want them to vanish cleanly as a group.\n")
	b.WriteString("       - A marker SHARING its line with other content keeps the line. See\n")
	b.WriteString("         the \"- scope:\" example below: with no scope at boot it still\n")
	b.WriteString("         renders \"- scope: \" -- only the marker itself goes.\n")
	b.WriteString("     Blank lines you write between markers are your content and survive;\n")
	b.WriteString("     Cairn never reformats prose.\n")
	b.WriteString("-->\n")
	b.WriteString("\n")
	b.WriteString("<!-- cairn:slot example -->\n")
	b.WriteString("\n")
	b.WriteString("## Example\n")
	b.WriteString("\n")
	b.WriteString("- scope: <!-- cairn:value scope -->\n")
	return []byte(b.String())
}

// skillScaffold renders skills/<id>/SKILL.md.
//
// The frontmatter is exactly the two fields every one of this bundle's 17
// skills carries (name, description) and nothing more — verified by scanning
// every skills/*/SKILL.md frontmatter block in the live bundle, not assumed
// from one example. name is the directory name itself, matching how every
// existing skill is written (skills/commit/SKILL.md carries "name: commit").
func skillScaffold(spec Spec) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + spec.ID + "\n")
	b.WriteString(scalarLine("description", spec.Description))
	b.WriteString("---\n")
	b.WriteString("\n")
	b.WriteString("<!-- Skill body. See skills/commit/SKILL.md or skills/adr/SKILL.md in\n")
	b.WriteString("     this bundle for the real shape: usually a short lead line, then\n")
	b.WriteString("     whatever procedure or reference this skill exists to save someone\n")
	b.WriteString("     from re-deriving. Delete this comment once real content replaces\n")
	b.WriteString("     it. -->\n")
	return []byte(b.String())
}

// bindingScaffold renders bindings/<id>.yaml.
//
// It deliberately does NOT go through internal/binding's own
// [binding.Store.Create]: that requires non-empty Profile and Scope
// ([binding.Binding.Validate]), and Spec carries neither for a binding —
// unlike a profile's "spec: {}" or a skill's blank body, "profile" and
// "scope" are the only two things a binding file has, so there is no
// content left to leave blank except those two values themselves. This
// scaffold leaves both keys present with empty values ("profile:\n" /
// "scope:\n"), matching the same "start blank, let the user fill in the
// one thing that must be theirs" idiom every other scaffold in this file
// uses. internal/binding's own scan treats a present-but-empty key as
// valid, not corrupt (see that package's scan.go, scanScalarToken) — a
// freshly scaffolded binding is readable immediately, if obviously
// incomplete, rather than making the whole bindings/ directory fail to
// list until it is edited (see that package's doc, "shape and existence
// only, now failed loud," for why a truly *unrecognized* file — missing
// either key entirely — is not this package's concern to avoid, but a
// present, empty key is not that).
//
// This is "at minimum produce the exact same on-disk shape internal/binding
// itself writes" (this task's own allowance) rather than "call
// [binding.Store.Create] directly" (its stated preference) — a deliberate,
// narrow exception to "don't reimplement file-writing logic" for exactly
// the reason above, not an oversight. [registry]'s relPath for
// bundle.KindBinding is [binding.BindingRelPath] itself, not a function
// defined in this file, which is what keeps bindings/'s directory name and
// ".yaml" extension known in exactly one place despite this scaffold's
// content being written here rather than by internal/binding.
func bindingScaffold(spec Spec) []byte {
	_ = spec // no field of Spec is used by this scaffold today
	var b strings.Builder
	b.WriteString("# New binding -- `cairn boot <name>` looks here first, falling back to a\n")
	b.WriteString("# profile of the same id. profile names a profile under profiles/\n")
	b.WriteString("# (refused when the catalog is read if it does not exist, not here);\n")
	b.WriteString("# scope is a literal path, never one of ../scopes.yaml's own alias keys.\n")
	b.WriteString("# See bindings/README.md in this bundle for the full shape.\n")
	b.WriteString("profile:\n")
	b.WriteString("scope:\n")
	return []byte(b.String())
}

// scalarLine renders "key: value\n" for a frontmatter scalar, quoting value
// only when it actually needs it (see [needsQuoting]) so an ordinary
// description reads the same unquoted way this bundle's own profiles and
// skills already do. An empty value renders "key:\n" — the same "a marker
// sharing a line keeps everything but the marker, an absent value still
// leaves the key" shape templates/agents.md's own
// "- scope: <!-- cairn:value scope -->" line demonstrates for the analogous
// case in a template.
func scalarLine(key, value string) string {
	if value == "" {
		return key + ":\n"
	}
	if needsQuoting(value) {
		return key + ": " + yamlQuote(value) + "\n"
	}
	return key + ": " + value + "\n"
}

// needsQuoting reports whether value would change meaning, or break parsing,
// as a plain (unquoted) YAML scalar: a leading indicator character, a
// ": " or trailing ":" that would read as a mapping, a " #" that would start
// a comment mid-value, a reserved scalar keyword, embedded whitespace at
// either end, or a literal newline.
func needsQuoting(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return true
	}
	if strings.TrimSpace(value) != value {
		return true
	}
	if strings.Contains(value, ": ") || strings.HasSuffix(value, ":") {
		return true
	}
	if strings.Contains(value, " #") {
		return true
	}
	switch value[0] {
	case '"', '\'', '#', '&', '*', '!', '|', '>', '%', '@', '`', '[', ']', '{', '}', ',', '-', '?', ':':
		return true
	}
	switch strings.ToLower(value) {
	case "null", "~", "true", "false", "yes", "no", "on", "off":
		return true
	}
	return false
}

// yamlQuote renders value as a YAML double-quoted scalar that also round
// trips through internal/bundle's own Header scanner (header.go's
// unquoteDouble): only backslash and the double quote are escaped, matching
// the two escapes that scanner actually understands (its default case for
// any other "\X" just drops the backslash, which is why this deliberately
// does not lean on \u escapes the way encoding/json's string quoting would).
// A literal newline or carriage return — which that scanner's line-based
// walk cannot see past — is folded to a single space rather than escaped,
// since Name and Description are single-line display fields, not prose.
func yamlQuote(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
