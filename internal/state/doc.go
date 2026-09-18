// Package state is the single definition of where Tachyon's own files live
// on disk, as distinct from bundle content: the agent-setup bundle lives
// wherever the active bundle root points, is tracked in the user's own git
// history, and is never this package's concern.
//
// # Two roots, not one
//
// [ConfigDir] holds what a person chose: the active bundle root, the
// project list, the hotkey and window geometry, and the launch profiles
// under [LaunchDir]. [StateDir] holds what Tachyon generated and can
// regenerate: boot directories, and nothing else today.
//
// That split is the package's whole shape. Before it, everything lived in
// one tree under os.UserConfigDir() -- ~/Library/Application Support/Tachyon
// on macOS -- which got both halves wrong at once. Disposable boot
// directories sat beside durable choices, so "back this up" and "delete
// this safely" had no answer; and the durable half was buried somewhere a
// person editing a launch profile by hand could not reasonably be asked to
// look. Both roots now come from github.com/hollis-labs/go-apppaths, the
// shared lib the standing cross-app decision (memory key
// standardize_storage_paths_go_apppaths, Torque CW-20260517-0058) locked
// "XDG layout on every OS" to, and which internal/config was already using
// for its own file while this package was not.
//
// # Why this package exists at all
//
// Before it, the per-user directory plus a "Tachyon" segment existed in
// three independent places with no single definition: internal/shell's
// prefs path, internal/bundle's root store, and internal/compose's
// documentation and tests asserting a boot-root shape they also built by
// hand. Nothing defined the convention; it survived as agreement between
// copies, one of which was a test asserting a shape it constructed itself.
// If one drifted they would disagree silently -- about where boot
// directories get planted, which is D9's hazard: Cairn's own default boot
// root is dev/agent-os/runtime/boot under $HOME, and ~/dev/agent-os is a
// real git working tree, measured 2026-09-02 with 1,249 dirty files on its
// own branch.
//
// # What this package is not
//
// It does not decide where a Cairn boot lands, and it is not a place either
// internal/compose or internal/boot reaches into on its own. [BootRoot] is a
// value a caller computes and passes in -- compose's Composition.BootRoot
// and boot's Prepare/CurrentPath root parameter both stay caller-supplied.
// Pulling either package's defaulting in here would be precisely the
// regression D9 forbids: an implicit boot root a caller never chose.
// compose.Build's ErrNoBootRoot and its TestBootRootNeverImplicit, and
// boot's own never-delete guarantees, exist to keep that true; this package
// exists so their eventual caller has one place to compute the value from,
// nothing more.
//
// # Overriding it
//
// [DirEnv] redirects both roots at once, and keeps them distinct while it
// does: <value>/config and <value>/state. Production code never sets it;
// tests do, to prove that moving Tachyon's footprint is a one-variable
// change every consumer follows.
package state
