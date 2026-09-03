// Package state is the single definition of where Tachyon's own files live
// on disk, as distinct from bundle content: the agent-setup bundle lives
// wherever the active bundle root points, is tracked in the user's own git
// history, and is never this package's concern.
//
// # Why this package exists
//
// Before it, Tachyon's per-user config directory plus a "Tachyon" segment
// existed in three independent places with no single definition:
// internal/shell/prefs.go built the shell.json path from it directly,
// internal/bundle/root.go built the bundle-root store's path from it
// directly, and internal/compose's documentation and tests asserted a
// boot-root shape that reconstructed the same string a second, independent
// way. Nothing actually defined the convention; it survived only as
// agreement between copies, and one of those "copies" was a test asserting
// a shape it also built by hand. If one of the three drifted, they would
// disagree silently — about where boot directories get planted, which is
// D9's hazard: Cairn's own default boot root is dev/agent-os/runtime/boot
// under $HOME, and ~/dev/agent-os is a real git working tree, measured
// 2026-09-02 with 1,249 dirty files on its own branch.
//
// # What this package is not
//
// It does not decide where a Cairn boot lands, and it is not a place either
// internal/compose or internal/boot reaches into on its own. [Root] and
// [BootRoot] are values a caller computes and passes in — compose's
// Composition.BootRoot and boot's Prepare/CurrentPath root parameter both
// stay caller-supplied, exactly as before this package existed. Pulling
// either package's defaulting in here would be precisely the regression D9
// forbids: an implicit boot root a caller never chose. compose.Build's
// ErrNoBootRoot and its TestBootRootNeverImplicit, and boot's own
// never-delete guarantees, exist to keep that true; this package exists so
// their eventual caller (plan task T10, and Tachyon's own startup code) has
// one place to compute the value from, nothing more.
//
// # Overriding it
//
// [Dir] — and therefore [Root], [BootRoot], and every consumer built on
// them — is overridable by setting [DirEnv] to a non-empty value.
// Production code never sets it; tests do, to prove that changing where
// Tachyon's files live is a one-line change in one function, and that every
// consumer follows it.
package state
