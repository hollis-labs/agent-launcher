package packagecheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyFrontendAssetsAcceptsTrackedHashedAssets(t *testing.T) {
	repo, index := testRepository(t, true, true)
	if err := VerifyFrontendAssets(context.Background(), repo, index); err != nil {
		t.Fatalf("VerifyFrontendAssets: %v", err)
	}
}

func TestVerifyFrontendAssetsRejectsMissingHashedAsset(t *testing.T) {
	repo, index := testRepository(t, false, false)
	err := VerifyFrontendAssets(context.Background(), repo, index)
	if err == nil || !strings.Contains(err.Error(), "missing hashed asset") {
		t.Fatalf("VerifyFrontendAssets error = %v, want missing hashed asset", err)
	}
}

func TestVerifyFrontendAssetsRejectsUntrackedHashedAsset(t *testing.T) {
	repo, index := testRepository(t, true, false)
	err := VerifyFrontendAssets(context.Background(), repo, index)
	if err == nil || !strings.Contains(err.Error(), "untracked hashed asset") {
		t.Fatalf("VerifyFrontendAssets error = %v, want untracked hashed asset", err)
	}
}

func TestHashedAssetReferencesRejectsEscape(t *testing.T) {
	_, err := hashedAssetReferences([]byte(`<script src="../outside-12345678.js"></script>`))
	if err == nil || !strings.Contains(err.Error(), "unsafe asset reference") {
		t.Fatalf("hashedAssetReferences error = %v, want unsafe reference", err)
	}
}

func TestHashedAssetReferencesHandlesUnquotedAttributes(t *testing.T) {
	refs, err := hashedAssetReferences([]byte(`<script src=./assets/index-AbCd1234.js></script>`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(refs, ","), "assets/index-AbCd1234.js"; got != want {
		t.Fatalf("hashedAssetReferences = %q, want %q", got, want)
	}
}

func TestHashedAssetReferencesDecodesHTMLEntities(t *testing.T) {
	refs, err := hashedAssetReferences([]byte(`<link href="./assets/index-AbCd1234&#46;css">`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(refs, ","), "assets/index-AbCd1234.css"; got != want {
		t.Fatalf("hashedAssetReferences = %q, want %q", got, want)
	}
}

func TestVerifyFrontendAssetsTreatsGitPathspecMetacharactersLiterally(t *testing.T) {
	repo := t.TempDir()
	dist := filepath.Join(repo, "frontend", "dist")
	literalDir := filepath.Join(dist, "assets[copy]")
	matchedDir := filepath.Join(dist, "assetsc")
	if err := os.MkdirAll(literalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(matchedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dist, "index.html")
	if err := os.WriteFile(index, []byte(`<script src="./assets[copy]/index-AbCd1234.js"></script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(literalDir, "index-AbCd1234.js"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(matchedDir, "index-AbCd1234.js"), []byte("tracked pathspec match\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	git(t, repo, "init", "-q")
	git(t, repo, "add", "frontend/dist/index.html", "frontend/dist/assetsc/index-AbCd1234.js")
	err := VerifyFrontendAssets(context.Background(), repo, index)
	if err == nil || !strings.Contains(err.Error(), "untracked hashed asset") {
		t.Fatalf("VerifyFrontendAssets error = %v, want literal untracked hashed asset", err)
	}
}

func testRepository(t *testing.T, writeAsset, trackAsset bool) (string, string) {
	t.Helper()
	repo := t.TempDir()
	dist := filepath.Join(repo, "frontend", "dist")
	assetDir := filepath.Join(dist, "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dist, "index.html")
	if err := os.WriteFile(index, []byte(`<script src="./assets/index-AbCd1234.js?cache=1"></script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(assetDir, "index-AbCd1234.js")
	if writeAsset {
		if err := os.WriteFile(asset, []byte("export {};\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git(t, repo, "init", "-q")
	git(t, repo, "add", "frontend/dist/index.html")
	if trackAsset {
		git(t, repo, "add", "frontend/dist/assets/index-AbCd1234.js")
	}
	return repo, index
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
