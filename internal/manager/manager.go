// Package manager is the manager window's surface to the frontend: a tree
// over the agent-setup bundle grouped by artifact kind, and open/save for one
// artifact's bytes.
//
// It is a thin JSON-shaped wrapper around [bundle.Bundle] and nothing more.
// Every method re-opens the bundle at its persisted (or default) root and
// re-reads the tree from disk — [bundle.Bundle] caches nothing, so a file
// that appears, disappears or changes outside Tachyon is visible on the very
// next call, with no watcher and no manual invalidation to get wrong.
//
// # Bytes in, bytes out
//
// [Service.Open] returns exactly what [bundle.Bundle.Read] returns, and
// [Service.Save] passes exactly what it is given to [bundle.Bundle.Write].
// Nothing here parses, reformats or validates content (D5, D8). Content is
// carried as []byte, which Go's encoding/json — the same encoder Wails uses
// to answer a frontend call — encodes as a base64 string automatically. That
// is deliberate: transporting the bytes through a JSON string field directly
// would require them to already be valid UTF-8, and nothing in this package
// or in bundle guarantees that of an arbitrary hook script or role prose
// file. Base64 makes the transport byte-safe regardless.
//
// # The two "architect"s
//
// [bundle.KindProfile] and [bundle.KindRoleProse] share every basename in the
// bundle (profiles/architect.md, templates/roles/architect.md). Being in
// different [Group]s in the tree is not sufficient on its own — see plan
// CW-20260518-0061, task CW-20260903-0009. Every [Node] and every [Content]
// this package returns carries both Kind and RelPath, so a UI can label a row
// or a tab with "profile · profiles/architect.md" versus
// "role prose · templates/roles/architect.md" without the viewer having to
// remember which group they clicked into.
package manager

import (
	"fmt"
	"path/filepath"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/skeleton"
)

// Service is bound to the frontend as a Wails service. Its exported methods
// are callable from JavaScript as
// "github.com/hollis-labs/tachyon/internal/manager.Service.<Method>".
type Service struct {
	store bundle.RootStore
}

// New returns a Service that reads and writes whichever bundle store
// resolves to. store is usually [bundle.DefaultRootStore]; a test passes one
// rooted in a temp directory instead.
func New(store bundle.RootStore) *Service {
	return &Service{store: store}
}

// open resolves the active bundle root and opens it fresh. Called at the top
// of every exported method — see the package doc for why nothing is cached.
func (s *Service) open() (*bundle.Bundle, error) {
	root, err := s.store.Resolve()
	if err != nil {
		return nil, err
	}
	return bundle.Open(root)
}

// KindLabel is the human-readable label for one of [bundle.Kind]'s values, in
// the singular ("Profile") and the plural used for a tree group's heading
// ("Profiles"). Order matches [bundle.Kinds].
type KindLabel struct {
	Kind     bundle.Kind `json:"kind"`
	Singular string      `json:"singular"`
	Plural   string      `json:"plural"`
}

var kindLabels = map[bundle.Kind]KindLabel{
	bundle.KindProfile:   {bundle.KindProfile, "Profile", "Profiles"},
	bundle.KindRoleProse: {bundle.KindRoleProse, "Role prose", "Role prose"},
	bundle.KindTemplate:  {bundle.KindTemplate, "Template", "Templates"},
	bundle.KindSkill:     {bundle.KindSkill, "Skill", "Skills"},
	bundle.KindHook:      {bundle.KindHook, "Hook", "Hooks"},
	bundle.KindBinding:   {bundle.KindBinding, "Binding", "Bindings"},
}

// labelFor never returns the zero value for one of the six known kinds —
// kindLabels is exhaustive over [bundle.Kinds] and a test pins that.
func labelFor(k bundle.Kind) KindLabel {
	if l, ok := kindLabels[k]; ok {
		return l
	}
	return KindLabel{Kind: k, Singular: string(k), Plural: string(k)}
}

