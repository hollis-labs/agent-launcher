package launchprofile

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Ext is the extension every launch profile carries. Cairn reads a part by
// path and does not care about the extension, but a directory a person
// browses and edits should hold files their editor opens as Markdown.
const Ext = ".md"

// Profile is one launch profile, as a list needs it: enough to label a row
// and to hand Cairn a path.
type Profile struct {
	// Name is the id: the file's basename without [Ext]. It is what the
	// palette shows and what [Store] addresses a profile by.
	Name string `json:"name"`

	// Path is the file itself, absolute. This is the value that becomes
	// `--with <path>` — see internal/compose. Cairn treats a value holding
	// a path separator as a file rather than a catalog id, so an absolute
	// path is unambiguous by construction.
	Path string `json:"path"`

	// Provider is the frontmatter's `provider`, or "" when the file
	// declares none. Shown, never enforced: a launch profile that declares
	// no provider is refused by Cairn at the boot, naming what it does
	// know, and a second opinion here could only disagree with that one.
	Provider string `json:"provider"`

	// Description is the frontmatter's `description`, or "". Display only.
	Description string `json:"description"`
}

// ErrNotFound is returned by [Store.Get] and [Store.Read] when no launch
// profile of that name exists. Test for it with errors.Is.
var ErrNotFound = errors.New("launchprofile: not found")

// ErrExists is returned by [Store.Create] when one already does.
var ErrExists = errors.New("launchprofile: already exists")

// ErrDirMissing is returned by [Store.List] and [Store.Get] when the launch
// directory itself does not exist — distinct from [ErrNotFound] (a specific
// name absent from a directory that is there) and from the plain errors
// returned when the directory exists but cannot be read.
//
// The distinction is what lets the palette tell "you have not written a
// launch profile yet" from "this build cannot read the ones you have," which
// are the same empty list and very different sentences.
var ErrDirMissing = errors.New("launchprofile: launch directory not found")

// nameRe is what a launch profile's name must match. Deliberately stricter
// than the filesystem allows, because a name accepted here becomes a
// filename with no escaping: no separator, and no name that could ever spell
// ".." (the pattern requires an alphanumeric first byte).
//
// It is also [boot.Key]'s safeSeed pattern, which is not a coincidence — a
// launch profile's name is part of the boot directory's identity, and a name
// that passes here passes through Key as a readable path segment rather than
// a hash.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidateName reports whether name is safe to place in the store.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("launchprofile: invalid name %q: must match %s", name, nameRe.String())
	}
	return nil
}

// frontmatter is the narrow view [Parse] takes of a launch profile. The
// three fields are what a list needs; every other key Cairn accepts is
// deliberately absent, and yaml.v3 ignores what it is not asked for.
type frontmatter struct {
	ID          string `yaml:"id"`
	Provider    string `yaml:"provider"`
	Description string `yaml:"description"`
}

// Parse reads the display fields out of a launch profile's bytes.
//
// A document with no frontmatter, or frontmatter that does not parse, is not
// an error: it returns a zero [frontmatter] and lets the file through. That
// is deliberate — this function's job is to label a row, and a file it
// cannot label is still a file a person wrote and must be able to see and
// open in the manager. Handing back an error would make an unparseable
// launch profile invisible, which is the one outcome that makes it hard to
// fix. Cairn refuses it at the boot, where the refusal is actionable.
func Parse(data []byte) (name, provider, description string) {
	block, ok := frontmatterBlock(data)
	if !ok {
		return "", "", ""
	}
	var fm frontmatter
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return "", "", ""
	}
	return fm.ID, fm.Provider, fm.Description
}

// frontmatterBlock returns the bytes between the opening "---" line and the
// next "---" line, or ok=false when the document does not open with one.
func frontmatterBlock(data []byte) ([]byte, bool) {
	const fence = "---"
	// A UTF-8 BOM ahead of the fence is what a Windows editor leaves and
	// what makes an otherwise-correct file parse as having no frontmatter.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fence {
		return nil, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fence {
			return []byte(strings.Join(lines[1:i], "\n")), true
		}
	}
	return nil, false
}
