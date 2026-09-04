// Command tachyon-packagecheck validates the built frontend before it is
// embedded into a packaged Tachyon application.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hollis-labs/tachyon/internal/packagecheck"
)

func main() {
	repoRoot, err := gitRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tachyon-packagecheck: %v\n", err)
		os.Exit(1)
	}
	indexPath := filepath.Join(repoRoot, "frontend", "dist", "index.html")
	if err := packagecheck.VerifyFrontendAssets(context.Background(), repoRoot, indexPath); err != nil {
		fmt.Fprintf(os.Stderr, "tachyon-packagecheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("verified: every hashed asset referenced by frontend/dist/index.html exists and is tracked")
}

func gitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("locate repository root: %w", err)
	}
	return filepath.Abs(strings.TrimSpace(string(output)))
}