// HeaderView is the display projection of a frontmatter scan, reshaped for
// JSON with lowerCamelCase field names. It carries the same "absent vs.
// unreadable" distinction as [bundle.Header] — see that type's documentation.
type HeaderView struct {
	Present     bool   `json:"present"`
	Unreadable  bool   `json:"unreadable"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Extends     string `json:"extends"`
}

func headerView(h bundle.Header) *HeaderView {
	return &HeaderView{
		Present:     h.Present,
		Unreadable:  h.Unreadable,
		ID:          h.ID,
		Name:        h.Name,
		Description: h.Description,
		Extends:     h.Extends,
	}
}

// Node is one row in the tree: enough to label it, and enough to open it.
type Node struct {
	Kind    bundle.Kind `json:"kind"`
	ID      string      `json:"id"`
	Path    string      `json:"path"`
	RelPath string      `json:"relPath"`
	// Header is present only for kinds that carry frontmatter for display
	// (profile, skill). It is nil, not a zero HeaderView, for every other
	// kind — a nil Header means "this kind never has one", where a non-nil
	// Header with Present: false means "this file has none".
	Header *HeaderView `json:"header,omitempty"`
	// Note is a short caveat about the row, e.g. a skill directory missing
	// its SKILL.md. Empty when there is nothing to say.
	Note string `json:"note,omitempty"`
}

// Group is one artifact kind's section of the tree.
type Group struct {
	Kind  bundle.Kind `json:"kind"`
	Label string      `json:"label"`
	Nodes []Node      `json:"nodes"`
	Count int         `json:"count"`
}

// Tree is one enumeration of the whole bundle, grouped by kind, in the
// stable order [bundle.Kinds] defines. A kind whose directory is absent
// still appears, as a group with zero nodes — "no bindings yet" is a fact
// about the bundle, not an error, and the row-of-six should not shrink to a
// row-of-five because one directory does not exist today.
type Tree struct {
	// Root is the bundle root this tree was read from, absolute. Surfaced so
	// the manager can show the user which bundle they are editing.
	Root   string  `json:"root"`
	Groups []Group `json:"groups"`
	// State distinguishes, at the scope Tree can actually speak to, two
	// situations that both enumerate as all-empty groups and both leave
	// err nil (CW-20260904-0019): "ok" means the root looks like a real
	// bundle — [bundle.Bundle.HasKnownShape] found at least one of the
	// five known artifact directories under it — possibly still all-empty,
	// when there is genuinely nothing in it yet. "unrecognized" means the
	// root exists and is readable but has none of them: the shape check
	// (D8 — directory names only, never a file's content) that tells a
	// genuinely empty bundle apart from a directory that was never a
	// bundle at all (a home directory, a Desktop, a typo in a path). This
	// mirrors internal/binding.ListResult's three-state shape for
	// bindings/ specifically, at the whole-bundle scope Tree owns.
	//
	// A root that does not exist, or is not a directory, never reaches
	// State at all: [Service.Tree] returns a non-nil error instead
	// ([bundle.ErrRootMissing], naming the path), the same as it always
	// has — there is no readable directory to describe a shape for, and
	// that case was already distinguishable before this field existed.
	State string `json:"state"`
}

// Tree enumerates the active bundle, grouped by kind.
func (s *Service) Tree() (Tree, error) {
	b, err := s.open()
	if err != nil {
		return Tree{}, err
	}
	c, err := b.Contents()
	if err != nil {
		return Tree{}, err
	}

	// t.Groups is never nil: the loop below runs once per bundle.Kinds(), a
	// fixed six, and always appends. g.Nodes needs the same guarantee spelled
	// out explicitly — nothing here ever appends to it for a kind with zero
	// members (bindings/ does not exist in the live bundle today), and a nil
	// []Node marshals to JSON `null`, not `[]`. That is not cosmetic: the
	// frontend calls .map() and reads .length on every group's nodes
	// unconditionally, and a `null` there throws during render — with no
	// error boundary in place, that unmounts the whole tree and the manager
	// opens to a blank window. Every kind with zero members must still
	// produce `"nodes":[]` on the wire.
	t := Tree{Root: b.Root(), Groups: make([]Group, 0, len(bundle.Kinds())), State: "ok"}
	if !b.HasKnownShape() {
		t.State = "unrecognized"
	}
	for _, k := range bundle.Kinds() {
		g := Group{Kind: k, Label: labelFor(k).Plural, Nodes: []Node{}}
		switch k {
		case bundle.KindProfile:
			for _, p := range c.Profiles {
				g.Nodes = append(g.Nodes, Node{
					Kind: k, ID: string(p.ID), Path: p.Path, RelPath: p.RelPath,
					Header: headerView(p.Header),
				})
			}
		case bundle.KindRoleProse:
			for _, r := range c.RoleProse {
				g.Nodes = append(g.Nodes, Node{
					Kind: k, ID: string(r.Role), Path: r.Path, RelPath: r.RelPath,
				})
			}
		case bundle.KindTemplate:
			for _, tpl := range c.Templates {
				g.Nodes = append(g.Nodes, Node{
					Kind: k, ID: string(tpl.ID), Path: tpl.Path, RelPath: tpl.RelPath,
				})
			}
		case bundle.KindSkill:
			for _, sk := range c.Skills {
				n := Node{Kind: k, ID: string(sk.Name), Path: sk.Path, RelPath: sk.RelPath}
				if sk.HasSkillFile {
					n.Header = headerView(sk.Header)
				} else {
					n.Note = "no SKILL.md"
				}
				g.Nodes = append(g.Nodes, n)
			}
		case bundle.KindHook:
			for _, h := range c.Hooks {
				g.Nodes = append(g.Nodes, Node{
					Kind: k, ID: string(h.Name), Path: h.Path, RelPath: h.RelPath,
				})
			}
		case bundle.KindBinding:
			for _, bd := range c.Bindings {
				g.Nodes = append(g.Nodes, Node{
					Kind: k, ID: string(bd.Name), Path: bd.Path, RelPath: bd.RelPath,
				})
			}
		}
		g.Count = len(g.Nodes)
		t.Groups = append(t.Groups, g)
	}
	return t, nil
}

// Content is one artifact's bytes plus enough identity to label an editor tab
// unambiguously — see the package doc's note on the two "architect"s.
type Content struct {
	Kind    bundle.Kind `json:"kind"`
	ID      string      `json:"id"`
	Path    string      `json:"path"`
	RelPath string      `json:"relPath"`
	// Bytes are exactly what is on disk (D5). Marshaled as base64 by
	// encoding/json — see the package doc.
	Bytes []byte `json:"bytes"`
}

// Open returns one artifact's bytes exactly as they sit on disk.
func (s *Service) Open(kind, id string) (Content, error) {
	b, err := s.open()
	if err != nil {
		return Content{}, err
	}
	ref := bundle.Ref{Kind: bundle.Kind(kind), ID: id}
	data, err := b.Read(ref)
	if err != nil {
		return Content{}, err
	}
	return s.describe(b, ref, data)
}

// Save writes bytes back to one artifact, unchanged, and returns the same
// shape [Service.Open] would — so the frontend can confirm what actually
// landed without a second round trip.
//
// Nothing is validated (D8): whatever the caller sends is what is written.
func (s *Service) Save(kind, id string, content []byte) (Content, error) {
	b, err := s.open()
	if err != nil {
		return Content{}, err
	}
	ref := bundle.Ref{Kind: bundle.Kind(kind), ID: id}
	if err := b.Write(ref, content); err != nil {
		return Content{}, err
	}
	return s.describe(b, ref, content)
}

func (s *Service) describe(b *bundle.Bundle, ref bundle.Ref, data []byte) (Content, error) {
	path, err := b.Resolve(ref)
	if err != nil {
		return Content{}, err
	}
	rel, err := filepath.Rel(b.Root(), path)
	if err != nil {
		return Content{}, fmt.Errorf("manager: relativizing %s: %w", path, err)
	}
	if data == nil {
		// A nil []byte and an empty []byte are the same content (a
		// zero-byte file) but not the same JSON: encoding/json marshals a
		// nil []byte as `null`, an empty one as `""`. Content.Bytes must
		// always be the latter — see Tree's identical guarantee for Nodes,
		// and the package doc's note on why this is checked at every
		// aggregation point, not just the one a reviewer happened to find.
		data = []byte{}
	}
	return Content{
		Kind:    ref.Kind,
		ID:      ref.ID,
		Path:    path,
		RelPath: filepath.ToSlash(rel),
		Bytes:   data,
	}, nil
}

// Root returns the bundle root the manager is currently reading, so the
// window can show the user which bundle they are editing without paying for
// a full Tree call.
func (s *Service) Root() (string, error) {
	return s.store.Resolve()
}

// SetRoot changes the active bundle root and persists it — [bundle.RootStore
// .Save] bound to the frontend (CW-20260904-0019). See that method's own
// doc for exactly what it does: an atomic write to the settings file under
// state.Dir(), never anything under either bundle root.
//
// Every method on this Service — and on internal/binding.Service, which
// shares the same [bundle.RootStore] — resolves the active root fresh on
// every call rather than caching it (see the package doc), so a change
// made here is visible to the very next Tree/Open/Save/List call with no
// restart and no further plumbing.
//
// SetRoot does not validate that root is a real bundle (D8): a path typed
// by hand and a folder chosen through a native picker are the same case
// from this method's point of view. The caller finds out what it got from
// the next [Service.Tree] call's State.
func (s *Service) SetRoot(root string) error {
	return s.store.Save(root)
}

// DefaultRoot returns the bundle Tachyon opens when nothing has been
// chosen — the same value [bundle.DefaultRoot] computes, expanded and
// absolute. It exists so the manager's "reset to default" affordance never
// hardcodes [bundle.DefaultRootPath] a second time in JavaScript, which
// would risk drifting from the one Go source of truth for it.
func (s *Service) DefaultRoot() (string, error) {
	return bundle.DefaultRoot()
}

// --- CW-20260903-0010: the "new artifact" entry point ---
//
// The two methods below are this task's one deliberately narrow addition to
// this service. Everything above this line is T05's (CW-20260903-0009) and
// is untouched; the scaffold content itself, and the dispatch table a future
// binding case extends, live entirely in internal/skeleton — see that
// package's doc for the seam CW-20260903-0011 (T07) will fill in.

// NewArtifactKinds reports which kinds [Service.NewArtifact] can create
// today, so the frontend's "new artifact" picker does not hardcode a list
// that could drift from what actually works. bundle.KindBinding is
// deliberately absent — see internal/skeleton's package doc — and reappears
// here automatically once that package's registry gains an entry for it, no
// frontend change required.
func (s *Service) NewArtifactKinds() []bundle.Kind {
	return skeleton.SupportedKinds()
}

// NewArtifact creates a brand-new artifact from a kind-appropriate skeleton
// (internal/skeleton) and returns it in the same [Content] shape
// [Service.Open] and [Service.Save] do, so the frontend can open the result
// directly in the editor without a second round trip.
//
// name and description are used only by the kinds whose scaffold has
// somewhere to put them (see [skeleton.Spec]); passing them for a kind that
// ignores them is harmless.
func (s *Service) NewArtifact(kind, id, name, description string) (Content, error) {
	b, err := s.open()
	if err != nil {
		return Content{}, err
	}
	ref, err := skeleton.New(b.Root(), skeleton.Spec{
		Kind:        bundle.Kind(kind),
		ID:          id,
		Name:        name,
		Description: description,
	})
	if err != nil {
		return Content{}, err
	}
	data, err := b.Read(ref)
	if err != nil {
		return Content{}, err
	}
	return s.describe(b, ref, data)
}
