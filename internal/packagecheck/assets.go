// Package packagecheck verifies the committed frontend assets that are put in
// the macOS application bundle. It is intentionally separate from the package
// script so the two failure cases that matter here are ordinary Go tests.
package packagecheck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// Vite's production output is name-HASH.ext. Keep this deliberately
// format-based instead of depending on today's exact hash length.
var hashedFilename = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

// VerifyFrontendAssets rejects every missing or untracked hashed asset
// referenced by dist/index.html. repoRoot and indexPath must be absolute.
func VerifyFrontendAssets(ctx context.Context, repoRoot, indexPath string) error {
	html, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("read frontend entry point: %w", err)
	}

	refs, err := hashedAssetReferences(html)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		assetPath := filepath.Join(filepath.Dir(indexPath), filepath.FromSlash(ref))
		info, err := os.Stat(assetPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("frontend/dist/index.html references missing hashed asset %q", ref)
			}
			return fmt.Errorf("stat referenced frontend asset %q: %w", ref, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("frontend/dist/index.html references non-file hashed asset %q", ref)
		}

		rel, err := filepath.Rel(repoRoot, assetPath)
		if err != nil {
			return fmt.Errorf("make frontend asset path repository-relative: %w", err)
		}
		rel = filepath.ToSlash(rel)
		// --literal-pathspecs is a global Git option. Without it, brackets,
		// asterisks and question marks in a real asset name are pathspec
		// operators and may match a different tracked file. NUL output gives us
		// an exact comparison even for unusual but valid filenames.
		cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "--literal-pathspecs", "ls-files", "--error-unmatch", "-z", "--", rel)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			detail := strings.TrimSpace(stderr.String())
			if detail != "" {
				detail = ": " + detail
			}
			return fmt.Errorf("frontend/dist/index.html references untracked hashed asset %q%s", ref, detail)
		}
		if !bytes.Equal(output, append([]byte(rel), 0)) {
			return fmt.Errorf("git returned a non-exact tracked path for hashed asset %q", ref)
		}
	}
	return nil
}

func hashedAssetReferences(document []byte) ([]string, error) {
	seen := make(map[string]struct{})
	tokenizer := html.NewTokenizer(bytes.NewReader(document))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if err := tokenizer.Err(); err != io.EOF {
				return nil, fmt.Errorf("parse frontend/dist/index.html: %w", err)
			}
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}

		_, moreAttributes := tokenizer.TagName()
		for moreAttributes {
			key, value, more := tokenizer.TagAttr()
			moreAttributes = more
			if !bytes.Equal(key, []byte("href")) && !bytes.Equal(key, []byte("src")) {
				continue
			}
			// Tokenizer.TagAttr handles quoted and unquoted HTML attributes and
			// returns the entity-decoded value. A regexp here would leave both as
			// bypasses around the packaging check.
			ref := string(bytes.TrimSpace(value))
			if err := collectHashedAssetReference(seen, ref); err != nil {
				return nil, err
			}
		}
	}

	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs, nil
}

func collectHashedAssetReference(seen map[string]struct{}, ref string) error {
	if ref == "" || strings.HasPrefix(ref, "//") || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") {
		return nil
	}
	original := ref
	if cut := strings.IndexAny(ref, "?#"); cut >= 0 {
		ref = ref[:cut]
	}
	ref = strings.TrimPrefix(ref, "./")
	ref = strings.TrimPrefix(ref, "/")
	ref = filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
	if ref == "." || ref == ".." || strings.HasPrefix(ref, "../") {
		return fmt.Errorf("frontend/dist/index.html has unsafe asset reference %q", original)
	}
	if hashedFilename.MatchString(filepath.Base(ref)) {
		seen[ref] = struct{}{}
	}
	return nil
}
