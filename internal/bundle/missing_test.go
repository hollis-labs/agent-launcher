package bundle_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// tempBundle builds a one-profile bundle in a temp directory and returns the
// root and an open Bundle.
func tempBundle(t *testing.T) (string, *bundle.Bundle) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "agent-setup")
	mkdirAll(t, filepath.Join(root, "profiles"))
	writeFile(t, filepath.Join(root, "profiles", "base.md"), "---\nid: base\nname: Base\n---\n")
	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return root, b
}

// TestVanishedRootIsNotAnEmptyBundle is the whole point of ErrRootMissing.
//
// The per-directory rule — an absent directory enumerates as empty — is
// deliberate and stays. Applied to the root it would hand a user who renamed
// the repo or mistyped the path a clean, error-free, completely empty bundle:
// an empty result that looks like an answer. T05 renders this into a tree, and
// "your bundle is gone" must not draw the same tree as "your bundle is empty".
func TestVanishedRootIsNotAnEmptyBundle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		vanish func(t *testing.T, root string)
	}{{
		name:   "deleted",
		vanish: func(t *testing.T, root string) { rm(t, root) },
	}, {
		// The realistic case: the repo gets renamed or moved.
		name: "renamed",
		vanish: func(t *testing.T, root string) {
			if err := os.Rename(root, root+"-moved"); err != nil {
				t.Fatalf("Rename: %v", err)
			}
		},
	}, {
		// Something that is not a directory now stands where the bundle was.
		name: "replaced by a file",
		vanish: func(t *testing.T, root string) {
			rm(t, root)
			writeFile(t, root, "not a bundle\n")
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			root, b := tempBundle(t)

			c, err := b.Contents()
			if err != nil || len(c.Profiles) != 1 {
				t.Fatalf("before: Contents = %d profiles, err %v; want 1, nil", len(c.Profiles), err)
			}

			tc.vanish(t, root)

			// Every enumeration, not just Contents: they all funnel through
			// the same directory read, and a caller may use any of them.
			if _, err := b.Contents(); !errors.Is(err, bundle.ErrRootMissing) {
				t.Errorf("Contents: err = %v; want ErrRootMissing", err)
			}
			for name, call := range map[string]func() error{
				"Profiles":  func() error { _, err := b.Profiles(); return err },
				"RoleProse": func() error { _, err := b.RoleProse(); return err },
				"Templates": func() error { _, err := b.Templates(); return err },
				"Prompts":   func() error { _, err := b.Prompts(); return err },
				"Skills":    func() error { _, err := b.Skills(); return err },
				"Hooks":     func() error { _, err := b.Hooks(); return err },
				"Bindings":  func() error { _, err := b.Bindings(); return err },
			} {
				if err := call(); !errors.Is(err, bundle.ErrRootMissing) {
					t.Errorf("%s: err = %v; want ErrRootMissing", name, err)
				}
			}

			ref := bundle.Ref{Kind: bundle.KindProfile, ID: "base"}
			if _, err := b.Resolve(ref); !errors.Is(err, bundle.ErrRootMissing) {
				t.Errorf("Resolve: err = %v; want ErrRootMissing", err)
			}
			// Not ErrNotFound: "the bundle is gone" must not arrive dressed up
			// as "you named the wrong artifact".
			_, err = b.Read(ref)
			if !errors.Is(err, bundle.ErrRootMissing) {
				t.Errorf("Read: err = %v; want ErrRootMissing", err)
			}
			if errors.Is(err, bundle.ErrNotFound) {
				t.Errorf("Read: err = %v; a missing root must not read as a missing artifact", err)
			}
			// The message names the root, so the user can see the typo.
			if !strings.Contains(err.Error(), root) {
				t.Errorf("Read: err = %q; want the root path in the message", err)
			}
		})
	}
}

func TestOpenReportsAMissingRootWithTheSentinel(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-bundle")
	_, err := bundle.Open(missing)
	if !errors.Is(err, bundle.ErrRootMissing) {
		t.Fatalf("Open of a missing root: err = %v; want ErrRootMissing", err)
	}
	// Still an fs.ErrNotExist underneath, for a caller that wants that.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open: err = %v; want fs.ErrNotExist to be preserved", err)
	}

	file := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, file, "x\n")
	if _, err := bundle.Open(file); !errors.Is(err, bundle.ErrRootMissing) {
		t.Fatalf("Open of a file: err = %v; want ErrRootMissing", err)
	}
}

