package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hollis-labs/tachyon/internal/state"
)

// DefaultRootPath is the bundle Tachyon opens when the user has not chosen
// one. It is a default, not a constant the read path depends on: every
// function in this package takes its root from [Open].
const DefaultRootPath = "~/dev/projects/agent-setup"

// DefaultRoot is [DefaultRootPath] expanded and made absolute.
func DefaultRoot() (string, error) { return ExpandRoot(DefaultRootPath) }

// RootStore persists which bundle is active. One bundle is active at a time.
//
// This is the only thing in this package that writes, and what it writes is
// Tachyon's own settings file — never bundle content. It lives here rather
// than in the shell so that the root stays a parameter of the bundle rather
// than a fact the UI happens to know.
type RootStore struct {
	// Path is the settings file. Its directory is created on save.
	Path string
}

// rootSettings is the on-disk shape. It is Tachyon's file, so this is the one
// place a struct is serialized — D5's "bytes in, bytes out" governs bundle
// content, which this is not.
type rootSettings struct {
	BundleRoot string `json:"bundle_root"`
}

// DefaultRootStore is the store under the user's config directory.
//
// This deliberately builds its own "tachyon" (lowercase) directory name
// under [state.Dir] rather than nesting inside [state.Root] ("Tachyon",
// capitalized): that casing mismatch predates this package's convergence
// onto internal/state and is preserved here byte-for-byte so an existing
// user's bundle.json is not silently relocated by this change. [state.Dir]
// is still the single place the per-user config directory itself is
// located, so overriding it moves this path too, in lockstep with
// [state.Root] and everything built on it.
func DefaultRootStore() (RootStore, error) {
	dir, err := state.Dir()
	if err != nil {
		return RootStore{}, fmt.Errorf("bundle: locating config dir: %w", err)
	}
	return RootStore{Path: filepath.Join(dir, "tachyon", "bundle.json")}, nil
}

// Load returns the persisted root, or "" when nothing has been saved yet.
//
// A missing file is not an error: first run is the normal case. A malformed
// file is an error, because silently reverting to the default bundle would
// point the editor at a different tree than the user last chose.
func (s RootStore) Load() (string, error) {
	if s.Path == "" {
		return "", errors.New("bundle: root store has no path")
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("bundle: reading %s: %w", s.Path, err)
	}
	var rs rootSettings
	if err := json.Unmarshal(data, &rs); err != nil {
		return "", fmt.Errorf("bundle: parsing %s: %w", s.Path, err)
	}
	return rs.BundleRoot, nil
}

// Save records the active bundle root, creating the settings directory if
// needed. The root is expanded and stored absolute so that a later Load does
// not depend on the process's working directory.
//
// The write is atomic: a temporary file in the same directory, then a rename.
func (s RootStore) Save(root string) error {
	if s.Path == "" {
		return errors.New("bundle: root store has no path")
	}
	abs, err := ExpandRoot(root)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(rootSettings{BundleRoot: abs}, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle: encoding settings: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("bundle: creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.Path)+".*")
	if err != nil {
		return fmt.Errorf("bundle: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("bundle: writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("bundle: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("bundle: replacing %s: %w", s.Path, err)
	}
	return nil
}

// Resolve is the root a caller should open: the persisted one when there is
// one, otherwise [DefaultRoot].
//
// The fallback is silent on purpose — first run is the normal case and there
// is no better answer than the default. A caller that needs to say which of
// the two it got, rather than implying the user's choice was honoured, calls
// [RootStore.Load] first: an empty string there means nothing is persisted.
//
// It does not check that the directory exists. [Open] reports that, as
// [ErrRootMissing], with the path in the message.
func (s RootStore) Resolve() (string, error) {
	saved, err := s.Load()
	if err != nil {
		return "", err
	}
	if saved != "" {
		return ExpandRoot(saved)
	}
	return DefaultRoot()
}
