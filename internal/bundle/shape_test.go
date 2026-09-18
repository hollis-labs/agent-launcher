package bundle_test

import (
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

// TestHasKnownShape is CW-20260904-0019's own addition: the shape check
// internal/manager.Service.Tree uses to tell "this directory was never a
// bundle" apart from "this bundle is genuinely empty" — two situations
// that enumerate identically today (see bundle.go's own doc on why that
// stays true) and were, before this task, indistinguishable above this
// package too.
func TestHasKnownShape(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T, root string)
		want  bool
	}{
		{
			name:  "nothing at all",
			build: func(t *testing.T, root string) {},
			want:  false,
		},
		{
			name: "an unrelated file and directory, no recognized names",
			build: func(t *testing.T, root string) {
				mkdirAll(t, filepath.Join(root, "Documents"))
				writeFile(t, filepath.Join(root, "notes.txt"), "not a bundle\n")
			},
			want: false,
		},
		{
			name: "an empty profiles/ -- a genuinely empty bundle",
			build: func(t *testing.T, root string) {
				mkdirAll(t, filepath.Join(root, "profiles"))
			},
			want: true,
		},
		{
			name: "just templates/, nothing else",
			build: func(t *testing.T, root string) {
				mkdirAll(t, filepath.Join(root, "templates"))
			},
			want: true,
		},
		{
			// bindings/ was a shape directory until agent-setup retired it.
			// A root holding only one is now a root holding nothing this
			// package knows about.
			name: "just bindings/, which is no longer a bundle directory",
			build: func(t *testing.T, root string) {
				mkdirAll(t, filepath.Join(root, "bindings"))
			},
			want: false,
		},
		{
			name: "just hooks/, nothing else",
			build: func(t *testing.T, root string) {
				mkdirAll(t, filepath.Join(root, "hooks"))
			},
			want: true,
		},
		{
			name: "just prompts/, nothing else",
			build: func(t *testing.T, root string) {
				mkdirAll(t, filepath.Join(root, "prompts"))
			},
			want: true,
		},
		{
			name: "a fully populated bundle",
			build: func(t *testing.T, root string) {
				mkdirAll(t, filepath.Join(root, "profiles"))
				writeFile(t, filepath.Join(root, "profiles", "base.md"), "---\nid: base\n---\n")
				mkdirAll(t, filepath.Join(root, "templates", "lenses"))
				mkdirAll(t, filepath.Join(root, "skills"))
				mkdirAll(t, filepath.Join(root, "hooks"))
				mkdirAll(t, filepath.Join(root, "prompts"))
			},
			want: true,
		},
		{
			name: "a file named profiles, not a directory",
			build: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "profiles"), "not a directory\n")
			},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.build(t, root)
			b, err := bundle.Open(root)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if got := b.HasKnownShape(); got != tc.want {
				t.Errorf("HasKnownShape() = %v; want %v", got, tc.want)
			}
		})
	}
}
