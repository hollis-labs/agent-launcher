package bundle

// Kind names one of the five artifact kinds a bundle holds.
//
// The kind is part of an artifact's identity, not a label on it: an id alone
// does not name a file.
//
// Two kinds retired on 2026-09-10, and neither is coming back under another
// name. KindRoleProse read templates/roles/*.md, which agent-setup deleted
// when profiles became templates in their own right — a role's prose is a
// `{{ section charter }}` in the profile now, not a separate file a slot
// pulls in. KindBinding read bindings/*, which agent-setup retired
// wholesale and cairn stopped accepting; launch configuration is the
// launcher's, and lives in internal/launchprofile.
type Kind string

const (
	// KindProfile is profiles/<id>.md or profiles/parts/<id>.md — the unit
	// of composition. The subdirectory is organization, not another kind.
	KindProfile Kind = "profile"
	// KindTemplate is templates/**/*.md — a document a profile pulls in,
	// at any depth. The id carries the path below templates/ ("lenses/
	// primary-source-first"), because that is what makes it addressable:
	// agent-setup organizes templates into lenses/, projects/ and per-role
	// directories, and a top-level-only reader saw 3 of its 13 files.
	KindTemplate Kind = "template"
	// KindPrompt is prompts/<id>.md — a flat file that Cairn plants whole at
	// .claude/commands/boot/<id>.md so a running session can invoke it as
	// /boot:<id>. Nothing here delivers it anywhere: this package only ever
	// hands back its bytes for the manager's own open/save (see plan
	// CW-20260904-0006, "there is no delivery work").
	KindPrompt Kind = "prompt"
	// KindSkill is skills/<id>/SKILL.md — one directory per skill.
	KindSkill Kind = "skill"
	// KindHook is hooks/<id>.sh — referenced in place by settings, edited as
	// text.
	KindHook Kind = "hook"
)

// Kinds returns the five artifact kinds, in a stable order suitable for
// grouping a tree. The order is presentational and carries no semantics.
func Kinds() []Kind {
	return []Kind{KindProfile, KindTemplate, KindPrompt, KindSkill, KindHook}
}

// Artifact ids. Each kind has its own id type so that the compiler rejects a
// template id where a profile id is wanted, and vice versa.
type (
	// ProfileID is the basename of a profiles/*.md or immediate
	// profiles/parts/*.md file, without ".md".
	ProfileID string
	// TemplateID is a templates/**/*.md file's path below templates/,
	// without ".md" and always slash-separated: "claude",
	// "lenses/primary-source-first", "projects/cairn".
	TemplateID string
	// PromptID is the basename of a prompts/*.md file, without ".md".
	PromptID string
	// SkillID is the name of a directory under skills/.
	SkillID string
	// HookID is the basename of a hooks/*.sh file, without ".sh".
	HookID string
)

// Ref addresses one artifact by kind and id. It is the handle a UI passes back
// to [Bundle.Read]. Because Kind is part of it, Ref{KindProfile, "architect"}
// and Ref{KindPrompt, "architect"} are different refs.
type Ref struct {
	Kind Kind
	ID   string
}

// Profile is profiles/<id>.md or profiles/parts/<id>.md. Both locations
// share one bare-ID namespace.
type Profile struct {
	// ID is the file's basename without ".md". It is not necessarily the "id"
	// in the frontmatter; nothing here reconciles the two (D8).
	ID ProfileID
	// Path is absolute. RelPath is slash-separated and relative to the root.
	Path    string
	RelPath string
	// Header is a shallow read of the frontmatter, for display. It is absent
	// rather than an error when the file has no frontmatter.
	Header Header
}

// Ref returns the profile's handle.
func (p Profile) Ref() Ref { return Ref{Kind: KindProfile, ID: string(p.ID)} }

// Template is templates/**/*.md — a document a profile pulls in through
// `{{ file: ... }}`, at any depth below templates/.
type Template struct {
	ID      TemplateID
	Path    string
	RelPath string
}

// Ref returns the template's handle.
func (t Template) Ref() Ref { return Ref{Kind: KindTemplate, ID: string(t.ID)} }

// Prompt is prompts/<id>.md — a flat, plain-markdown file. Unlike
// [Template] it carries no roles/-style subdirectory to exclude and, unlike
// [Profile] or [Skill], no frontmatter this package reads for display: it is
// authored text, handled only as bytes (D5), with no delivery path anywhere
// in Tachyon (see [KindPrompt]'s own doc).
type Prompt struct {
	ID      PromptID
	Path    string
	RelPath string
}

// Ref returns the prompt's handle.
func (p Prompt) Ref() Ref { return Ref{Kind: KindPrompt, ID: string(p.ID)} }

// Skill is skills/<id>/, whose editable file is SKILL.md.
type Skill struct {
	// Name is the directory name under skills/.
	Name SkillID
	// Dir is the absolute skill directory.
	Dir string
	// Path is the absolute path to SKILL.md. It is reported even when the file
	// is absent, so a caller can offer to create it.
	Path    string
	RelPath string
	// HasSkillFile records whether a regular SKILL.md is visible. Existence is
	// reported, not enforced: a directory without one still enumerates (D8).
	HasSkillFile bool
	// Header is a shallow read of SKILL.md's frontmatter, for display. Its
	// Unreadable field distinguishes a SKILL.md with no frontmatter from one
	// whose bytes could not be read.
	Header Header
}

// Ref returns the skill's handle.
func (s Skill) Ref() Ref { return Ref{Kind: KindSkill, ID: string(s.Name)} }

// Hook is hooks/<id>.sh.
type Hook struct {
	// Name is the file's basename without ".sh".
	Name    HookID
	Path    string
	RelPath string
}

// Ref returns the hook's handle.
func (h Hook) Ref() Ref { return Ref{Kind: KindHook, ID: string(h.Name)} }

// Contents is one enumeration of the whole bundle, grouped by kind.
//
// Every field is non-nil after a successful [Bundle.Contents], and empty for a
// kind whose directory is absent.
type Contents struct {
	Profiles  []Profile
	Templates []Template
	Prompts   []Prompt
	Skills    []Skill
	Hooks     []Hook
}
