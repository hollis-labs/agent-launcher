package skeleton_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/skeleton"
)

// TestNewPartMatchesTheInstalledCairnContract verifies the delivered Cairn
// dependency against a scratch bundle only. No path comes from `cairn list`:
// Tachyon's bundle tests own relPath, while Cairn is asked exclusively to
// resolve and compose the bare id produced by NewPart.
func TestNewPartMatchesTheInstalledCairnContract(t *testing.T) {
	cairn, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn is not installed: %v", err)
	}
	root := t.TempDir()
	base := filepath.Join(root, "profiles", "base.md")
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatalf("MkdirAll profiles: %v", err)
	}
	if err := os.WriteFile(base, []byte("---\nid: base\nname: Base\nprovider: claude\nspec: {}\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile base: %v", err)
	}
	if _, err := skeleton.NewPart(root, "observability"); err != nil {
		t.Fatalf("NewPart: %v", err)
	}

	showPart := runCairn(t, cairn, "show", "observability", "--profile", root)
	for _, want := range []string{"observability", "base -> observability"} {
		if !strings.Contains(showPart, want) {
			t.Errorf("cairn show part output is missing %q:\n%s", want, showPart)
		}
	}
	runCairn(t, cairn, "boot", "observability", "--profile", root,
		"--scope", t.TempDir(), "--boot-root", t.TempDir(), "--session", "direct")

	showComposition := runCairn(t, cairn, "show", "base", "--profile", root, "--with", "observability")
	if !strings.Contains(showComposition, "base -> observability") {
		t.Errorf("cairn show --with did not compose the part by bare id:\n%s", showComposition)
	}
	runCairn(t, cairn, "boot", "base", "--profile", root, "--with", "observability",
		"--scope", t.TempDir(), "--boot-root", t.TempDir(), "--session", "composed")

	bindings := filepath.Join(root, "bindings")
	if err := os.MkdirAll(bindings, 0o755); err != nil {
		t.Fatalf("MkdirAll bindings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bindings, "replay.yaml"),
		[]byte("profile: base\nparts:\n  - observability\nscope: "+t.TempDir()+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile binding: %v", err)
	}
	if replay := runCairn(t, cairn, "show", "replay", "--profile", root); !strings.Contains(replay, "base -> observability") {
		t.Errorf("binding did not replay nested part by bare id:\n%s", replay)
	}

	saveScope := t.TempDir()
	runCairn(t, cairn, "boot", "base", "--profile", root, "--with", "observability",
		"--scope", saveScope, "--boot-root", t.TempDir(), "--session", "save", "--save-as", "saved")
	saved, err := os.ReadFile(filepath.Join(bindings, "saved.yaml"))
	if err != nil {
		t.Fatalf("ReadFile saved binding: %v", err)
	}
	if !strings.Contains(string(saved), "observability") || strings.Contains(string(saved), "parts/observability") {
		t.Fatalf("saved binding did not preserve the bare part id:\n%s", saved)
	}
	if replay := runCairn(t, cairn, "show", "saved", "--profile", root); !strings.Contains(replay, "base -> observability") {
		t.Errorf("saved binding did not replay nested part:\n%s", replay)
	}
}

func TestProfileCollisionRefusalLeavesInstalledCairnCatalogUsable(t *testing.T) {
	cairn, err := exec.LookPath("cairn")
	if err != nil {
		t.Skipf("cairn is not installed: %v", err)
	}
	for _, tc := range []struct {
		name   string
		seed   func(root string) error
		refuse func(root string) error
	}{
		{
			name: "existing part refuses root profile",
			seed: func(root string) error {
				_, err := skeleton.NewPart(root, "shared")
				return err
			},
			refuse: func(root string) error {
				_, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "shared"})
				return err
			},
		},
		{
			name: "existing root profile refuses part",
			seed: func(root string) error {
				_, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "shared"})
				return err
			},
			refuse: func(root string) error {
				_, err := skeleton.NewPart(root, "shared")
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeCairnBase(t, root)
			if err := tc.seed(root); err != nil {
				t.Fatalf("seed profile: %v", err)
			}
			if err := tc.refuse(root); !errors.Is(err, skeleton.ErrAlreadyExists) {
				t.Fatalf("collision error = %v; want ErrAlreadyExists", err)
			}
			if shown := runCairn(t, cairn, "show", "shared", "--profile", root); !strings.Contains(shown, "shared") {
				t.Errorf("Cairn catalog was not usable after refusal:\n%s", shown)
			}
		})
	}
}

func writeCairnBase(t *testing.T, root string) {
	t.Helper()
	base := filepath.Join(root, "profiles", "base.md")
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatalf("MkdirAll profiles: %v", err)
	}
	if err := os.WriteFile(base, []byte("---\nid: base\nname: Base\nprovider: claude\nspec: {}\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile base: %v", err)
	}
}

func runCairn(t *testing.T, cairn string, args ...string) string {
	t.Helper()
	cmd := exec.Command(cairn, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", cairn, strings.Join(args, " "), err, out)
	}
	return string(out)
}
