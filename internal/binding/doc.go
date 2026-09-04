// Package binding reads and writes Tachyon's bindings: named
// {profile, scope} pairs that `cairn boot <name>` looks up before falling
// back to a profile of the same id.
//
// # Why this package exists, and why it is not internal/bundle
//
// A binding lives as one file per binding under bindings/ at the bundle
// root — bindings/<name>.yaml, two top-level scalars, "profile" and
// "scope" — with scope aliases in a separate scopes.yaml beside it. That
// is what Cairn's own bindings/README.md documents, and what every real
// binding in ~/dev/projects/agent-setup looks like today.
//
// It was not always this shape. Earlier, both this package and Cairn read
// a single top-level file holding a `bindings:` map and a `scopes:` alias
// map together; this package's own history — CW-20260903-0011 (T07),
// which built the [Store]/[MemStore]/contract_test.go seam this package
// still uses, and CW-20260904-0003 (T24), which hardened this package's
// callers to fail loudly rather than silently once that single file
// disappeared out from under them — is preserved in git history and in
// those two tasks' own tracking records, not repeated here. CW-20260904-0002
// (T23) is the task that migrated this package's read and write path to
// bindings/ itself, which is what this file now describes as the present,
// not as a plan.
//
// internal/bundle's Bindings() also enumerates bindings/ — see that
// package's bundle.go — but it is a plain, format-blind directory listing:
// it does not open a file, does not know "profile" or "scope" are the keys
// that matter, and does not resolve an alias. That is where the plan drew
// the line: internal/bundle owns a six-artifact-kind tree as raw bytes
// (D5), and this package is the one place, above that, that actually
// interprets a binding's content — CRUD, alias resolution, and everything
// [Service] and internal/launch depend on. The two packages read the same
// directory name independently (see [dirName] here and internal/bundle's
// own dirBindings) because internal/bundle's copy carries no semantic
// weight; this package's [Open] is the one entry point anything that DOES
// care about a binding's meaning is required to use — see [dirName]'s own
// doc for the full reasoning, and the "exactly one place knows the storage
// format" bar this design keeps to.
//
// # The interface is the contract, not the file format
//
// [Store] is load, list, write — [Binding] in, [Binding] out. Nothing above
// Store may know that today's binding lives in one file per name under
// bindings/ rather than a single shared file, a database row, or anything
// else. [FileStore] is the only thing in this package, or in Tachyon, that
// has ever heard of [dirName] or [bindingExt]. [MemStore] is a second,
// deliberately trivial Store — an in-memory map — that exists to prove that
// claim: contract_test.go runs one shared test suite against both, so "swap
// the storage format" is demonstrated to be a one-file change (swap which
// Store a caller constructs) rather than merely asserted. Migrating this
// package from the old single shared file to bindings/ did not change
// [Store]'s method set, [MemStore]'s implementation, or a single assertion
// in runContract — only [FileStore]'s implementation, and contract_test.go's
// one line constructing it (a directory path, pre-created empty, rather
// than a single file's path — see that test's own comment on what "a fresh
// store" now means for a [FileStore] specifically), which is exactly what
// that seam was built to prove.
//
// # Scope is always a path
//
// A binding file's `scope` value is sometimes an alias — a key into
// scopes.yaml — rather than a literal path (`scope: chrispian`, where
// `chrispian` is a scopes.yaml key, not `~/dev/chrispian`). This package
// does not build scope-alias resolution as a feature: no alias picker, no
// alias CRUD, no editing scopes.yaml, and nothing a caller can use to save
// a new — or edited — binding whose scope is an alias name rather than a
// path.
//
// What this package's own acceptance bar requires, in the same breath, is
// that "a binding's scope surfaces as a path, never an alias" — and every
// one of the live bundle's eight entries has an alias for its scope today,
// so meeting that bar requires resolving the alias at read time, once,
// mechanically: look the scope token up in scopes.yaml, and if it is a key
// there, replace it with that key's literal value before the entry ever
// reaches a [Binding] this package hands out. That lookup is internal to
// [FileStore]; scopes.yaml's own contents are never exposed, [Binding] has
// no alias-shaped field, and Create/Update never accept anything but a
// literal path for Scope. See resolve.go for the lookup itself (unchanged
// by T23 — only where the alias map comes from changed, see aliases.go)
// and rejectAliasScope in aliases.go for the write-side fence.
//
// (This point was flagged explicitly by CW-20260903-0011's own
// implementation report: a Torque decision comment on that task's tracking
// record reads "do not build scope-alias resolution... do not expand
// `chrispian` to `~/dev/chrispian` anywhere," which taken fully literally
// would leave every live binding surfacing its raw alias token and fail
// this package's own acceptance bar in the same document. This package
// follows the worked example and acceptance criterion instead — resolve
// once at read time, never build a general alias feature, never let a
// write carry an alias — and that tension was called out for a human to
// confirm at the time; nothing about T23's migration reopens it.)
//
// # Bytes in, bytes out still governs — at the entry, not just the file
//
// A binding file is small — two or three lines — but it is not always
// bare: conductor.yaml and chrispian.yaml in the live bundle both carry a
// leading, load-bearing comment block ("The conductor holds no scope...",
// "Carried over from the previous system."). Decoding the whole file and
// re-encoding it — the "bytes in, bytes out" violation D5 names for the
// manager's text editor — would destroy that comment on the very first
// edit, even though the edited *values* are fully Tachyon's own content
// (the plan's "bindings are one of the two things Tachyon fully authors").
// "Fully authored" describes the profile/scope values a write changes, not
// the file they live in: a file that already exists may carry human
// commentary this package must leave alone.
//
// So [FileStore.Update] is surgical, not a re-serialization: scan.go's
// scanBindingFile locates the exact byte span of the "profile:" and/or
// "scope:" value token(s) that actually changed, and splice.go's splice
// replaces only those spans, leaving everything else in the file —
// comments, blank lines, any other top-level key — untouched. A no-op
// Update (called with the binding's own current values) writes nothing at
// all, the strongest form of that guarantee: not merely byte-identical,
// it never touches the file.
//
// [FileStore.Create] is the one deliberate exception, matching
// internal/skeleton's identical exception for a brand-new artifact of any
// kind (see that package's doc): there is no existing content to preserve
// when a binding is created for the first time, so Create writes the whole
// file, "profile: <p>\nscope: <s>\n", nothing else. [FileStore.Delete] is a
// whole-file removal for the same reason in reverse — there is nothing to
// carry through once the entry itself is gone.
//
// splice.go's own [splice] function — generic byte-span replacement in a
// []byte, with no opinion about what the spans mean or how big the file
// is — was written for the old single shared file, where dozens of entries
// lived in one document and a create or an edit had to locate and touch
// only its own corner of it. T23 considered deleting it: a 2-4 line file
// with its own leading comment looked, at first, like a case where writing
// the whole thing was simpler and safer than splicing. It is not, once
// Update has to preserve a leading comment block it does not itself write:
// [FileStore.Update] still needs to change exactly the "profile:" or
// "scope:" value token and nothing else in a file that may carry
// human-authored content before those two lines, which is precisely what
// [splice] already does correctly, generically, and with an existing test
// suite behind it. Rewriting an equivalent single-purpose "replace this
// span in this byte slice" helper inside filestore.go would only
// reintroduce the same logic under a different name. [splice] earns its
// place — the file it edits got smaller, not the shape of the problem.
//
// # Shape and existence only, now failed loud
//
// Per D8, nothing in this package validates a binding's *content* — it
// does not check that Profile names a real profile, or that Scope exists
// on disk. It does check *shape*: [scanBindingFile] requires a file to
// carry both a `profile:` and a `scope:` top-level key, each a recognized
// plain or quoted scalar (empty is fine — see internal/skeleton's binding
// scaffold), before this package will call it a binding at all.
//
// Under the old single shared file, a line that did not match the
// recognized shape was simply left alone — not listed, not editable — and
// the rest of the document still parsed, because one entry was one line
// among many. That is not the right choice for a directory of one file per
// binding: a whole file is now one binding, so a file this package cannot
// parse is not "one skipped line in an otherwise fine document," it is a
// bundle whose bindings/ directory partially cannot be read at all — and
// [FileStore.List] fails the whole call rather than silently returning a
// list with one entry quietly missing from it. A caller who got back an
// incomplete list with no error would have no way to know it was
// incomplete; a caller who gets an error naming the exact file that could
// not be parsed can go fix it, which matches T24's whole point: this
// package would rather fail loud than let a broken bundle read as an
// empty, or merely smaller, one. [FileStore.Get] is narrower on purpose —
// see "List and Get disambiguate differently," below.
//
// # No skills field
//
// The target architecture says a binding may optionally carry a skills
// list, and that `--save-as` round-trips `--skill`, but also says how that
// field is spelled in a binding file is undefined — the live bundle's
// eight entries carry only {profile, scope}. This package does not invent
// a spelling: [Binding] has no skills field, and neither Create nor Update
// accepts one.
//
// # List and Get disambiguate differently, on purpose
//
// [FileStore.List] must open every *.yaml file directly under bindings/ to
// enumerate them at all, so a parse failure anywhere in the directory
// necessarily surfaces during that walk — see "shape and existence only,
// now failed loud," above. [FileStore.Get] only ever touches
// bindings/<name>.yaml, the one file its caller asked about, plus
// scopes.yaml to resolve its scope: a different, unrelated binding file
// being corrupt does not stop Get from returning a name that is itself
// perfectly readable. This asymmetry is deliberate — internal/launch calls
// Get for exactly one binding on every launch, and a stranger's typo in a
// binding nobody is launching should not be able to block launching a
// different one.
//
// # The directory's own existence is real information
//
// CW-20260904-0003 (T24) went looking for a way to make an unreadable
// bindings store report a real error instead of a bare empty list — so the
// palette could show "I cannot read your bindings" rather than silently
// implying there were none — and concluded, correctly for what this
// package was at the time, that it could not be done safely: the single
// shared file's own absence was bit-for-bit indistinguishable between "a
// bundle that has never saved a binding" and "a bundle whose bindings used
// to live there and do not anymore," and telling them apart would have
// meant this package coupling to whatever *replaced* that file before this
// task existed to do exactly that.
//
// T23 changes that premise on purpose: this package now IS the thing that
// reads bindings/ itself, so its own existence is no longer a fact this
// package has to borrow from somewhere else — it is the fact this package
// is built around. And unlike the old single file (which was genuinely
// optional right up until a binding was first saved), a bindings/
// directory is not incidental: Cairn ships one — with bindings/README.md
// documenting the shape — as part of what a bundle *is*, the same standing
// every other artifact directory (profiles/, templates/, skills/, hooks/)
// already has in internal/bundle's own tree. A bundle root that resolves
// to a real directory but has no bindings/ under it at all is therefore
// treated as a real problem, not a quiet first-run state — the wrong
// bundle root, or an unset one defaulting somewhere that turns out not to
// be a real Cairn bundle, are the far more likely explanations day to day
// than "someone has a bundle with every other directory present and
// deliberately no bindings/." [ErrBindingsDirMissing] is what
// [FileStore.List] and [FileStore.Get] now return for exactly that case,
// and [Service.List]'s [ListResult] surfaces it to the frontend as
// State == "missing" — see the acceptance table in this task's own record
// for the three states Palette.jsx renders from it.
//
// This resolves the question T24 explicitly left open for a later task
// with a later premise to answer: production [FileStore.List]/[FileStore.Get]
// CAN now distinguish "never had bindings" (bindings/ exists, zero *.yaml
// files in it — [ListResult.State] == "ok" with an empty slice, the one
// case where "No bindings yet" is actually true) from "bindings/ is
// missing entirely" (State == "missing") from "bindings/ exists but cannot
// be fully read" (State == "unreadable", a wrong-shaped Dir or a file
// [scanBindingFile] rejects). All three are real, and none of them
// collapses into either of the others any more.
//
// Two consequences follow from that. First, internal/skeleton's binding
// scaffold (see that package's doc) is what is expected to bring
// bindings/ into existence on a bundle that has genuinely never had one —
// the same way [FileStore.Create] already brings the directory into
// existence via atomicWrite's os.MkdirAll if a binding is saved through
// this package directly — so "the directory is missing" reliably means
// "look at this bundle root," not "the very first binding has not been
// made yet through this exact code path." Second, internal/testbundle's
// own Resolve — see that package's doc — no longer needs a separate
// "treat zero results as unreadable" heuristic of its own: now that
// [FileStore.List] itself returns a real, non-nil error for both the
// missing and the unreadable cases (rather than the old behavior of
// silently returning an empty, error-free list for a missing file),
// testbundle.Resolve can just propagate whatever [FileStore.List] reports
// directly. That simplification is not incidental — it is the empirical
// proof that the disambiguation now really lives at this package's own
// boundary, in production code, rather than only in a test helper working
// around this package's limits.
package binding
