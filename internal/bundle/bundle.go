package bundle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Directory and file names the bundle layout is made of.
const (
	dirProfiles  = "profiles"
	dirTemplates = "templates"
	dirRoles     = "roles" // under dirTemplates
	dirPrompts   = "prompts"
	dirSkills    = "skills"
	dirHooks     = "hooks"
	dirBindings  = "bindings"

	extMarkdown = ".md"
	extShell    = ".sh"
	skillFile   = "SKILL.md"
)

// ErrNotFound is returned by [Bundle.Read] and [Bundle.Resolve] when a [Ref]
// names nothing in the bundle. Test for it with errors.Is.
var ErrNotFound = errors.New("bundle: artifact not found")

// ErrRootMissing reports that the bundle root itself is gone, or that
// something that is not a directory now stands where it was. Test for it with
// errors.Is.
//
// An artifact directory that does not exist enumerates as empty — bindings/
// does not exist in the bundle today and that is not a fault. The root is the
// one place where that rule must not apply: a user who renames the repo,
// switches branches or types the path wrong would otherwise get a clean,
// error-free, completely empty bundle. An empty tree meaning "your bundle is
// gone" and an empty tree meaning "your bundle is empty" must not be the same
// value, so the root is re-checked whenever a directory read fails and its
// absence is an error rather than a silence.
var ErrRootMissing = errors.New("bundle: root is missing")

// Bundle is a rooted view of one agent-setup bundle. It holds no cached
// contents: every enumeration reads the tree, because the user edits it out
// from under us with an editor and a git checkout.
type Bundle struct {
	root string
}

// Open returns a Bundle rooted at the given directory.
//
// The root is a parameter, not a constant — see [DefaultRoot] and [RootStore]
// for where a caller usually gets one. The root itself must exist and be a
// directory; the artifact directories inside it need not, and a missing one
// enumerates as empty.
//
// A leading "~/" is expanded, and the path is made absolute, so a root read
// back out of a config file behaves the same as one typed at a terminal.
func Open(root string) (*Bundle, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("bundle: root is empty")
	}
	abs, err := ExpandRoot(root)
	if err != nil {
		return nil, err
	}
	b := &Bundle{root: abs}
	if err := b.checkRoot(); err != nil {
		return nil, err
	}
	return b, nil
}

// checkRoot reports whether the root is still a directory we can see.
//
// It is called on the failure path of every directory read, so that a root
// that has gone away is never reported as an empty bundle. A root we can see
// but cannot enter is a permission problem and is reported as itself, not as
// a missing root.
func (b *Bundle) checkRoot() error {
	info, err := os.Stat(b.root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s: %w", ErrRootMissing, b.root, err)
	case err != nil:
		return fmt.Errorf("bundle: root %s: %w", b.root, err)
	case !info.IsDir():
		return fmt.Errorf("%w: %s is not a directory", ErrRootMissing, b.root)
	}
	return nil
}

