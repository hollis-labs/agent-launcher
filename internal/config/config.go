// Package config resolves Tachyon's user-editable settings file: a plain
// XDG-style config a person edits by hand, distinct from internal/state's
// per-user Application Support tree (shell.json, boot directories), which
// is Tachyon's own generated state and never meant for hand-editing.
//
// # Why this exists
//
// Tachyon spawns the real `cairn` binary once per launch (internal/boot,
// internal/launch). Locating it has always been exec.LookPath("cairn") --
// fine from a terminal, where the interactive shell's PATH already includes
// wherever `go install` or Homebrew put it. It breaks the moment Tachyon
// runs as a login item: launchd starts a GUI app with a minimal PATH
// (/usr/bin:/bin:/usr/sbin:/sbin) that was never sourced from ~/.zshrc or
// ~/.zprofile, and `launchctl setenv` does not survive a logout or reboot
// either -- there is no durable way to hand a launchd-started process a
// wider PATH short of a file it reads itself.
//
// [Config.CairnPath] is that file's escape hatch: set once, by hand, and it
// stops mattering how Tachyon was started. Absent, everything falls back to
// exactly the exec.LookPath behavior this package replaces, so a
// terminal-launched Tachyon with cairn already on PATH needs no config file
// at all.
//
// # Where the file lives
//
// [Path] resolves to $XDG_CONFIG_HOME/tachyon/config.json, defaulting to
// ~/.config/tachyon/config.json when XDG_CONFIG_HOME is unset -- the same
// directory convention already in use for cerberus, torque and this
// machine's other per-tool config. The directory itself comes from
// github.com/hollis-labs/go-apppaths ([paths.Layout.ConfigDir]), the shared
// lib the standing cross-app decision (memory key
// standardize_storage_paths_go_apppaths, Torque CW-20260517-0058) already
// locked "XDG layout on every OS" to. That lib deliberately bypasses
// github.com/adrg/xdg's own per-OS defaults (~/Library/Application Support
// on macOS) for exactly the same reason this package would otherwise want
// to: XDG_CONFIG_HOME unset should mean ~/.config everywhere, not whatever a
// given OS's native convention is. This package used to hand-roll that same
// two-branch check itself before finding go-apppaths already existed and
// already covered it -- see the decision record for the discovery. It is
// deliberately NOT internal/state.Dir's os.UserConfigDir() (~/Library/
// Application Support on macOS), which is the right place for Tachyon's own
// *generated* state but not for a config file a person is expected to open
// in an editor.
//
// # Future settings
//
// CairnPath is the first field because it is the first friction. Nothing
// about this package's shape is specific to it: a later setting a person
// should control without a rebuild belongs on [Config] as another field,
// read the same way. If hand-editing this file ever becomes the friction,
// the manager (internal/manager) is the natural place to grow an editor for
// it, the same way it already edits the bundle -- there is no reason to
// build that ahead of the need.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hollis-labs/go-apppaths/paths"
)

// appName is the name this package resolves go-apppaths paths under --
// lowercase, matching the bundle identifier's own naming and the sibling
// per-tool directories already on disk (~/.config/cerberus, ~/.config/torque).
const appName = "tachyon"

// DirEnv, when set to a non-empty value, overrides [Dir] in place of the
// go-apppaths lookup. Exists for the identical reason internal/state.DirEnv
// does: a test -- or, if ever needed, an operator -- can redirect this
// package's file with one environment variable.
const DirEnv = "TACHYON_CONFIG_DIR"

// Config is Tachyon's hand-editable settings.
type Config struct {
	// CairnPath, when non-empty, is used to run cairn instead of looking it
	// up on PATH. See the package doc for why PATH alone is not enough.
	CairnPath string `json:"cairnPath,omitempty"`
}

// Dir returns the directory [Path] reads from: the value of [DirEnv] when
// set, otherwise [paths.Layout.ConfigDir] from go-apppaths -- $XDG_CONFIG_HOME/
// tachyon, defaulting to ~/.config/tachyon. [paths.WithoutMaterialize] is
// passed because this package only ever reads the file itself ([Load]);
// nothing here needs go-apppaths' data/state/cache/workspace roots created
// on Tachyon's behalf, and a config file that does not exist yet is not this
// package's problem to create -- see the package doc's "Why this exists".
func Dir() (string, error) {
	if v := os.Getenv(DirEnv); v != "" {
		return v, nil
	}
	layout, err := paths.Resolve(appName, paths.WithoutMaterialize())
	if err != nil {
		return "", fmt.Errorf("config: resolving app paths: %w", err)
	}
	return layout.ConfigDir(), nil
}

// Path is the settings file itself: [Dir] plus "config.json".
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the settings file, returning a zero-value [Config] when it
// does not exist -- an absent file means "use every default," not an
// error. A file that exists but cannot be read or parsed IS an error: that
// is likelier to be a typo or a permission problem than an intentionally
// empty file, and silently falling back to defaults would hide it.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Config{}, nil
	case err != nil:
		return Config{}, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return c, nil
}

// ResolveCairnPath finds the cairn binary: [Config.CairnPath] first, then
// the ordinary PATH lookup. This is the one place that decision is made --
// internal/launch and cmd/tachyon both call it rather than each keeping
// their own exec.LookPath("cairn"), so the config file has exactly one
// effect on the whole app, not one per call site that could drift.
//
// A configured path that does not exist is reported immediately, by name,
// rather than left to fail obscurely inside exec.Command later. A missing
// config and a PATH miss are reported together, naming the file a person
// would create to fix it, since that is now the durable fix -- see the
// package doc.
func ResolveCairnPath() (string, error) {
	cfg, err := Load()
	if err != nil {
		return "", err
	}

	if cfg.CairnPath != "" {
		if _, err := os.Stat(cfg.CairnPath); err != nil {
			return "", fmt.Errorf("config: cairnPath %q does not exist: %w", cfg.CairnPath, err)
		}
		return cfg.CairnPath, nil
	}

	p, err := exec.LookPath("cairn")
	if err != nil {
		path, pathErr := Path()
		if pathErr != nil {
			return "", fmt.Errorf("cairn not found on PATH, and its config file could not be located: %w", err)
		}
		return "", fmt.Errorf("cairn not found on PATH; set \"cairnPath\" in %s: %w", path, err)
	}
	return p, nil
}
