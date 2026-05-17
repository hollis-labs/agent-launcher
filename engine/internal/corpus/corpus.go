// Package corpus embeds the S4.4 LaunchSpec / launch-bag / template
// corpus (re-expressed from go-agent-launch's agentlaunch/testdata/specs)
// directly into the tachyon-engine binary.
//
// Embedding is what makes the engine LOCAL-FIRST (design decision D1):
// the .app bundle ships the engine binary with this corpus baked in, so
// `tachyon-engine list` works with no directory service AND no on-disk
// catalog. The corpus is materialized to a temp dir at runtime so the
// go-agent-launch file-backed registrar — which is filesystem-driven —
// can walk it like any catalog root.
package corpus

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Files holds the embedded spec corpus. The directory layout mirrors a
// Tether catalog root closely enough for the file-backed registrar:
// launches/ maps to the execution-template kind and providers/ maps to
// the runtime-binding kind.
//
//go:embed launch-assembly.yaml launches providers templates
var Files embed.FS

// SpecID is the catalog id of the single canonical LaunchSpec the corpus
// ships (see launch-assembly.yaml).
const SpecID = "tether.launch"

// AssemblyFile is the corpus-relative path of the canonical LaunchSpec.
const AssemblyFile = "launch-assembly.yaml"

// ProvidersSubdir is the corpus subdirectory holding the runtime-binding
// contracts (one per `runner` token). The go-agent-launch file-backed
// registrar maps a providers/ subdir to the runtime-binding kind.
const ProvidersSubdir = "providers"

// Materialize writes the embedded corpus into a fresh temp directory and
// returns its absolute root. The layout is a catalog root the
// go-agent-launch file-backed registrar can ingest directly: the
// launches/ subdir holds the launch bags.
//
// The caller owns cleanup of the returned directory.
func Materialize() (root string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "tachyon-corpus-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("corpus: mkdtemp: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	walkErr := fs.WalkDir(Files, ".", func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, rerr := Files.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if walkErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("corpus: materialize: %w", walkErr)
	}
	return dir, cleanup, nil
}
