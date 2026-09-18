package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hollis-labs/go-apppaths/paths"
)

// appName is the directory name every root below is resolved under --
// lowercase, matching internal/config's own appName, the bundle
// identifier's naming, and the sibling per-tool directories already on disk
// (~/.config/cerberus, ~/.config/torque).
const appName = "tachyon"

// DirEnv, when set to a non-empty value, redirects every root this package
// resolves. It exists so a test -- or, if ever needed, an operator -- can
// move Tachyon's entire on-disk footprint with one environment variable and
// see every consumer follow: the boot root, the launch-profile store, the
// shell's preferences file, and the bundle-root store.
//
// It redirects the ROOTS and not the split between them: [ConfigDir] becomes
// <value>/config and [StateDir] becomes <value>/state. Collapsing the two
// under one override would let a test pass while production kept config and
// state in the same directory, which is the distinction this package exists
// to make.
const DirEnv = "TACHYON_STATE_DIR"

// ConfigDir is where Tachyon keeps files that record a person's choices:
// the active bundle root, the project list, the hotkey and window geometry,
// and the launch profiles under [LaunchDir]. $XDG_CONFIG_HOME/tachyon,
// defaulting to ~/.config/tachyon.
//
// It is deliberately NOT os.UserConfigDir(), which resolves to
// ~/Library/Application Support on macOS. That was where this package sent
// everything before, and it is the wrong half of the split twice over: it
// buries hand-editable files somewhere a person cannot reasonably open them,
// and it puts disposable boot directories in the same tree as durable
// choices. go-apppaths is the shared lib the standing cross-app decision
// (memory key standardize_storage_paths_go_apppaths, Torque
// CW-20260517-0058) locked "XDG layout on every OS" to, and internal/config
// already resolved its own file through it -- this package was the half
// that had not caught up.
func ConfigDir() (string, error) {
	if v := os.Getenv(DirEnv); v != "" {
		return filepath.Join(v, "config"), nil
	}
	layout, err := resolve()
	if err != nil {
		return "", err
	}
	return layout.ConfigDir(), nil
}

// StateDir is where Tachyon keeps files it generated and can regenerate:
// boot directories, and nothing else today. $XDG_STATE_HOME/tachyon,
// defaulting to ~/.local/state/tachyon.
//
// The split from [ConfigDir] is the point rather than tidiness. A boot
// directory is disposable, machine-local and rebuilt on every launch; a
// launch profile is a durable choice a person wrote and will want in git.
// Keeping them in one tree makes "back this up" and "delete this safely"
// unanswerable. Cairn's own default boot root is already
// ~/.local/state/cairn/boot, so this also puts the two tools' disposable
// output in the same place under the same rule.
func StateDir() (string, error) {
	if v := os.Getenv(DirEnv); v != "" {
		return filepath.Join(v, "state"), nil
	}
	layout, err := resolve()
	if err != nil {
		return "", err
	}
	return layout.StateDir(), nil
}

// BootRoot is where Tachyon plants Cairn boot directories: [StateDir] plus
// "boot".
//
// BootRoot is a value a caller computes and passes in. internal/compose's
// Composition.BootRoot and internal/boot's Prepare/CurrentPath root
// parameter are both caller-supplied on purpose (D9), and neither package
// calls this function itself -- doing so would let a boot root become
// implicit again, which is exactly the hazard those packages' own guard
// tests (compose's TestBootRootNeverImplicit; boot's rename-not-delete,
// never-os.RemoveAll guarantees) exist to catch.
func BootRoot() (string, error) {
	root, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "boot"), nil
}

// LaunchDir is the launch-profile store: [ConfigDir] plus "launch".
//
// Each file in it is an ordinary Cairn part -- a profile with frontmatter
// and an optional body -- that Tachyon passes to `cairn boot` as
// `--with <path>`. It lives under config rather than state because it is
// the durable half of a launch: the provider, the sandbox posture and the
// settings a person chose, which they will want to edit, diff and commit.
// See internal/launchprofile.
func LaunchDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "launch"), nil
}

// resolve is the one go-apppaths lookup this package makes.
//
// [paths.WithoutMaterialize] is passed because nothing here wants
// go-apppaths creating roots on Tachyon's behalf: the two directories
// Tachyon actually writes into are created by the code that writes them
// (internal/boot's Prepare, internal/launchprofile's Save), and the cache,
// data and workspace roots this app has no use for should not appear on
// disk merely because a path was resolved.
func resolve() (paths.Layout, error) {
	layout, err := paths.Resolve(appName, paths.WithoutMaterialize())
	if err != nil {
		return paths.Layout{}, fmt.Errorf("state: resolving app paths: %w", err)
	}
	return layout, nil
}
