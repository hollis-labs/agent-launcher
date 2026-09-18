// Package compose turns one composition — what the palette built out of a
// role, skills, a project, one-off directions, additional parts and (at
// authoring time only) a template — into the argv for `cairn boot`.
//
// # Pure, on purpose
//
// [Build] runs nothing, spawns nothing, and writes nothing. It is a plain
// function from [Composition] to a []string and an error, which is what
// makes it exhaustively table-driven-testable without a fixture bundle, a
// real Cairn binary or a real filesystem. Running Cairn is a different
// package's job (plan task T10); computing where its boot directory lives is
// another's (T09). This package only builds the slice of arguments; it does
// not know or care what runs them, and it imports nothing from Wails and
// nothing that shells out (no os/exec anywhere in this package).
//
// # The mapping
//
// Every palette control maps onto exactly one part of the invocation:
//
//	control              reaches Cairn as
//	------------------   ------------------------------------------
//	role                 the boot target — Composition.Target, the
//	                     positional argument (a profile id or a
//	                     saved binding's name; Cairn disambiguates,
//	                     this package does not need to know which)
//	skills               one comma-joined --skill flag
//	prompts              one comma-joined --prompt flag (CW-20260904-0006)
//	                     — a name, never a prompt's own content; see
//	                     "no delivery" below
//	project / path       --scope <path>
//	provider             --provider <name>, omitted entirely when the
//	                     control is empty — which is the ordinary case
//	                     and means "whatever the resolved profile
//	                     cascade declares" (CW-20260906-0001)
//	one-off direction    --set <slot>=<value>, one flag per Set
//	additional parts     --with <name>, one flag per part
//	template             nothing — authoring-time only (D4); there
//	                     is no field for it here
//
// [Build] renders these, plus --profile (the active bundle root) and
// --boot-root, in the fixed order the target contract specifies:
//
//	boot <target> --profile <bundle> --boot-root <root> --session current
//	     [--provider <name>] [--with <part>]... [--skill <a,b,c>]
//	     [--prompt <a,b,c>] [--set <slot>=<value>]... [--scope <path>] --json
//
// --session current and --json are unconditional — every invocation this
// package builds carries both, with no Composition field controlling either
// (see T09 for why the session id is always "current"; --json is how
// whatever runs this argv gets machine-readable output back).
//
// Skills and Prompts are each one flag, not one per item: the mapping table
// shows "--skill a,b,c" and "--prompt a,b,c", not "--skill a --skill b" /
// "--prompt a --prompt b", and that shape is deliberate here — they are the
// two rows in the table that render as a single flag carrying a joined
// list, distinct from --with and --set, which each get their own flag per
// item. Order within that list, within --with, and within --set is
// preserved exactly as given in Composition; this package does not sort or
// deduplicate anything a caller handed it (D8's shape and existence
// spirit — the caller owns what a value means, this package owns only
// where it lands in argv).
//
// # A provider is a target, never an inference (CW-20260906-0001)
//
// Composition.Provider is the harness one launch materializes into, and it
// arrives from a control a person set — nothing in this package derives it.
// In particular nothing reads Target to guess one: a binding named
// "codex-coord-agent-setup" says nothing about a harness, and a launcher
// that read one out of a name would render a Codex layout the first time
// somebody named a binding after a project rather than a tool, silently. A
// binding that should boot Codex says so in the content it resolves — its
// own profile cascade, or a part declaring `provider: codex` — which is
// also how the palette's direct Enter-on-a-binding path, which builds no
// Composition beyond Target/Bundle/BootRoot, lands on the right harness.
// See TestProviderIsNeverInferredFromTheTarget.
//
// The value is not validated here either (D8). Cairn refuses a word that
// names no harness it knows, and distinguishes that from a harness it knows
// and cannot yet render — two different answers a second list in this
// package could only disagree with.
//
// # No delivery (CW-20260904-0006)
//
// Composition.Prompts carries prompt NAMES, resolved by Cairn against
// prompts/ the same way Composition.Skills carries skill names resolved
// against skills/ — never a prompt's own content. This package has no
// field anywhere for prompt bytes, reads no prompt file, and would have
// nowhere in argv to put one even if it did: --prompt takes names, and
// nothing in the target contract accepts prompt content over argv, stdin,
// or any other channel. A person types /boot:<name> once a session is
// running (Cairn plants the file at .claude/commands/boot/<name>.md); this
// package's whole job stops at getting the name into --prompt.
//
// # The hazard this package exists to close (D9)
//
// Cairn's own default boot root is dev/agent-os/runtime/boot under the
// user's home directory — and ~/dev/agent-os is a real git working tree,
// measured 2026-09-02 at 1,249 dirty files on its own branch. Nothing in
// Cairn checks whether a boot root lands inside a working tree. A boot
// planted at that default becomes an agent's cwd inside that repo, and
// `git add -A && git commit` from there commits all of it. The only thing
// preventing that today is CAIRN_BOOT_ROOT exported in one interactive
// shell, which a GUI app started from Finder or Spotlight does not inherit.
//
// So Composition.BootRoot is not optional and [Build] does not default it.
// A caller — T09's boot directory, ultimately — computes a value under
// Tachyon's own state directory (internal/state.StateDir, the single definition
// internal/shell.DefaultPrefsPath and internal/bundle.DefaultRootStore also
// build their own paths from) and passes it in. [Build] requires it
// non-empty and returns [ErrNoBootRoot]
// rather than silently proceeding without it — an argv missing --boot-root,
// generated by this package, would depend on whatever CAIRN_BOOT_ROOT
// happens to be set to in the process that eventually runs it, which is
// exactly the failure mode D9 names. compose_test.go's
// TestBootRootNeverImplicit is the guard this buys: every composition
// [TestBuild] exercises is replayed there, asserting --boot-root is present
// in the resulting argv and that its value sits under Tachyon's state
// directory rather than under ~/dev.
//
// # What this package deliberately does not build
//
// No ephemeral-part synthesis: --skill supersedes the older design where
// Tachyon generated a part file for skills, and Composition.Skills renders
// as a flag, never as a file this package writes (it writes no files at
// all). No AGENTS.md scope scrape: that was only ever a fallback for a
// world where --json lagged, and --json is part of the target contract this
// package builds against. No --db, no sqlite reference and no seed step
// anywhere — that store is being removed elsewhere in this rebuild, and
// nothing here should ever reach for it.
package compose