// ExpandRoot expands a leading "~/" and makes the path absolute, without
// requiring that it exist. [Open] applies it; a caller validating user input
// before opening can apply it too.
func ExpandRoot(root string) (string, error) {
	p := strings.TrimSpace(root)
	if p == "" {
		return "", errors.New("bundle: root is empty")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("bundle: expanding %q: %w", root, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("bundle: resolving %q: %w", root, err)
	}
	return abs, nil
}

// Root is the absolute directory this Bundle reads.
func (b *Bundle) Root() string { return b.root }

// shapeDirs are the top-level directory names whose presence under a root
// is what [Bundle.HasKnownShape] checks for. dirRoles is deliberately
// excluded: it nests under dirTemplates rather than sitting at the root
// itself, so its presence or absence says nothing about the root.
var shapeDirs = []string{dirProfiles, dirTemplates, dirPrompts, dirSkills, dirHooks, dirBindings}

// HasKnownShape reports whether the root looks like a bundle at all: does
// at least one of the six top-level artifact directories this package
// knows about exist directly under it?
//
// This is a shape check, not a content check (D8): it only looks at
// directory names, never opens a file inside any of them, and is
// satisfied by just one of the six being present. Its whole job is
// telling apart two roots that both enumerate as "zero artifacts
// everywhere" — a bundle that is genuinely empty (created, but nothing
// has been added to it yet: profiles/ exists and has no files in it, say)
// from a directory that was never a bundle to begin with (a home
// directory, a Desktop, a typo in a path). [Bundle.Contents] and every
// per-kind enumeration answer both the same way today, by design — "a
// missing artifact directory enumerates as empty" — and that stays true
// here: this method exists so a caller ABOVE that layer (see
// internal/manager.Service.Tree) can still tell the two situations apart
// and say so, without this package's own enumeration semantics changing
// at all.
//
// It does not re-check that the root itself exists: a caller that already
// has a *Bundle already went through [Open], which would have failed with
// [ErrRootMissing] otherwise — this only ever answers "which of the known
// directories exist under it," nothing about the root's own existence.
func (b *Bundle) HasKnownShape() bool {
	for _, name := range shapeDirs {
		info, err := os.Stat(b.abs(name))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// Profiles enumerates profiles/*.md, sorted by id.
//
// Each profile's frontmatter is read shallowly for display. A file whose
// frontmatter is absent or unreadable still appears, with an empty [Header];
// nothing here validates content (D8).
func (b *Bundle) Profiles() ([]Profile, error) {
	names, err := b.listFiles(extMarkdown, dirProfiles)
	if err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(names))
	for _, name := range names {
		id := strings.TrimSuffix(name, extMarkdown)
		rel := path.Join(dirProfiles, name)
		out = append(out, Profile{
			ID:      ProfileID(id),
			Path:    b.abs(rel),
			RelPath: rel,
			Header:  b.readHeader(rel),
		})
	}
	return out, nil
}

// RoleProse enumerates templates/roles/*.md, sorted by role.
//
// These share every basename with a profile. They are not profiles.
func (b *Bundle) RoleProse() ([]RoleProse, error) {
	names, err := b.listFiles(extMarkdown, dirTemplates, dirRoles)
	if err != nil {
		return nil, err
	}
	out := make([]RoleProse, 0, len(names))
	for _, name := range names {
		rel := path.Join(dirTemplates, dirRoles, name)
		out = append(out, RoleProse{
			Role:    RoleProseID(strings.TrimSuffix(name, extMarkdown)),
			Path:    b.abs(rel),
			RelPath: rel,
		})
	}
	return out, nil
}

// Templates enumerates templates/*.md, sorted by id. The roles/ subdirectory
// is not included; see [Bundle.RoleProse].
func (b *Bundle) Templates() ([]Template, error) {
	names, err := b.listFiles(extMarkdown, dirTemplates)
	if err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(names))
	for _, name := range names {
		rel := path.Join(dirTemplates, name)
		out = append(out, Template{
			ID:      TemplateID(strings.TrimSuffix(name, extMarkdown)),
			Path:    b.abs(rel),
			RelPath: rel,
		})
	}
	return out, nil
}

// Prompts enumerates prompts/*.md, sorted by id.
//
// Flat files, like [Bundle.Templates] — simpler, in fact, since there is no
// roles/-style subdirectory to exclude. Nothing here reads a prompt's body:
// unlike a profile or a skill, a prompt carries no frontmatter this package
// scans for display, so there is no Header field to populate. See
// [Prompt]'s own doc for why: a prompt is handled only as bytes, with no
// delivery path in Tachyon at all.
func (b *Bundle) Prompts() ([]Prompt, error) {
	names, err := b.listFiles(extMarkdown, dirPrompts)
	if err != nil {
		return nil, err
	}
	out := make([]Prompt, 0, len(names))
	for _, name := range names {
		rel := path.Join(dirPrompts, name)
		out = append(out, Prompt{
			ID:      PromptID(strings.TrimSuffix(name, extMarkdown)),
			Path:    b.abs(rel),
			RelPath: rel,
		})
	}
	return out, nil
}

// Skills enumerates the directories under skills/, sorted by name.
//
// A directory missing its SKILL.md still enumerates, with HasSkillFile false —
// existence is reported, not enforced.
func (b *Bundle) Skills() ([]Skill, error) {
	names, err := b.listDirs(dirSkills)
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		rel := path.Join(dirSkills, name, skillFile)
		abs := b.abs(rel)
		info, statErr := os.Stat(abs)
		has := statErr == nil && info.Mode().IsRegular()
		s := Skill{
			Name:         SkillID(name),
			Dir:          b.abs(path.Join(dirSkills, name)),
			Path:         abs,
			RelPath:      rel,
			HasSkillFile: has,
		}
		if has {
			s.Header = b.readHeader(rel)
		}
		out = append(out, s)
	}
	return out, nil
}

// Hooks enumerates hooks/*.sh, sorted by name.
func (b *Bundle) Hooks() ([]Hook, error) {
	names, err := b.listFiles(extShell, dirHooks)
	if err != nil {
		return nil, err
	}
	out := make([]Hook, 0, len(names))
	for _, name := range names {
		rel := path.Join(dirHooks, name)
		out = append(out, Hook{
			Name:    HookID(strings.TrimSuffix(name, extShell)),
			Path:    b.abs(rel),
			RelPath: rel,
		})
	}
	return out, nil
}

