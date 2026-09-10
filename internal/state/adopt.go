package state

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// legacyRootName is the directory Tachyon's files lived in before the roots
// moved to XDG: os.UserConfigDir() plus this, which on macOS is
// ~/Library/Application Support/Tachyon.
const legacyRootName = "Tachyon"

// adoptedFiles are the files [Adopt] moves, and the list is deliberately
// explicit rather than "everything in the directory".
//
// Each one records a choice a person made and cannot re-derive: the rebound
// hotkey and window geometry, the project list, and which bundle is active.
// Losing any of them is silent -- the app comes up with defaults and nothing
// says why -- which is what makes moving them worth code rather than a note
// in a release.
//
// boot/ is deliberately absent. Boot directories are disposable and
// regenerated on the next launch, and moving them would not preserve what
// makes them worth keeping anyway: the harness's trust entry in
// ~/.claude.json keys on the path, and the path is what changed. Leaving
// them costs a stale directory a person can delete; moving them would cost
// the same and take longer.
var adoptedFiles = []string{"shell.json", "projects.json", "bundle.json"}

// LegacyRoot is where Tachyon's files lived before the move to XDG roots:
// os.UserConfigDir() plus "Tachyon". It returns "" when the lookup fails or
// when [DirEnv] is set, because an overridden footprint has no legacy to
// adopt -- a test pointing DirEnv at a temp directory must never be handed a
// path to the real machine's Application Support.
func LegacyRoot() string {
	if os.Getenv(DirEnv) != "" {
		return ""
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, legacyRootName)
}

// Adopt moves the files listed in [adoptedFiles] from [LegacyRoot] into
// [ConfigDir], reporting each move on w. It is idempotent and safe to call
// on every startup.
//
// The rule per file is go-apppaths' own adoptRoot rule, for the reason that
// library states it: a run must never clobber data.
//
//   - legacy absent                  -> nothing
//   - legacy present, target absent  -> move
//   - legacy present, target present -> leave both, say so
//
// The third case is the one worth being careful about. Overwriting the
// target would discard whatever the person has done since the move, and
// picking a winner by mtime would be this function guessing about a file it
// does not understand. Reporting it and touching neither leaves a person
// with both files and enough information to merge them, which is the only
// outcome here that cannot lose something.
//
// Every failure is reported and none is fatal: an app that will not start
// because a five-year-old preferences file could not be moved is worse than
// one that starts with defaults and says what happened. The error return is
// reserved for not being able to work out where the files go at all.
func Adopt(w io.Writer) error {
	legacy := LegacyRoot()
	if legacy == "" {
		return nil
	}
	if _, err := os.Stat(legacy); err != nil {
		// No legacy directory is the overwhelmingly common case -- every
		// machine that installs Tachyon from here on. Not an error.
		return nil
	}

	target, err := ConfigDir()
	if err != nil {
		return fmt.Errorf("state: adopting legacy files: %w", err)
	}

	var made bool
	for _, name := range adoptedFiles {
		from := filepath.Join(legacy, name)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		to := filepath.Join(target, name)
		if _, err := os.Stat(to); err == nil {
			report(w, "tachyon: %s exists in both %s and %s; leaving both untouched -- merge them by hand", name, legacy, target)
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			report(w, "tachyon: cannot check %s: %v", to, err)
			continue
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			report(w, "tachyon: cannot create %s to adopt %s: %v", target, name, err)
			continue
		}
		if err := os.Rename(from, to); err != nil {
			report(w, "tachyon: cannot move %s to %s: %v", from, to, err)
			continue
		}
		report(w, "tachyon: moved %s to %s", from, to)
		made = true
	}

	if made {
		report(w, "tachyon: %s is no longer read; anything left in it (boot directories included) is safe to delete", legacy)
	}
	return nil
}

// report writes one newline-terminated line to w, discarding the result and
// tolerating a nil writer -- adoption diagnostics are best-effort, exactly
// as go-apppaths' own are.
func report(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}
