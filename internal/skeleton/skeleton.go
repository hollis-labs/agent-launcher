package skeleton

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/bundle"
)

// Spec describes one new artifact to create. Every kind uses ID; Name and
// Description are used only by the kinds whose scaffold has somewhere to put
// them (today: [bundle.KindProfile] and [bundle.KindSkill]'s description) and
// are silently ignored by the others.
type Spec struct {
	// Kind is which of [SupportedKinds] to create.
	Kind bundle.Kind
	// ID is the artifact's file basename without extension — or, for
	// bundle.KindSkill, the directory name under skills/. It becomes part of
	// a path and part of a YAML scalar, so it is restricted: see
	// [validateID].
	ID string
	// Name is a display name, used by the profile scaffold. Defaults to a
	// title-cased ID when empty (architect -> Architect), matching how this
	// bundle's own profiles read.
	Name string
	// Description is free text, written into the scaffold's frontmatter. It
	// is quoted and escaped as needed so that arbitrary user-typed text — a
	// colon, a leading "#", an embedded quote — cannot produce invalid YAML;
	// see [scalarLine].
	Description string
}

// Errors [New] returns. Test against these with errors.Is; the message alone
// is not a stable contract.
var (
	// ErrInvalidID reports that Spec.ID does not match [idPattern].
	ErrInvalidID = errors.New("skeleton: invalid id")
	// ErrKindNotSupported reports a kind [registry] has no entry for —
	// bundle.KindHook today, deliberately (hook creation is out of scope
	// entirely; see the package doc). bundle.KindBinding used to be one of
	// these too, before CW-20260904-0002 (T23) gave it a real scaffold.
	ErrKindNotSupported = errors.New("skeleton: kind not supported")
	// ErrAlreadyExists reports that the target path is already occupied. New
	// creates; it never overwrites — a caller that wants to edit an existing
	// artifact uses internal/manager's Open/Save instead.
	ErrAlreadyExists = errors.New("skeleton: already exists")
)

// idPattern is the character set every id in this bundle already uses:
// architect, capture-decision, end-of-session, search-first, qhealth,
// pull-request, blg — lowercase words, digits and hyphens. New enforces it
// for two structural reasons that have nothing to do with D8's "no content
// validation": an id becomes a filename (so "/", "..", a leading "." that
// internal/bundle's own listings skip, and other path-meaningful characters
// must be refused), and it is written as a plain, unquoted YAML scalar in
// "id: <id>" (so a colon or leading special character must be refused there
// too, rather than silently producing a frontmatter block nothing here ever
// re-checks). Upper case is accepted since nothing in the live bundle uses
// it but nothing about the format forbids it either.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// entry is one artifact kind's scaffold: where it lands, and what it starts
// out containing.
type entry struct {
	// relPath returns the bundle-relative, slash-separated path a new
	// artifact of this kind lands at, given its id.
	relPath func(id string) string
	// scaffold renders the file's full initial content.
	scaffold func(spec Spec) []byte
	// refID derives the ID [New] returns in its [bundle.Ref] from the
	// caller's own id. Nil (every kind but binding) means the identity —
	// the [bundle.Ref].ID a caller gets back is exactly the id it gave.
	// bundle.KindBinding is the one kind where a [bundle.Ref].ID carries
	// its file's extension (see bundle.BindingID's own doc: "the format is
	// not pinned and the extension is part of the name"), so its entry
	// sets this to append it — without New itself needing to know that
	// binding is special.
	refID func(id string) string
}

// registry is the seam.
//
// One entry per artifact kind [New] knows how to create today. bundle.KindBinding's
// entry (CW-20260904-0002 / T23) points at [binding.BindingRelPath] rather
// than a relPath function defined in this package — see scaffolds.go's
// bindingScaffold for why its starting content is not what
// internal/binding's own [binding.Store.Create] would write, and why that
// is still "the exact same on-disk shape" this package's own doc allowed
// for. Nothing else in this package — not New, not SupportedKinds — and
// nothing in internal/manager's NewArtifact/NewArtifactKinds wrapper needed
// to change to add it: the whole dispatch this package makes on kind is
// this one map.
//
// bundle.KindHook has no entry: hook creation stays out of this package's
// scope entirely (see the package doc), unrelated to bindings.
var registry = map[bundle.Kind]entry{
	bundle.KindProfile:   {relPath: profileRelPath, scaffold: profileScaffold},
	bundle.KindRoleProse: {relPath: roleProseRelPath, scaffold: roleProseScaffold},
	bundle.KindTemplate:  {relPath: templateRelPath, scaffold: templateScaffold},
	bundle.KindPrompt:    {relPath: promptRelPath, scaffold: promptScaffold},
	bundle.KindSkill:     {relPath: skillRelPath, scaffold: skillScaffold},
	bundle.KindBinding:   {relPath: binding.BindingRelPath, scaffold: bindingScaffold, refID: bindingRefID},
}

