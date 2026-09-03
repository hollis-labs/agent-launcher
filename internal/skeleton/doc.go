// Package skeleton renders a starting file for a brand-new bundle artifact —
// a role profile, a template, a piece of role prose, or a skill — and writes
// it into the bundle so [github.com/hollis-labs/tachyon/internal/bundle]'s
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
// own New, per CW-20260903-0010's last acceptance criterion.
//
// # Nothing here validates content (D8)
//
// A scaffold's comments are advisory, not enforced: this package writes them
// once, at creation, and never checks them again, and no other part of
// Tachyon validates a scaffold once the user starts editing it. In
// particular the template scaffold documents two rules Cairn itself does not
// check — the six names cairn:value fills from, and how a marker's own line
// behaves when it substitutes nothing — because nothing else says them.
//
// # The binding seam
//
// This package can create four of the bundle's five creatable artifact
// kinds: profile, template, role prose, and skill (see [SupportedKinds]).
// bundle.KindBinding is deliberately absent from [registry]. Its scaffold
// depends on what CW-20260903-0011 (T07) settles as the shape of a binding —
// a task running concurrently in a sibling worktree as this package was
// written, and not yet landed. Guessing the shape here would either be
// thrown away once T07's real interface exists, or worse, ship a shape T07's
// own read/write code cannot parse.
//
// Adding it once T07 lands is exactly one more [registry] entry — a relPath
// function naming where a new binding lands, and a scaffold function for its
// starting content — and nothing else in this package, in
// internal/manager's NewArtifact, or in the frontend's creatable-kind list
// needs to change. See the comment on [registry] for the precise seam, and
// internal/manager.Service.NewArtifactKinds for how the frontend learns which
// kinds are live without hardcoding the list twice.
package skeleton
