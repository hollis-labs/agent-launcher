package boot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CurrentSegment is the fixed --session value every plant this package
// prepares for uses, and the final path segment [CurrentPath] always
// returns. Fixing it is what makes the boot directory stable across
// relaunches: without it, Cairn's own default session naming (bootdir's
// <name>/<session> layout) would produce a new directory, and a new
// ~/.claude.json trust entry, on every launch.
const CurrentSegment = "current"

// PrevPrefix is the fixed prefix every directory [Prepare] moves aside
// carries. It is exported so a later, separate sweep (T15,
// CW-20260903-0019) can recognize what this package leaves behind without
// redefining the convention.
const PrevPrefix = ".prev-"

// ErrInvalidKey is returned by [Prepare] when key is empty or is not a
// single, traversal-safe path segment. [Key] always produces a valid key;
// this guards callers that construct one some other way.
var ErrInvalidKey = errors.New("boot: invalid key")

// CurrentPath returns the fixed boot directory for key under root:
// root/key/current. It performs no filesystem access and never errors —
// callers needing to know whether that path currently holds a planted
// directory should stat it themselves, or call [Prepare], which does that
// and clears it.
//
// Launching the same binding twice — the same root and the same key, since
// [Key] is deterministic — always yields the same CurrentPath.
func CurrentPath(root, key string) string {
	return filepath.Join(root, key, CurrentSegment)
}

// Plan is the outcome of [Prepare]: where the next plant should target, and
// what, if anything, was moved aside to clear that path.
type Plan struct {
	// Current is root/key/current. Prepare guarantees nothing exists at
	// this path when it returns without error, so a subsequent plant (T10)
	// can create it fresh — Cairn's own PlantFiles refuses an existing
	// target.
	Current string
	// MovedAside is the path Current's previous contents were renamed to —
	// root/key/.prev-<timestamp> — or "" if Current did not exist and
	// nothing was moved.
	MovedAside string
}

// Moved reports whether Prepare found an existing directory at Current and
// moved it aside.
func (p Plan) Moved() bool { return p.MovedAside != "" }

// Prepare clears the path a plant is about to target, without deleting
// anything: if root/key/current exists, Prepare renames it to
// root/key/.prev-<timestamp>, which preserves any open handle a live
// session holds on it — see this package's doc comment for why that
// matters. It never removes a directory, and it never touches a .prev-*
// directory this or any previous call already produced.
//
// Prepare creates root/key if it does not already exist. It does not plant
// anything at Current itself; that is left for the caller (T10) to do
// immediately afterward, since Prepare's only postcondition is that nothing
// exists at Current when it returns successfully.
//
// Calling Prepare twice in a row for the same root and key without a plant
// landing at Current in between is a safe no-op on the second call: there is
// nothing at Current to move aside, so no .prev-* directory is produced.
func Prepare(root, key string) (Plan, error) {
	if root == "" {
		return Plan{}, errors.New("boot: root is required")
	}
	if err := validateKey(key); err != nil {
		return Plan{}, err
	}

	keyDir := filepath.Join(root, key)
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		return Plan{}, fmt.Errorf("boot: create %s: %w", keyDir, err)
	}

	current := filepath.Join(keyDir, CurrentSegment)
	plan := Plan{Current: current}

	switch _, err := os.Lstat(current); {
	case err == nil:
		prev, err := reservePrevPath(keyDir)
		if err != nil {
			return Plan{}, err
		}
		if err := os.Rename(current, prev); err != nil {
			return Plan{}, fmt.Errorf("boot: move %s aside to %s: %w", current, prev, err)
		}
		plan.MovedAside = prev
	case errors.Is(err, os.ErrNotExist):
		// Nothing there; Current is already clear.
	default:
		return Plan{}, fmt.Errorf("boot: stat %s: %w", current, err)
	}

	return plan, nil
}

// validateKey rejects a key that cannot safely be joined onto root as a
// single path segment: empty, ".", "..", or anything containing a path
// separator. [Key] never produces such a value; this exists for callers
// that hand Prepare a key some other way.
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	if key == "." || key == ".." {
		return fmt.Errorf("%w: %q is not a valid directory segment", ErrInvalidKey, key)
	}
	if strings.ContainsRune(key, '/') || (os.PathSeparator != '/' && strings.ContainsRune(key, os.PathSeparator)) {
		return fmt.Errorf("%w: %q must not contain a path separator", ErrInvalidKey, key)
	}
	return nil
}

// reservePrevPath returns an unused root/.prev-<timestamp> path inside
// keyDir. It uses nanosecond-precision UTC timestamps, which in practice
// never repeat within a single process, but it still probes for an existing
// name and appends a counter on collision rather than assuming that: two
// Prepare calls landing in the same nanosecond, or a clock that jumps
// backward, must never produce one .prev-* clobbering another.
func reservePrevPath(keyDir string) (string, error) {
	base := PrevPrefix + time.Now().UTC().Format("20060102T150405.000000000Z")
	candidate := filepath.Join(keyDir, base)

	for n := 1; ; n++ {
		_, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("boot: stat %s: %w", candidate, err)
		}
		candidate = filepath.Join(keyDir, fmt.Sprintf("%s-%d", base, n))
	}
}
