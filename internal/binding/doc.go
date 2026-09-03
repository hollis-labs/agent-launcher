// Package binding reads and writes Tachyon's bindings: named
// {profile, scope} pairs that `cairn boot <name>` looks up before falling
// back to a profile of the same id.
//
// # Why this package exists, and why it is not internal/bundle
//
// The target architecture (plan CW-20260518-0061, D4) described bindings as
// one file per binding under a bindings/ directory. [R3], corrected
// 2026-09-03: that directory does not exist. What exists, and what Cairn
// reads today, is a single top-level file — <bundle root>/bindings.yaml —
// holding a `bindings:` map and a `scopes:` alias map. Chrispian chose to
// adopt the existing file rather than migrate it.
//
// internal/bundle's Bindings() deliberately still enumerates the bindings/
// directory and returns empty against the live bundle — see that package's
// bundle.go and its live_test.go census, which pins liveBindings = 0. That is
// not a bug this package works around; it is where the plan drew the line:
// internal/bundle owns the six-artifact-kind tree, bindings.yaml is a
// different shape entirely (two maps, not a directory of files), and giving
// bundle a seventh, structurally different read path would blur the one
// property that makes it testable — that it is a plain filesystem walk with
// no format opinions. This package owns bindings.yaml on its own, and reads
// the bundle root bundle.RootStore already persists (see [Service]) rather
// than duplicating that setting.
//
// # The interface is the contract, not bindings.yaml
//
// [Store] is load, list, write — [Binding] in, [Binding] out. Nothing above
// Store may know that today's binding lives in one YAML file rather than one
// file per binding, a database row, or whatever Cairn's bindings/ directory
// ends up being once its format is pinned. [FileStore] is the only thing in
// this package, or in Tachyon, that has ever heard of bindings.yaml.
// [MemStore] is a second, deliberately trivial Store — an in-memory map —
// that exists to prove that claim: contract_test.go runs one shared test
// suite against both, so "swap the storage format" is demonstrated to be a
// one-file change (swap which Store a caller constructs) rather than merely
// asserted.
//
// # Scope is always a path
//
// bindings.yaml's `bindings:` entries carry a `scope` value that today is
// often an alias — a key into the file's own `scopes:` map — rather than a
// literal path (`{ profile: planner, scope: chrispian }`, where `chrispian`
// is a `scopes:` key, not `~/dev/chrispian`). The plan is explicit that
// scope-alias resolution is not something this package builds as a feature:
// no alias picker, no alias CRUD, no `scopes:` editing, and nothing a caller
// can use to save a new — or edited — binding whose scope is an alias name
// rather than a path.
//
// What the plan's own acceptance bar requires, in the same breath, is that
// "a binding's scope surfaces as a path, never an alias" — and every one of
// the live file's eight entries has an alias for its scope today, so meeting
// that bar requires resolving the alias at read time, once, mechanically:
// look the scope token up in `scopes:`, and if it is a key there, replace it
// with that key's literal value before the entry ever reaches a [Binding]
// this package hands out. That lookup is internal to [FileStore]; the
// `scopes:` map itself is never exposed, [Binding] has no alias-shaped field,
// and Create/Update never accept anything but a literal path for Scope. See
// resolve.go.
//
// (This point is flagged explicitly in the implementation report: a Torque
// decision comment on this task's tracking record reads "do not build
// scope-alias resolution... do not expand `chrispian` to `~/dev/chrispian`
// anywhere," which taken fully literally would leave every one of the eight
// live bindings surfacing its raw alias token and fail the plan's own
// acceptance bar in the same document. This package follows the plan body's
// explicit worked example and acceptance criterion — resolve once at read
// time, never build a general alias feature, never let a write carry an
// alias — and that tension is called out for a human to confirm.)
//
// # Bytes in, bytes out still governs — at the entry, not just the file
//
// bindings.yaml is hand-authored: a load-bearing header comment, inline-flow
// mappings (`{ profile: planner, scope: chrispian }`), alignment padding, and
// section comments inside the `bindings:` map ("Carried over from the
// previous system.", "The conductor holds no scope..."). Decoding the whole
// document and re-encoding it — the "bytes in, bytes out" violation D5 names
// for the manager's text editor — would destroy every one of those on the
// very first write, even though a new binding is fully Tachyon's own content
// (D4's "bindings are one of the two things Tachyon fully authors").
// "Fully authored" describes the new entry, not the file it lands in: the
// file already carries eight entries and human commentary around them.
//
// So writes here are surgical, not a re-serialization: [FileStore] locates
// the exact byte span of the one thing that changed — one entry's line for a
// create or a delete, one field's value token for an edit — and splices only
// that span. Everything else in the file, including formatting this package
// does not understand, is carried through unread and unwritten. See scan.go
// for how an entry's on-disk spans are located, and filestore.go for how they
// are used. A no-op Update (called with the binding's own current values)
// writes nothing at all, which is the strongest form of that guarantee: it is
// not merely byte-identical, it never touches the file.
//
// # Shape and existence only
//
// Per D8, elsewhere in this plan, nothing validates content. This package
// keeps to that in its own narrow way: an entry whose line is not the
// recognized `key: { profile: x, scope: y }` flow-map shape is not listed and
// not editable through this package — it is left alone, exactly as written,
// rather than guessed at or coerced. Every entry in the live file today is in
// that shape; a hand edit that leaves one in some other shape is the user's
// prerogative, not a parse error this package raises.
//
// # No skills field
//
// The target architecture says a binding may optionally carry a skills list,
// and that `--save-as` round-trips `--skill`, but also says how that field is
// spelled in bindings.yaml is undefined — the eight live entries carry only
// {profile, scope}. This package does not invent a spelling: [Binding] has no
// skills field, and neither Create nor Update accepts one.
package binding
