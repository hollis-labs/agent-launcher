package skeleton_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/skeleton"
)

const wantPartScaffold = "---\n" +
	"id: observability\n" +
	"extends: base\n" +
	"spec: {}\n" +
	"# Replace spec with only the keys this part contributes.\n" +
	"---\n"

func TestNewPartCreatesMinimalOrdinaryProfile(t *testing.T) {
	root := tempRoot(t)
	ref, err := skeleton.NewPart(root, "observability")
	if err != nil {
		t.Fatalf("NewPart: %v", err)
	}
	if ref.Kind != bundle.KindProfile || ref.ID != "observability" {
		t.Fatalf("NewPart ref = %+v; want ordinary bare profile ref", ref)
	}
	path := filepath.Join(root, "profiles", "parts", "observability.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != wantPartScaffold {
		t.Fatalf("part scaffold = %q; want exact minimal scaffold %q", data, wantPartScaffold)
	}
	for _, forbidden := range []string{"name:", "description:", "provider:"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("part scaffold unexpectedly contains %q", forbidden)
		}
	}

	b, err := bundle.Open(root)
	if err != nil {
		t.Fatalf("bundle.Open: %v", err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != "observability" || profiles[0].RelPath != "profiles/parts/observability.md" {
		t.Fatalf("Profiles after NewPart = %+v", profiles)
	}
}

func TestProfileCreationPreflightsBothLocationsWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		existingRel  string
		requestedRel string
		create       func(root string) error
	}{
		{
			name:         "root profile conflicts with existing part",
			existingRel:  "profiles/parts/shared.md",
			requestedRel: "profiles/shared.md",
			create: func(root string) error {
				_, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "shared"})
				return err
			},
		},
		{
			name:         "part conflicts with existing root profile",
			existingRel:  "profiles/shared.md",
			requestedRel: "profiles/parts/shared.md",
			create: func(root string) error {
				_, err := skeleton.NewPart(root, "shared")
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tempRoot(t)
			path := filepath.Join(root, filepath.FromSlash(tc.existingRel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(path, []byte("---\nid: shared\n---\n"), 0o640); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			before := snapshot(t, root)

			err := tc.create(root)
			if !errors.Is(err, skeleton.ErrAlreadyExists) {
				t.Fatalf("create error = %v; want ErrAlreadyExists", err)
			}
			for _, want := range []string{tc.requestedRel, tc.existingRel} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("collision error does not name %q: %v", want, err)
				}
			}
			if after := snapshot(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("bundle changed on collision refusal\nbefore: %#v\nafter:  %#v", before, after)
			}

			b, openErr := bundle.Open(root)
			if openErr != nil {
				t.Fatalf("bundle.Open after refusal: %v", openErr)
			}
			if profiles, listErr := b.Profiles(); listErr != nil || len(profiles) != 1 {
				t.Fatalf("bundle no longer opens after refusal: profiles=%+v err=%v", profiles, listErr)
			}
		})
	}
}

func TestNewPartRefusesUnsafeDestinationWithoutWritingThrough(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T, root, outside string)
	}{
		{
			name: "parts symlink",
			build: func(t *testing.T, root, outside string) {
				if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				if err := os.Symlink(outside, filepath.Join(root, "profiles", "parts")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
		},
		{
			name: "parts is a file",
			build: func(t *testing.T, root, _ string) {
				path := filepath.Join(root, "profiles", "parts")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				if err := os.WriteFile(path, []byte("occupied\n"), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			},
		},
		{
			name: "profiles symlink",
			build: func(t *testing.T, root, outside string) {
				if err := os.Symlink(outside, filepath.Join(root, "profiles")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tempRoot(t)
			outside := t.TempDir()
			tc.build(t, root, outside)
			beforeRoot := snapshot(t, root)
			beforeOutside := snapshot(t, outside)

			_, err := skeleton.NewPart(root, "escape")
			if !errors.Is(err, skeleton.ErrUnsafeDestination) {
				t.Fatalf("NewPart error = %v; want ErrUnsafeDestination", err)
			}
			if after := snapshot(t, root); !reflect.DeepEqual(after, beforeRoot) {
				t.Fatalf("bundle changed on unsafe-destination refusal\nbefore: %#v\nafter:  %#v", beforeRoot, after)
			}
			if after := snapshot(t, outside); !reflect.DeepEqual(after, beforeOutside) {
				t.Fatalf("NewPart wrote through symlink\nbefore: %#v\nafter:  %#v", beforeOutside, after)
			}
		})
	}
}

func TestNewPartKeepsDestinationExclusivePublication(t *testing.T) {
	root := tempRoot(t)
	if _, err := skeleton.NewPart(root, "once"); err != nil {
		t.Fatalf("first NewPart: %v", err)
	}
	before := snapshot(t, root)
	if _, err := skeleton.NewPart(root, "once"); !errors.Is(err, skeleton.ErrAlreadyExists) {
		t.Fatalf("second NewPart error = %v; want ErrAlreadyExists", err)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("second NewPart mutated tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestRootProfileCreationDoesNotFollowSymlinkedPartsForCollision(t *testing.T) {
	root := tempRoot(t)
	profiles := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "shared.md"), []byte("outside\n"), 0o644); err != nil {
		t.Fatalf("WriteFile outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(profiles, "parts")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "shared"}); err != nil {
		t.Fatalf("root profile creation treated content behind ignored parts symlink as a collision: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profiles, "shared.md")); err != nil {
		t.Fatalf("root profile was not created: %v", err)
	}
	outsideBytes, err := os.ReadFile(filepath.Join(outside, "shared.md"))
	if err != nil || string(outsideBytes) != "outside\n" {
		t.Fatalf("outside file changed: %q, %v", outsideBytes, err)
	}
}

type snapshotEntry struct {
	path string
	mode fs.FileMode
	data string
}

func snapshot(t *testing.T, root string) []snapshotEntry {
	t.Helper()
	var out []snapshotEntry
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := snapshotEntry{path: filepath.ToSlash(rel), mode: info.Mode()}
		if info.Mode()&fs.ModeSymlink != 0 {
			item.data, err = os.Readlink(path)
		} else if info.Mode().IsRegular() {
			var data []byte
			data, err = os.ReadFile(path)
			item.data = string(data)
		}
		if err != nil {
			return err
		}
		out = append(out, item)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}
