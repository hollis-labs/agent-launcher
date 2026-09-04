// Package skeleton renders a starting file for a brand-new bundle artifact —
// a role profile, a template, a piece of role prose, a skill, or a
// binding — and writes it into the bundle so
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
// This package can create six of the bundle's seven artifact kinds:
// profile, template, role prose, skill, prompt (CW-20260904-0006), and —
// since CW-20260904-0002 (T23) — binding (see [SupportedKinds]).
// bundle.KindHook is the one kind deliberately absent from [registry]; hook
// creation is out of this package's scope entirely, unrelated to bindings.
//
// bundle.KindBinding stayed absent for longer than the other four: its
// scaffold depended on what CW-20260903-0011 (T07) settled as the shape of
// a binding, a task running concurrently in a sibling worktree as most of
// this package was first written, and Cairn's own storage for bindings
// moved out from under T07's implementation shortly after
// (CW-20260904-0002 / T23 — see internal/binding's own doc for that
// history). Guessing the shape early would have either been thrown away
// once a real interface existed, or worse, shipped a shape internal/binding's
// own read/write code could not parse.
//
// Adding it, now that internal/binding's directory-of-one-file-per-binding
// interface is real, needed exactly one more [registry] entry: bindingScaffold
// in scaffolds.go for the starting content, and
// [github.com/hollis-labs/tachyon/internal/binding.BindingRelPath] — not a
// relPath function defined in this package — for where a new binding
// lands, so bindings/'s directory name and file extension stay known in
// exactly the one place internal/binding itself defines them. Nothing else
// in this package, in internal/manager's NewArtifact, or in the frontend's
// creatable-kind list needed to change: SupportedKinds() and
// internal/manager.Service.NewArtifactKinds both pick it up automatically,
// the same "one more registry entry" seam this doc described before T23
// landed. See scaffolds.go's own bindingScaffold doc for why its starting
// content does not go through internal/binding's own
// [github.com/hollis-labs/tachyon/internal/binding.Store.Create].
package skeleton
