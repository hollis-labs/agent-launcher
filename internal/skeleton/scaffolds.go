package skeleton

import (
	"path"
	"strings"
)

// --- relPath functions: where each kind lands, mirroring internal/bundle's
// own (unexported) directory layout constants. Keeping these short path
// joins here — rather than exporting bundle's dirProfiles/dirTemplates/etc. —
// keeps the creation package coupled only to bundle's public surface. The
// bundle itself independently owns enumeration and resolution of both profile
// locations.
// rather than a function here —
// see [registry]'s own doc for why. ---

func profileRelPath(id string) string  { return path.Join("profiles", id+".md") }
func partRelPath(id string) string     { return path.Join("profiles", "parts", id+".md") }
func templateRelPath(id string) string { return path.Join("templates", id+".md") }
func promptRelPath(id string) string   { return path.Join("prompts", id+".md") }
func skillRelPath(id string) string    { return path.Join("skills", id, "SKILL.md") }

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
	b.WriteString("spec: {}\n")
	b.WriteString("# No provider. A runtime is a launch's to choose, not an agent's to\n")
	b.WriteString("# carry: no profile in this catalog declares one, and Tachyon supplies\n")
	b.WriteString("# it from the launch profile instead. Adding `provider:` here would put\n")
	b.WriteString("# it back where it just left.\n")
	b.WriteString("#\n")
	b.WriteString("# Every profile inherits base's templates, standing prose, install\n")
	b.WriteString("# skills and settings (profiles/base.md) and adds only what makes it\n")
	b.WriteString("# this role. See profiles/architect.md, profiles/engineer.md and the\n")
	b.WriteString("# rest of this bundle's profiles for real examples. Nothing here is\n")
	b.WriteString("# validated (D8) -- get the shape right by matching one of them.\n")
	b.WriteString("#\n")
	b.WriteString("# Replace `spec: {}` above with something like:\n")
	b.WriteString("#\n")
	b.WriteString("# spec:\n")
	b.WriteString("#   skills: [skill-one, skill-two]\n")
	b.WriteString("#   subagents: [reviewer]\n")
	b.WriteString("---\n")
	b.WriteString("\n")
	b.WriteString("<!-- Below the frontmatter is the profile's BODY, and it is a template.\n")
	b.WriteString("     Sections fill the holes the layout yields:\n")
	b.WriteString("\n")
	b.WriteString("       {{ section charter }}\n")
	b.WriteString("       # " + displayName(spec) + "\n")
	b.WriteString("       What this role decides that no other role decides.\n")
	b.WriteString("       {{ end }}\n")
	b.WriteString("\n")
	b.WriteString("       {{ section lens }}\n")
	b.WriteString("       {{ file: $CAIRN_PROFILE_ROOT/templates/lenses/<a-lens>.md }}\n")
	b.WriteString("       {{ end }}\n")
	b.WriteString("\n")
	b.WriteString("     A profile declaring only sections is a fragment, and base.md\n")
	b.WriteString("     supplies the shape. profiles/engineer.md is the worked example.\n")
	b.WriteString("     Delete this comment once real content replaces it. -->\n")
	return []byte(b.String())
}

// partScaffold renders profiles/parts/<id>.md. A part is directly bootable,
// so it extends base, but it deliberately carries no identity-like metadata:
// composed profile metadata participates in resolution, and a generic part
// must not overwrite the target profile's name, description, or provider.
func partScaffold(spec Spec) []byte {
	return []byte("---\n" +
		"id: " + spec.ID + "\n" +
		"extends: base\n" +
		"spec: {}\n" +
		"# Replace spec with only the keys this part contributes.\n" +
		"---\n")
}

