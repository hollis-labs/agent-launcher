// Package launchprofile is Tachyon's store of launch profiles: the files
// that say HOW an agent runs, as distinct from the bundle, which says what
// the agent IS.
//
// # What a launch profile is
//
// An ordinary Cairn part. Not a format of Tachyon's own — a file this
// package writes is exactly what `cairn boot <profile> --with <path>` takes,
// and Cairn resolves it through the same cascade as any profile in the
// bundle. The frontmatter vocabulary is Cairn's (id, extends, abstract,
// name, description, provider, model, spec) and this package validates none
// of it beyond what it must read to show a list.
//
//	---
//	id: codex
//	provider: codex
//	spec:
//	  settings:
//	    codex:
//	      approval_policy: never
//	      sandbox_mode: workspace-write
//	---
//
// That choice is the whole design. Tachyon held a private YAML shape before
// — bindings, `{name, profile, scope}` — which Cairn could not read, so the
// launcher and the materializer disagreed about what a saved launch was and
// only one of them could be checked. A launch profile is checkable by the
// tool that consumes it: `cairn show <profile> --with <path>` reports it as a
// chain member and a settings contributor, which is what internal/preview
// puts in front of a person before they launch.
//
// # Why these files are not in the bundle
//
// Ruled 2026-09-10 (Tesseract cairn_is_a_template_engine_not_an_authority,
// agent_setup_declares_no_runtime): agent-setup owns content and declares no
// runtime, Cairn materializes a directory and knows its shape only where it
// must, and the launcher owns everything about a launch. agent-setup removed
// all 34 bindings and every `provider:` line in the same pass; Cairn dropped
// bindings and `--save-as` on top of it. What is left of a launch —
// the provider, the sandbox posture, the settings — has no home but here.
//
// So the store is Tachyon's own directory ([state.LaunchDir],
// ~/.config/tachyon/launch), under config rather than state because these are
// durable choices a person writes, edits and will want in git — not
// something Tachyon regenerates.
//
// # What a launch profile does NOT carry
//
// Scope. Cairn refuses `scope:` in frontmatter outright — it is not one of
// the eight keys — because a scope belongs to the launch and not to the
// agent. Tachyon supplies it as `--scope`, from the project selected
// alongside the launch profile. Two axes: this package says how, the project
// says where, and neither has to know about the other. A launch with no
// project selected passes no --scope at all, which Cairn accepts and which is
// exactly right for a profile like conductor that holds no scope of its own.
//
// # Reading is narrow on purpose
//
// [Parse] reads three keys — id, provider, description — and ignores the
// rest of the document. It exists to label a row in the palette, not to
// understand a launch profile. Anything wrong with the file that matters is
// wrong in a way Cairn reports, at the moment it matters, with a diagnostic
// written by the component that owns the rule; a second, necessarily
// partial validator here could only disagree with that one. The single
// exception is the file's own name, which this package does constrain,
// because a name becomes a filename with no escaping.
package launchprofile
