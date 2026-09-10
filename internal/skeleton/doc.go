// Package skeleton renders a starting file for a brand-new bundle artifact —
// a role profile, a reusable part profile, a template, a prompt or a skill —
// and writes it into the bundle so
// [github.com/hollis-labs/tachyon/internal/bundle]'s
// next read finds it and [github.com/hollis-labs/tachyon/internal/manager]'s
// tree shows it, with no restart and no cache to invalidate (bundle.Bundle
// caches nothing; see its package doc).
//
// # The one exception to bytes in, bytes out
//
// internal/bundle's D5 rule — "Tachyon never reserializes a file it did not
// generate" — governs an EXISTING artifact a user is editing. A skeleton is
// the one deliberate exception: there are no user bytes yet to preserve when
// a file is created (plan CW-20260518-0061, D5), so [New] is allowed to
// author one whole. The moment it lands on disk, though, it is an ordinary
// file the manager's editor opens and saves like any other — D5 governs it
// unconditionally from that point forward. This package's tests prove that
// handoff by round-tripping a freshly created file through
// internal/manager's real Open/Save path, not merely through this package's
// own New, per CW-20260903-0010's last acceptance criterion. [NewPart] is
// the placement-specific entry point for the same handoff under
// profiles/parts; it still returns an ordinary profile ref.
//
// # Nothing here validates content (D8)
//
// A scaffold's comments are advisory, not enforced: this package writes them
// once, at creation, and never checks them again, and no other part of
// Tachyon validates a scaffold once the user starts editing it.
//
// What a scaffold says therefore has to be true of the engine that is
// actually running, because nothing will catch it if it is not. The template
// and profile scaffolds used to teach the marker engine — `cairn:slot`,
// `cairn:value`, and the six instance facts a value filled from. agent-setup
// moved off that engine on 2026-09-10: profiles became templates in their
// own right, with `{{ extends }}` / `{{ section }}` / `{{ yield }}` in the
// profile and `{{ file: ... }}` naming a template. Teaching markers now would
// hand someone a document the live bundle has no reader for, which is why
// TestScaffoldsPlaceNoRetiredMarkers is a negative guard rather than the
// three positive ones it replaced.
//
// The profile scaffold declares no provider, for the same class of reason:
// no profile in agent-setup declares one, a runtime is a launch's to choose,
// and a scaffold that wrote one would put it back one new profile at a time.
//
// # What it can create, and what it cannot
//
// Four of the bundle's five artifact kinds: profile (and, through [NewPart],
// a profile placed under profiles/parts), template, prompt and skill.
// bundle.KindHook is the one deliberately absent from [registry]; hook
// creation is out of this package's scope entirely.
//
// Two kinds it used to create are gone with their directories.
// bundle.KindRoleProse wrote templates/roles/<id>.md, and bundle.KindBinding
// wrote bindings/<id>.yaml through internal/binding's own path helper. Both
// directories left agent-setup on 2026-09-10 — a role's prose is a
// `{{ section charter }}` in the profile now, and launch configuration is
// the launcher's and lives in internal/launchprofile.
//
// Adding a kind back, or adding a new one, is still one [registry] entry:
// [SupportedKinds] and internal/manager.Service.NewArtifactKinds both read
// that map, and the frontend's creatable-kind list is filtered against what
// they return rather than hardcoding its own.
package skeleton