// templateScaffold renders templates/<id>.md.
//
// A template is prose a profile pulls in, and that is now the whole of it.
// It carries no markers and no directives: cairn's template engine puts the
// verbs in the PROFILE — `{{ extends }}`, `{{ section }}`, `{{ yield }}`,
// `{{ value }}`, `{{ parent }}` — and a file named by `{{ file: ... }}` is
// substituted whole.
//
// This scaffold used to teach the marker engine: the six names
// `cairn:value` filled from, and the rules about what a marker's own line
// did when it rendered nothing. agent-setup moved off that engine on
// 2026-09-10 (profiles became templates in their own right) and templates/
// grew lenses/ and projects/ in place of roles/. Teaching markers now would
// hand someone a document the live bundle has no reader for.
//
// What it deliberately does NOT teach is the profile-side syntax. That
// belongs beside a profile, and profiles/base.md is the worked example.
func templateScaffold(spec Spec) []byte {
	_ = spec // no field of Spec is used by this scaffold today
	var b strings.Builder
	b.WriteString("<!--\n")
	b.WriteString("  New template. This file is prose, pulled into a profile whole:\n")
	b.WriteString("\n")
	b.WriteString("      {{ section context }}\n")
	b.WriteString("      {{ file: $CAIRN_PROFILE_ROOT/templates/<this file> }}\n")
	b.WriteString("      {{ end }}\n")
	b.WriteString("\n")
	b.WriteString("  So there is nothing to declare here and no syntax to get right --\n")
	b.WriteString("  every directive lives in the profile that names this file. See\n")
	b.WriteString("  profiles/base.md and profiles/engineer.md in this bundle for the\n")
	b.WriteString("  worked examples, and templates/lenses/ for prose in this shape.\n")
	b.WriteString("\n")
	b.WriteString("  Subdirectories are yours to organize: templates/lenses/x.md is the\n")
	b.WriteString("  template \"lenses/x\". Delete this comment once real content\n")
	b.WriteString("  replaces it.\n")
	b.WriteString("-->\n")
	return []byte(b.String())
}

// promptScaffold renders prompts/<id>.md.
//
// This is the minimal-scaffold fallback CW-20260904-0006's own task body
// names ("if the real file's shape doesn't obviously suggest a sensible
// minimal starting template, keep the scaffold minimal"), not an invented
// house style: the one real, non-README file in this bundle's prompts/ as
// of 2026-09-03 (prompts/report.md) is a one-off, task-specific dispatch
// report with its own four cairn:value markers particular to that prompt --
// nothing about it generalizes into a shape every new prompt should start
// from, the same way profiles/base.md is the abstract floor rather than a
// template for an ordinary profile (see profileScaffold's own doc). So this
// scaffold borrows roleProseScaffold's shape instead (a "# Title" heading
// plus an explanatory HTML comment, no frontmatter) and states, once, as a
// comment, the one fact prompts/README.md documents about what a prompt
// IS -- quoted rather than invented: "A prompt is a template. It carries
// the same <!-- cairn:slot ... --> and <!-- cairn:value ... --> markers
// templates/ does" -- without writing any slot or value content into the
// scaffold itself, since there is no real, general-purpose example of one
// to derive that from.
//
// A prompt is plain markdown with no frontmatter (like role prose, unlike
// a profile or a skill), which is also why this scaffold has nowhere to
// put spec.Description: FIELD_SHAPE in frontend/src/NewArtifact.jsx hides
// that field for this kind for the same reason it hides it for role prose.
func promptScaffold(spec Spec) []byte {
	var b strings.Builder
	b.WriteString("# " + displayName(spec) + "\n")
	b.WriteString("\n")
	b.WriteString("<!-- New prompt scaffold. Plain markdown, no frontmatter. Cairn plants\n")
	b.WriteString("     this file whole at .claude/commands/boot/" + spec.ID + ".md once it is\n")
	b.WriteString("     declared (spec.prompts, or --prompt " + spec.ID + " for one launch), so a\n")
	b.WriteString("     running session can invoke it as /boot:" + spec.ID + " -- nothing in\n")
	b.WriteString("     Tachyon delivers it any other way; a person types the command.\n")
	b.WriteString("\n")
	b.WriteString("     Only the Claude tree plants prompts. A Codex boot has nowhere to\n")
	b.WriteString("     put them, drops them, and says so on stderr -- the boot directory\n")
	b.WriteString("     is otherwise complete.\n")
	b.WriteString("\n")
	b.WriteString("     Delete this comment and write the prompt's own content. -->\n")
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