// SupportedKinds reports which kinds [New] can create today, in
// [bundle.Kinds]'s stable presentational order. bundle.KindHook is never in
// this list: hook creation is out of this package's scope entirely — see
// the package doc. bundle.KindBinding was excluded the same way before
// CW-20260904-0002 (T23) gave it a real scaffold; it is included now.
//
// A caller (internal/manager.Service.NewArtifactKinds, and through it the
// frontend's "new artifact" menu) uses this instead of hardcoding the list,
// so a future kind lighting up requires no frontend change: once
// [registry] gains an entry, it appears here automatically.
func SupportedKinds() []bundle.Kind {
	out := make([]bundle.Kind, 0, len(registry))
	for _, k := range bundle.Kinds() {
		if _, ok := registry[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// New creates a brand-new artifact in the bundle rooted at root, and returns
// its [bundle.Ref].
//
// This is the one place in Tachyon that authors a file whole — see the
// package doc's note on D5. It refuses to overwrite an existing file
// ([ErrAlreadyExists]); refuses an id [validateID] rejects ([ErrInvalidID]);
// and refuses a kind [registry] has no entry for ([ErrKindNotSupported],
// which today means only bundle.KindHook — see the package doc).
//
// root is opened the same way every other bundle operation opens it
// ([bundle.Open]), so a bad root reports [bundle.ErrRootMissing] here exactly
// as it would for a read or a save, rather than a bespoke error from this
// package.
//
// Nothing here shells out to Cairn and nothing here writes to
// ~/.config/agents or ~/.claude — only into the bundle root's own tree.
func New(root string, spec Spec) (bundle.Ref, error) {
	id := strings.TrimSpace(spec.ID)
	if err := validateID(id); err != nil {
		return bundle.Ref{}, err
	}
	spec.ID = id

	e, ok := registry[spec.Kind]
	if !ok {
		return bundle.Ref{}, fmt.Errorf("%w: %s", ErrKindNotSupported, spec.Kind)
	}

	b, err := bundle.Open(root)
	if err != nil {
		return bundle.Ref{}, err
	}

	rel := e.relPath(id)
	target := filepath.Join(b.Root(), filepath.FromSlash(rel))

	if _, statErr := os.Stat(target); statErr == nil {
		return bundle.Ref{}, fmt.Errorf("%w: %s", ErrAlreadyExists, rel)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return bundle.Ref{}, fmt.Errorf("skeleton: checking %s: %w", target, statErr)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return bundle.Ref{}, fmt.Errorf("skeleton: creating directory for %s: %w", rel, err)
	}

	// O_EXCL makes the create atomic against a second, concurrent New for the
	// same id: one wins, the other gets ErrAlreadyExists instead of silently
	// clobbering the first — the same hazard bundle.Bundle.Write's rename
	// guards against on the edit side, met here with the create-time
	// equivalent.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return bundle.Ref{}, fmt.Errorf("%w: %s", ErrAlreadyExists, rel)
		}
		return bundle.Ref{}, fmt.Errorf("skeleton: creating %s: %w", target, err)
	}
	wrote := false
	defer func() {
		if !wrote {
			os.Remove(target)
		}
	}()

	if _, err := f.Write(e.scaffold(spec)); err != nil {
		f.Close()
		return bundle.Ref{}, fmt.Errorf("skeleton: writing %s: %w", target, err)
	}
	if err := f.Close(); err != nil {
		return bundle.Ref{}, fmt.Errorf("skeleton: closing %s: %w", target, err)
	}
	wrote = true

	refID := id
	if e.refID != nil {
		refID = e.refID(id)
	}
	return bundle.Ref{Kind: spec.Kind, ID: refID}, nil
}

// validateID rejects any id that would not round-trip safely: empty, or not
// matching [idPattern]. See idPattern's comment for why the restriction is
// structural rather than a D8 content check.
func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: empty", ErrInvalidID)
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf(
			"%w: %q -- ids are letters, digits, \"-\" and \"_\" only, and must start with a "+
				"letter or digit, matching every id already in this bundle (architect, "+
				"capture-decision, end-of-session, search-first, ...)",
			ErrInvalidID, id)
	}
	return nil
}

// titleFromID renders a default display name from an id, e.g.
// "search-first" -> "Search First" and "architect" -> "Architect" — matching
// how this bundle's own profiles read.
func titleFromID(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// displayName returns spec.Name, or titleFromID(spec.ID) when it is blank.
func displayName(spec Spec) string {
	if strings.TrimSpace(spec.Name) != "" {
		return spec.Name
	}
	return titleFromID(spec.ID)
}