// Bindings enumerates the files directly under bindings/, sorted by name.
//
// An absent directory is an empty result, not an error, same as every other
// artifact directory. Nothing here reads or interprets a binding's
// contents — see internal/binding for the package that does.
func (b *Bundle) Bindings() ([]Binding, error) {
	names, err := b.listFiles("", dirBindings)
	if err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(names))
	for _, name := range names {
		rel := path.Join(dirBindings, name)
		out = append(out, Binding{
			Name:    BindingID(name),
			Path:    b.abs(rel),
			RelPath: rel,
		})
	}
	return out, nil
}

// Contents enumerates all seven artifact kinds in one pass.
func (b *Bundle) Contents() (Contents, error) {
	var c Contents
	var err error
	if c.Profiles, err = b.Profiles(); err != nil {
		return Contents{}, err
	}
	if c.RoleProse, err = b.RoleProse(); err != nil {
		return Contents{}, err
	}
	if c.Templates, err = b.Templates(); err != nil {
		return Contents{}, err
	}
	if c.Prompts, err = b.Prompts(); err != nil {
		return Contents{}, err
	}
	if c.Skills, err = b.Skills(); err != nil {
		return Contents{}, err
	}
	if c.Hooks, err = b.Hooks(); err != nil {
		return Contents{}, err
	}
	if c.Bindings, err = b.Bindings(); err != nil {
		return Contents{}, err
	}
	return c, nil
}

