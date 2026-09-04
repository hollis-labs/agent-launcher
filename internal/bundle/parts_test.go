package bundle_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

func TestProfilesIncludesImmediatePartsInOneBareIDNamespace(t *testing.T) {
	root := t.TempDir()
	writePartTestFile(t, filepath.Join(root, "profiles", "z-root.md"), "---\nid: z-root\n---\n")
	writePartTestFile(t, filepath.Join(root, "profiles", "parts", "a-part.md"), "---\nid: a-part\nextends: z-root\n---\n")

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	ids := make([]string, 0, len(profiles))
	paths := map[string]string{}
	for _, profile := range profiles {
		ids = append(ids, string(profile.ID))
		paths[string(profile.ID)] = profile.RelPath
	}
	if want := []string{"a-part", "z-root"}; !slices.Equal(ids, want) {
		t.Fatalf("Profiles ids = %v; want globally sorted %v", ids, want)
	}
	if got, want := paths["a-part"], "profiles/parts/a-part.md"; got != want {
		t.Errorf("nested profile RelPath = %q; want %q", got, want)
	}

	ref := bundle.Ref{Kind: bundle.KindProfile, ID: "a-part"}
	resolved, err := b.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve nested bare id: %v", err)
	}
	if want := filepath.Join(root, "profiles", "parts", "a-part.md"); resolved != want {
		t.Fatalf("Resolve nested bare id = %q; want %q", resolved, want)
	}
	before, err := b.Read(ref)
	if err != nil {
		t.Fatalf("Read nested bare id: %v", err)
	}
	after := append(append([]byte{}, before...), []byte("edited\n")...)
	if err := b.Write(ref, after); err != nil {
		t.Fatalf("Write nested bare id: %v", err)
	}
	if onDisk, readErr := os.ReadFile(resolved); readErr != nil || !slices.Equal(onDisk, after) {
		t.Fatalf("nested write bytes = %q, err %v; want %q", onDisk, readErr, after)
	}
}

func TestProfilesReadsNestedAdditionsAndRemovalsFresh(t *testing.T) {
	root := t.TempDir()
	writePartTestFile(t, filepath.Join(root, "profiles", "base.md"), "---\nid: base\n---\n")
	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if profiles, readErr := b.Profiles(); readErr != nil || len(profiles) != 1 {
		t.Fatalf("initial Profiles = %+v, %v; want one", profiles, readErr)
	}

	part := filepath.Join(root, "profiles", "parts", "fresh.md")
	writePartTestFile(t, part, "---\nid: fresh\nextends: base\n---\n")
	if profiles, readErr := b.Profiles(); readErr != nil || len(profiles) != 2 || profiles[1].ID != "fresh" {
		t.Fatalf("Profiles after nested addition = %+v, %v; want base and fresh", profiles, readErr)
	}
	if err := os.Remove(part); err != nil {
		t.Fatalf("Remove nested profile: %v", err)
	}
	if profiles, readErr := b.Profiles(); readErr != nil || len(profiles) != 1 || profiles[0].ID != "base" {
		t.Fatalf("Profiles after nested removal = %+v, %v; want only base", profiles, readErr)
	}
}

func TestProfilesIgnoresDeeperAndUnsafePartsEntries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T, root string)
	}{
		{
			name: "deeper directory",
			build: func(t *testing.T, root string) {
				writePartTestFile(t, filepath.Join(root, "profiles", "parts", "deeper", "hidden.md"), "---\nid: hidden\n---\n")
			},
		},
		{
			name: "symlinked parts directory",
			build: func(t *testing.T, root string) {
				outside := t.TempDir()
				writePartTestFile(t, filepath.Join(outside, "hidden.md"), "---\nid: hidden\n---\n")
				if err := os.Symlink(outside, filepath.Join(root, "profiles", "parts")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
		},
		{
			name: "non-directory parts entry",
			build: func(t *testing.T, root string) {
				writePartTestFile(t, filepath.Join(root, "profiles", "parts"), "not a directory\n")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writePartTestFile(t, filepath.Join(root, "profiles", "base.md"), "---\nid: base\n---\n")
			tc.build(t, root)
			b, err := bundle.Open(root)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			profiles, err := b.Profiles()
			if err != nil {
				t.Fatalf("Profiles: %v", err)
			}
			if len(profiles) != 1 || profiles[0].ID != "base" {
				t.Fatalf("Profiles = %+v; unsafe/deeper entry must stay ignored", profiles)
			}
		})
	}
}

func TestDuplicateProfilesFailVisiblyAndNameBothPaths(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "profiles", "duplicate.md")
	partPath := filepath.Join(root, "profiles", "parts", "duplicate.md")
	writePartTestFile(t, rootPath, "---\nid: duplicate\n---\n")
	writePartTestFile(t, partPath, "---\nid: duplicate\n---\n")

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = b.Profiles()
	if !errors.Is(err, bundle.ErrDuplicateProfileID) {
		t.Fatalf("Profiles duplicate error = %v; want ErrDuplicateProfileID", err)
	}
	for _, want := range []string{rootPath, partPath} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("duplicate error does not name %s: %v", want, err)
		}
	}
	if _, err := b.Resolve(bundle.Ref{Kind: bundle.KindProfile, ID: "duplicate"}); !errors.Is(err, bundle.ErrDuplicateProfileID) {
		t.Fatalf("Resolve silently chose a duplicate: %v", err)
	}
	if _, err := b.Contents(); !errors.Is(err, bundle.ErrDuplicateProfileID) {
		t.Fatalf("Contents silently chose a duplicate: %v", err)
	}
}

func writePartTestFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, content)
}
