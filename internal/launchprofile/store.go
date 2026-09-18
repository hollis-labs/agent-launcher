package launchprofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store is a rooted view of one launch-profile directory. It caches
// nothing: every enumeration reads the tree, because a person edits these
// files with an editor and a git checkout while Tachyon is running.
type Store struct {
	// Dir is the directory launch profiles live in. Usually
	// [state.LaunchDir]; tests set their own.
	Dir string
}

// Open returns a Store rooted at dir.
func Open(dir string) Store { return Store{Dir: dir} }

// List enumerates the store, sorted by name.
//
// It returns [ErrDirMissing] when the directory does not exist — the caller
// turns that into a sentence rather than an empty list, because "you have
// not written one yet" and "this build cannot read yours" are the same
// empty slice and very different problems. See [Service.List], which is
// where that distinction reaches the UI.
func (s Store) List() ([]Profile, error) {
	if s.Dir == "" {
		return nil, errors.New("launchprofile: store has no directory")
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrDirMissing, s.Dir)
		}
		return nil, fmt.Errorf("launchprofile: reading %s: %w", s.Dir, err)
	}

	out := make([]Profile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), Ext)
		if ValidateName(name) != nil {
			// A file whose basename this store could never have written.
			// Skipped rather than errored: one oddly-named file must not
			// make the whole list unreadable.
			continue
		}
		path := filepath.Join(s.Dir, e.Name())
		p := Profile{Name: name, Path: path}
		if data, err := os.ReadFile(path); err == nil {
			_, p.Provider, p.Description = Parse(data)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns one launch profile by name.
func (s Store) Get(name string) (Profile, error) {
	if err := ValidateName(name); err != nil {
		return Profile{}, err
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, dirErr := os.Stat(s.Dir); errors.Is(dirErr, os.ErrNotExist) {
				return Profile{}, fmt.Errorf("%w: %s", ErrDirMissing, s.Dir)
			}
			return Profile{}, fmt.Errorf("%w: %s", ErrNotFound, name)
		}
		return Profile{}, fmt.Errorf("launchprofile: reading %s: %w", path, err)
	}
	p := Profile{Name: name, Path: path}
	_, p.Provider, p.Description = Parse(data)
	return p, nil
}

// Read returns one launch profile's raw bytes, for an editor.
func (s Store) Read(name string) ([]byte, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	path, err := s.path(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
		}
		return nil, fmt.Errorf("launchprofile: reading %s: %w", path, err)
	}
	return data, nil
}

// Create writes a new launch profile, refusing to overwrite one that
// already exists ([ErrExists]).
func (s Store) Create(name string, content []byte) (Profile, error) {
	if err := ValidateName(name); err != nil {
		return Profile{}, err
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Profile{}, fmt.Errorf("launchprofile: creating %s: %w", s.Dir, err)
	}
	// O_EXCL rather than a Stat-then-Write: the check and the write are one
	// syscall, so two Creates racing cannot both decide the name is free.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return Profile{}, fmt.Errorf("%w: %s", ErrExists, name)
		}
		return Profile{}, fmt.Errorf("launchprofile: creating %s: %w", path, err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return Profile{}, fmt.Errorf("launchprofile: writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return Profile{}, fmt.Errorf("launchprofile: closing %s: %w", path, err)
	}
	return s.Get(name)
}

// Save overwrites an existing launch profile, or creates it when absent.
//
// The write is atomic — a temp file in the same directory, renamed over the
// target — so a crash or a full disk mid-write leaves the previous content
// standing rather than a truncated file. These are documents a person hand
// edits and Cairn refuses to boot without; half of one is worse than the
// old one.
func (s Store) Save(name string, content []byte) (Profile, error) {
	if err := ValidateName(name); err != nil {
		return Profile{}, err
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Profile{}, fmt.Errorf("launchprofile: creating %s: %w", s.Dir, err)
	}
	tmp, err := os.CreateTemp(s.Dir, "."+name+".*"+Ext)
	if err != nil {
		return Profile{}, fmt.Errorf("launchprofile: staging a write to %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return Profile{}, fmt.Errorf("launchprofile: writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return Profile{}, fmt.Errorf("launchprofile: closing %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return Profile{}, fmt.Errorf("launchprofile: setting mode on %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return Profile{}, fmt.Errorf("launchprofile: replacing %s: %w", path, err)
	}
	return s.Get(name)
}

// path is the file a name addresses, with the name already validated by
// every caller — which is what makes the join safe.
func (s Store) path(name string) (string, error) {
	if s.Dir == "" {
		return "", errors.New("launchprofile: store has no directory")
	}
	return filepath.Join(s.Dir, name+Ext), nil
}