// An empty bundle is a real, legitimate answer and must stay one.
func TestEmptyRootIsStillAnEmptyBundle(t *testing.T) {
	root := t.TempDir()
	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	c, err := b.Contents()
	if err != nil {
		t.Fatalf("Contents on an empty but present root: %v", err)
	}
	if n := len(c.Profiles) + len(c.RoleProse) + len(c.Templates) + len(c.Prompts) + len(c.Skills) + len(c.Hooks) + len(c.Bindings); n != 0 {
		t.Fatalf("empty bundle enumerated %d artifacts", n)
	}
}

// A root we can see but cannot enter is a permission problem, and is reported
// as one rather than as a missing root or as an empty bundle.
func TestUnreadableDirectoryIsAPermissionErrorNotEmptiness(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not bind")
	}
	root, b := tempBundle(t)
	profiles := filepath.Join(root, "profiles")
	if err := os.Chmod(profiles, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(profiles, 0o755) })

	got, err := b.Profiles()
	if err == nil {
		t.Fatalf("Profiles over an unreadable directory returned %d profiles and no error", len(got))
	}
	if errors.Is(err, bundle.ErrRootMissing) {
		t.Errorf("Profiles: err = %v; a permission problem is not a missing root", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Profiles: err = %v; want fs.ErrPermission", err)
	}
}

// A file that could not be read must not label a tree row the same way a file
// with no frontmatter does — the second is something the user can go and edit,
// the first is not.
func TestUnreadableFileIsNotTheSameAsNoFrontmatter(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not bind")
	}
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "profiles"))
	writeFile(t, filepath.Join(root, "profiles", "plain.md"), "no frontmatter here\n")
	locked := filepath.Join(root, "profiles", "locked.md")
	writeFile(t, locked, "---\nid: locked\n---\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %d; want 2 — an unreadable file still enumerates (D8)", len(profiles))
	}
	byID := map[bundle.ProfileID]bundle.Header{}
	for _, p := range profiles {
		byID[p.ID] = p.Header
	}
	if got := byID["plain"]; got != (bundle.Header{}) {
		t.Errorf("plain.md header = %+v; want the zero Header", got)
	}
	if got := byID["locked"]; got != (bundle.Header{Unreadable: true}) {
		t.Errorf("locked.md header = %+v; want Unreadable", got)
	}
	if byID["plain"] == byID["locked"] {
		t.Error("a headerless file and an unreadable file produced the same Header")
	}
}

// An entry that was listed and then could not be opened — a broken symlink, or
// a file deleted between the listing and the read — collapses onto ErrNotFound
// so a caller has one absence to handle rather than two.
func TestDanglingEntryReadsAsNotFound(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "profiles"))
	if err := os.Symlink(filepath.Join(root, "profiles", "nowhere.md"), filepath.Join(root, "profiles", "dangling.md")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != "dangling" {
		t.Fatalf("profiles = %+v; want the dangling entry listed", profiles)
	}
	if profiles[0].Header != (bundle.Header{Unreadable: true}) {
		t.Errorf("dangling header = %+v; want Unreadable", profiles[0].Header)
	}
	// Resolve succeeds: the ref did name a listed entry.
	if _, err := b.Resolve(profiles[0].Ref()); err != nil {
		t.Errorf("Resolve of a dangling entry: %v", err)
	}
	if _, err := b.Read(profiles[0].Ref()); !errors.Is(err, bundle.ErrNotFound) {
		t.Errorf("Read of a dangling entry: err = %v; want ErrNotFound", err)
	}
}

// A file deleted between the listing and the read is the same shape as a
// dangling symlink, and must not become a missing root.
func TestFileDeletedAfterListingReadsAsNotFound(t *testing.T) {
	_, b := tempBundle(t)
	ref := bundle.Ref{Kind: bundle.KindProfile, ID: "base"}
	path, err := b.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	rm(t, path)
	// Resolve no longer lists it; Read reports the same absence either way.
	if _, err := b.Read(ref); !errors.Is(err, bundle.ErrNotFound) {
		t.Errorf("Read after the file was deleted: err = %v; want ErrNotFound", err)
	}
	if _, err := b.Contents(); err != nil {
		t.Errorf("Contents with the root intact: %v; want no error", err)
	}
}

func rm(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll(%s): %v", path, err)
	}
}
