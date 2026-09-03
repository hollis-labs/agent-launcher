package state

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirEnv, when set to a non-empty value, overrides [Dir] in place of the
// OS's per-user configuration directory. It exists so a test — or, if ever
// needed, an operator — can redirect Tachyon's entire state tree with one
// environment variable and see every consumer follow: the boot root, the
// shell's preferences file, and the bundle-root store.
const DirEnv = "TACHYON_STATE_DIR"

// Dir returns the per-user directory Tachyon's own files are rooted under:
// the standard library's per-user configuration directory lookup (which
// resolves to ~/Library/Application Support on macOS), or the value of
// [DirEnv] when it is set to something non-empty.
//
// This is the only place in the module that makes that OS lookup.
// internal/shell and internal/bundle build their paths from [Dir] or [Root]
// rather than making it themselves, so there is exactly one place this
// decision is made — see the package doc for why that is the point.
func Dir() (string, error) {
	if v := os.Getenv(DirEnv); v != "" {
		return v, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("state: locating user config dir: %w", err)
	}
	return dir, nil
}

// Root is Tachyon's own state root: [Dir] plus "Tachyon". This is where
// Tachyon plants files that are its own generated state, as distinct from
// bundle content, which lives under git wherever the active bundle root
// points and is never Tachyon's to manage.
//
// internal/shell's preferences file already lived at Root()/shell.json
// before this package existed; that behavior is unchanged. A new consumer
// of Tachyon's own state should build its path from Root() (or [BootRoot],
// for the one thing that already needs its own subdirectory) rather than
// reconstructing the "Tachyon" convention by hand.
func Root() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Tachyon"), nil
}

// BootRoot is where Tachyon plants Cairn boot directories: Root() plus
// "boot".
//
// BootRoot is a value a caller computes and passes in. internal/compose's
// Composition.BootRoot and internal/boot's Prepare/CurrentPath root
// parameter are both caller-supplied on purpose (D9), and neither package
// calls this function itself — doing so would let a boot root become
// implicit again, which is exactly the hazard those packages' own guard
// tests (compose's TestBootRootNeverImplicit; boot's rename-not-delete,
// never-os.RemoveAll guarantees) exist to catch.
func BootRoot() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "boot"), nil
}