// Read returns the artifact's bytes exactly as they are on disk.
//
// Nothing is parsed, decoded, re-encoded or normalized on this path: what
// comes back is byte-for-byte what os.ReadFile would return for the same file
// (D5). For a skill, the bytes are its SKILL.md.
//
// The ref is resolved by matching an enumerated artifact, never by joining the
// id onto a path, so an id carrying separators or ".." resolves to nothing
// rather than to a file outside its directory.
func (b *Bundle) Read(ref Ref) ([]byte, error) {
	p, err := b.Resolve(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err == nil {
		return data, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		// The entry was listed and then could not be opened: a broken symlink,
		// or a file deleted between the listing and the read. The root is
		// checked first so that "the bundle is gone" never arrives dressed up
		// as "you named the wrong artifact", and what is left collapses onto
		// ErrNotFound so a caller has one absence to handle rather than two.
		if rootErr := b.checkRoot(); rootErr != nil {
			return nil, rootErr
		}
		return nil, fmt.Errorf("%w: %s %q: %w", ErrNotFound, ref.Kind, ref.ID, err)
	}
	return nil, err
}

// Write replaces an artifact's bytes exactly as given.
//
// Nothing is parsed, decoded, re-encoded, validated or normalized on this
// path — no YAML AST, no formatter, no trailing-whitespace trim, no newline
// normalization (D5). Write is Read's mirror: what a caller hands in is what
// ends up on disk, byte for byte, or the write fails and nothing on disk
// changes.
//
// The ref must already resolve to a file the bundle enumerates — Write edits
// an existing artifact, it does not create a new one by name. A caller that
// wants a brand new file creates it directly (e.g. with os.WriteFile) and
// lets the next enumeration find it; that keeps "which files exist" answered
// in exactly one place, the directory listing, rather than also in this
// package's idea of a valid id.
//
// The write is atomic: the new bytes land in a temporary file in the same
// directory, then an os.Rename replaces the original in one step, so a crash
// or a concurrent read mid-write never observes a half-written file. The
// original file's permission bits are preserved when they can be read, and
// default to 0o644 otherwise — Write only ever touches content, never mode.
func (b *Bundle) Write(ref Ref, data []byte) error {
	p, err := b.Resolve(ref)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(p); statErr == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(p)
	tmp, err := os.CreateTemp(dir, ".tachyon-*.tmp")
	if err != nil {
		return fmt.Errorf("bundle: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("bundle: writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("bundle: closing %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("bundle: setting mode on %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("bundle: replacing %s: %w", p, err)
	}
	ok = true
	return nil
}

// Resolve returns the absolute path a ref names, without reading it.
//
// It returns [ErrNotFound] when nothing in the bundle answers to the ref —
// including a skill directory that exists but has no SKILL.md — and
// [ErrRootMissing] when the bundle root has gone away.
//
// The path comes from a directory listing, so it can still name a broken
// symlink. [Bundle.Read] is where that surfaces, also as [ErrNotFound].
func (b *Bundle) Resolve(ref Ref) (string, error) {
	switch ref.Kind {
	case KindProfile:
		return b.resolveFile(ref, extMarkdown, dirProfiles)
	case KindRoleProse:
		return b.resolveFile(ref, extMarkdown, dirTemplates, dirRoles)
	case KindTemplate:
		return b.resolveFile(ref, extMarkdown, dirTemplates)
	case KindPrompt:
		return b.resolveFile(ref, extMarkdown, dirPrompts)
	case KindHook:
		return b.resolveFile(ref, extShell, dirHooks)
	case KindBinding:
		// The binding id carries its own extension, because the format is not
		// pinned and the extension is part of the name.
		return b.resolveFile(ref, "", dirBindings)
	case KindSkill:
		dirs, err := b.listDirs(dirSkills)
		if err != nil {
			return "", err
		}
		for _, name := range dirs {
			if name != ref.ID {
				continue
			}
			abs := b.abs(path.Join(dirSkills, name, skillFile))
			info, statErr := os.Stat(abs)
			if statErr != nil || !info.Mode().IsRegular() {
				return "", fmt.Errorf("%w: %s %q has no %s", ErrNotFound, ref.Kind, ref.ID, skillFile)
			}
			return abs, nil
		}
		return "", fmt.Errorf("%w: %s %q", ErrNotFound, ref.Kind, ref.ID)
	default:
		return "", fmt.Errorf("bundle: unknown artifact kind %q", ref.Kind)
	}
}

// resolveFile finds the file in dir whose basename is ref.ID plus ext.
func (b *Bundle) resolveFile(ref Ref, ext string, dir ...string) (string, error) {
	names, err := b.listFiles(ext, dir...)
	if err != nil {
		return "", err
	}
	want := ref.ID + ext
	for _, name := range names {
		if name == want {
			return b.abs(path.Join(append(append([]string{}, dir...), name)...)), nil
		}
	}
	return "", fmt.Errorf("%w: %s %q", ErrNotFound, ref.Kind, ref.ID)
}

// listFiles returns the sorted basenames of the non-directory entries directly
// under the given subdirectory, filtered by extension when ext is non-empty.
//
// A missing directory is an empty result, not an error. Dotfiles are skipped:
// .DS_Store is real and lives in this bundle.
func (b *Bundle) listFiles(ext string, dir ...string) ([]string, error) {
	entries, err := b.readDir(dir...)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if b.entryIsDir(e, append(append([]string{}, dir...), name)...) {
			continue
		}
		if ext != "" && !strings.HasSuffix(name, ext) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// listDirs returns the sorted names of the directories directly under the
// given subdirectory. A missing directory is an empty result.
func (b *Bundle) listDirs(dir ...string) ([]string, error) {
	entries, err := b.readDir(dir...)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !b.entryIsDir(e, append(append([]string{}, dir...), name)...) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (b *Bundle) readDir(dir ...string) ([]os.DirEntry, error) {
	rel := path.Join(dir...)
	entries, err := os.ReadDir(b.abs(rel))
	if err == nil {
		return entries, nil
	}
	// Before an absent directory is reported as "nothing here", make sure the
	// bundle itself is still there. Every enumeration, Resolve and Read pass
	// through here, so this one guard is what keeps a vanished root from
	// reading as an empty bundle everywhere at once.
	if rootErr := b.checkRoot(); rootErr != nil {
		return nil, rootErr
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return nil, fmt.Errorf("bundle: reading %s: %w", rel, err)
}

// entryIsDir reports whether the entry is a directory, following a symlink to
// find out. Bundles get symlinked around; a symlinked skill directory should
// still read as a skill.
func (b *Bundle) entryIsDir(e os.DirEntry, rel ...string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(b.abs(path.Join(rel...)))
	return err == nil && info.IsDir()
}

// abs joins a slash-separated bundle-relative path onto the root.
func (b *Bundle) abs(rel string) string {
	return filepath.Join(b.root, filepath.FromSlash(rel))
}

// readHeader reads a file's frontmatter for display.
//
// It never fails the enumeration — an artifact hidden from the tree is an
// artifact that cannot be fixed (D8) — but it does distinguish the two ways a
// Header can come back empty: a file with no frontmatter, and a file that
// could not be read at all.
func (b *Bundle) readHeader(rel string) Header {
	data, err := os.ReadFile(b.abs(rel))
	if err != nil {
		return Header{Unreadable: true}
	}
	return ScanHeader(data)
}
