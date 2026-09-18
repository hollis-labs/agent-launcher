// Command tachyon is a minimal, terminal-drivable debug CLI over the same
// packages the Tachyon app itself binds — internal/compose and
// internal/boot — built to precede the spawn (T12, CW-20260903-0016) on
// purpose: the launch path (compose → Cairn → argv → terminal) is where
// bugs are silent and expensive — wrong scope, wrong settings tier, wrong
// boot root — and this makes that path verifiable from a terminal before a
// GUI can hide it (plan CW-20260518-0061, decision D10, §7).
//
// It resolves one composition (a saved binding's name, or a bare profile
// id, plus the bundle and boot root to use), runs it through
// internal/compose.Build and internal/boot.Invoke exactly as the app will,
// and prints what a person needs to answer four questions without spawning
// anything:
//
//   - What --boot-root is this going to use, and is it nowhere near
//     ~/dev/agent-os (D9)?
//   - Is --settings in the resulting harness argv (the silent-failure
//     hazard T10's package doc names)?
//   - What did cairn boot --json actually return — provider, scope,
//     settings path, project-dir arg name?
//   - What path will the boot directory land at (<boot-root>/<key>/current)?
//
// # This is explicitly not a product contract
//
// CONTRACT.md's frozen list/describe/launch seam was deleted with the old
// engine and is not recreated here in a new spelling: no stdout JSON
// contract, no exit-code contract, no stability promise. Its output shape
// may change between commits; nothing outside this command's own tests
// depends on it.
//
// # It spawns nothing
//
// This command shares its two packages with the app; it is not a second
// implementation of either. It does invoke the real `cairn boot --json`
// subprocess through internal/boot's own Invoke/Runner — that is a real
// read cairn performs against the bundle, not a spawn of the harness — but
// it never opens a terminal, never starts the harness process, and never
// calls internal/boot.Prepare, which is the one thing that would move an
// existing `current` boot directory aside. Running this command twice
// against the same target and boot root is therefore safe: the second
// `cairn boot` invocation finds `current` already occupied and refuses
// (cairn's own "boot directory already exists"), leaving the filesystem
// exactly as the first run left it. Dry-run means dry.
package main
