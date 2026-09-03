// Package boot manages the lifecycle of Tachyon's boot directories: the
// stable, reused-in-place directories Cairn plants a bundle into before a
// terminal opens on top of them.
//
// This package owns exactly two things: deriving a stable directory key for
// a binding or an unsaved composition, and clearing that key's path before
// the next plant. It does not invoke Cairn (that is T10, CW-20260903-0014)
// and it does not clean up what it leaves behind (that is T15,
// CW-20260903-0019, and it runs only behind a liveness guard this package
// has no part of). See plan CW-20260518-0061, decision D6, and target
// architecture §5 for the design this implements.
//
// # Why a stable directory at all
//
// Every boot directory the harness opens leaves a permanent trust entry in
// ~/.claude.json, keyed to the path. Measured 2026-09-02: 7,129 entries,
// 7,070 of them pointing at directories that no longer exist. Boot
// directories are disposable; the entries are not, and a launcher — one
// click per session, no human pause to reconsider — compounds that faster
// than a person typing commands does. The fix is structural: one boot
// directory per binding or saved composition, re-materialized in place, so
// there is one trust entry per binding, forever, rather than one per launch.
//
// Cairn plants at <boot-root>/<name>/<session> (bootdir.Location in Cairn's
// own source). A stable directory therefore needs a stable --session, not
// merely a stable --boot-root: this package fixes the session segment to
// the literal string "current" ([CurrentSegment]), so the planted path for
// a given key is always <root>/<key>/current.
//
// # Why the rename, and never a delete
//
// A live session's working directory is a handle on the directory's inode,
// not on its name. Renaming the old current aside — mv, not rm — moves the
// name while the inode and every file beneath it survive untouched; a
// session already running there keeps reading and writing exactly the files
// it launched with, including the settings file its harness has open.
// os.RemoveAll would instead unlink those files out from under a live
// session, silently, mid-session, and leave its cwd pointing at a directory
// that no longer exists.
//
// So: no path in this package deletes a boot directory or anything beneath
// one. [Prepare] moves an existing current aside to a timestamped .prev-*
// sibling and nothing in this package ever removes a .prev-* directory
// either — that sweep is T15's, deliberately held out of this package, and
// it runs only behind a liveness guard (an lsof-based check that a rename
// makes unnecessary here, because a rename can never orphan a live cwd in
// the first place).
//
// The trust entry in ~/.claude.json is unaffected by any of this, which is
// the whole point: it keys on the path a session was opened at, and that
// path — <root>/<key>/current — never changes across relaunches. Moving the
// old directory aside afterwards creates no new entry.
//
// # Deriving the key
//
// [Key] takes a caller-supplied seed — a saved binding's name, or, for an
// unsaved composition, some canonical identity string the caller builds
// from the composition's content — and returns a filesystem-safe key that
// is stable across restarts (same seed, same key, forever) and that never
// collides between two different seeds.
//
// A saved binding's name is already constrained (see internal/binding's
// nameRe) to the character set a single path segment can hold outright:
// [A-Za-z0-9_.-], starting with an alphanumeric. [Key] recognizes that case
// and returns such a seed completely unchanged. That is deliberate, not
// just an optimization: target architecture §5 says the planted path is
// <boot-root>/<key>/current precisely because Cairn's own <name> path
// segment is the same string a caller passes as the boot target (`cairn
// boot <binding>`). If [Key] transformed an already-safe binding name, this
// package's idea of where current lives would drift from where Cairn
// actually plants it. Passing the same string as both the boot target and
// the seed to [Key] keeps the two in sync by construction.
//
// A seed that is not already safe — the shape an unsaved composition's
// identity string is expected to take, since nothing constrains it the way
// a binding name is constrained — is never used as-is. [Key] sanitizes it
// into a short, readable slug for humans browsing the boot root, then
// appends a full SHA-256 hex digest of the original, untransformed seed.
// The slug exists only for readability; the hash is what makes the result
// collision-safe, because it is a lossless function of the whole seed, not
// of the lossy slug two different seeds might sanitize down to the same
// string.
//
// This package deliberately does not know what a binding or a composition
// is — it takes strings. Coupling this package to internal/binding's
// [binding.Binding] type or to a Composition type owned by internal/compose
// (T08, CW-20260903-0012) would cut against its own stated scope,
// "directory lifecycle only," and would make it a dependency of packages
// that are still in motion. Callers decide what seed identifies a binding
// or a composition; this package only guarantees what it does with that
// string once handed one.
package boot
