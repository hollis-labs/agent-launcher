package bundle

// Kind names one of the six artifact kinds a bundle holds.
//
// The kind is part of an artifact's identity, not a label on it: profiles and
// role prose share every basename in the bundle, so an id alone does not name
// a file.
type Kind string

const (
	// KindProfile is profiles/<id>.md — the unit of composition.
	KindProfile Kind = "profile"
	// KindRoleProse is templates/roles/<id>.md — prose a profile pulls in
	// through its "role" slot. Not a profile.
	KindRoleProse Kind = "role-prose"
	// KindTemplate is templates/<id>.md — a document with cairn:slot and
	// cairn:value markers. Top level only; roles/ is KindRoleProse.
	KindTemplate Kind = "template"
	// KindSkill is skills/<id>/SKILL.md — one directory per skill.
	KindSkill Kind = "skill"
	// KindHook is hooks/<id>.sh — referenced in place by settings, edited as
	// text.
	KindHook Kind = "hook"
	// KindBinding is bindings/<id> — a saved composition. The per-file format
	// is not pinned by Cairn and this package does not interpret it.
	KindBinding Kind = "binding"
)

// Kinds returns the six artifact kinds, in a stable order suitable for
// grouping a tree. The order is presentational and carries no semantics.
func Kinds() []Kind {
	return []Kind{KindProfile, KindRoleProse, KindTemplate, KindSkill, KindHook, KindBinding}
}

// Artifact ids. Each kind has its own id type so that the compiler rejects a
// role prose id where a profile id is wanted, and vice versa. The eight role
// prose files all share a basename with a profile, so this separation is what
// keeps "the architect role" from being ambiguous.
type (
	// ProfileID is the basename of a profiles/*.md file, without ".md".
	ProfileID string
	// RoleProseID is the basename of a templates/roles/*.md file, without
	// ".md". It is not a ProfileID even when it spells the same word.
	RoleProseID string
	// TemplateID is the basename of a templates/*.md file, without ".md".
	TemplateID string
	// SkillID is the name of a directory under skills/.
	SkillID string
	// HookID is the basename of a hooks/*.sh file, without ".sh".
	HookID string
	// BindingID is the basename of a file under bindings/, extension included,
	// because the format is not pinned and the extension is part of the name.
	BindingID string
)

// Ref addresses one artifact by kind and id. It is the handle a UI passes back
// to [Bundle.Read]. Because Kind is part of it, Ref{KindProfile, "architect"}
// and Ref{KindRoleProse, "architect"} are different refs.
type Ref struct {
	Kind Kind
	ID   string
}

// Profile is profiles/<id>.md.
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

// RoleProse is templates/roles/<id>.md — the prose behind a profile's "role"
// slot. It is a separate type from [Profile] on purpose; see the package doc.
type RoleProse struct {
	// Role is the file's basename without ".md".
	Role    RoleProseID
	Path    string
	RelPath string
}

// Ref returns the role prose file's handle.
func (r RoleProse) Ref() Ref { return Ref{Kind: KindRoleProse, ID: string(r.Role)} }

// Template is templates/<id>.md — a document carrying cairn:slot and
// cairn:value markers. Marker positions are the only ordering in the bundle
// that means anything, and they live in the bytes, not here.
type Template struct {
	ID      TemplateID
	Path    string
	RelPath string
}

// Ref returns the template's handle.
func (t Template) Ref() Ref { return Ref{Kind: KindTemplate, ID: string(t.ID)} }

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

// Binding is one file under bindings/ — a saved composition.
//
// The per-file format is not pinned by Cairn and is owned by a later task, so
// this type carries no fields derived from a binding's contents. bindings/ does
// not exist in the bundle today; that enumerates as empty, not as an error.
type Binding struct {
	// Name is the file's basename, extension included.
	Name    BindingID
	Path    string
	RelPath string
}

// Ref returns the binding's handle.
func (b Binding) Ref() Ref { return Ref{Kind: KindBinding, ID: string(b.Name)} }

// Contents is one enumeration of the whole bundle, grouped by kind.
//
// Every field is non-nil after a successful [Bundle.Contents], and empty for a
// kind whose directory is absent.
type Contents struct {
	Profiles  []Profile
	RoleProse []RoleProse
	Templates []Template
	Skills    []Skill
	Hooks     []Hook
	Bindings  []Binding
}
