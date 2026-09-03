// Package bundle reads the agent-setup bundle: the directory tree that holds
// Chrispian's agent system as text.
//
// The bundle is content. Cairn materializes it; Tachyon authors it. Nothing
// here shells out to cairn — the tree is read directly, so `cairn list` is
// useful but is not a dependency (plan CW-20260518-0061, D3).
//
// # The root is a parameter
//
// There is no compiled-in bundle path in the read path. [Open] takes a root;
// [DefaultRoot] and [RootStore] supply and persist one for a caller that wants
// the usual bundle, and both are ordinary values a test can replace.
//
// # Two things are called "role", and they are not the same
//
// A [Profile] (profiles/<id>.md) is the composable unit — frontmatter, slots,
// skills, extends. A [RoleProse] (templates/roles/<id>.md) is the prose a
// profile pulls in through its "role" slot. Every one of the eight role prose
// files shares a basename with a profile, so name is not a discriminator and
// never can be. They are separate Go types with separate id types, so the
// compiler refuses the substitution and a UI cannot conflate them.
//
// # Bytes in, bytes out
//
// [Bundle.Read] returns a file's bytes exactly as they sit on disk. Nothing in
// this package parses and re-serializes a file: there is no YAML document
// model, no formatter, no newline normalization (D5). [Header] is a shallow
// scan of a frontmatter block for four fields a tree wants to display; it is
// deliberately lossy and deliberately not a save path.
//
// # Shape and existence only
//
// Per D8 this package validates nothing. It does not resolve extends, does not
// check that a slot's marker exists in a template, does not check that a named
// skill is present, and does not detect cycles. A file that fails to parse
// still enumerates — the user owns correctness, and an artifact hidden from
// the tree is an artifact that cannot be fixed.
//
// A missing artifact directory enumerates as empty rather than as an error.
// bindings/ in particular does not exist in the bundle today; its per-file
// format is not pinned by Cairn and is owned by a later task, so this package
// enumerates whatever files are there and reads their bytes, nothing more.
//
// # An empty answer is never how a missing bundle is reported
//
// The rule above stops at the root. A root that has gone away — a renamed
// repo, a branch switch, a typo in the path — is [ErrRootMissing] from every
// enumeration, from Resolve and from Read, never a successful empty result.
// An empty tree that means "your bundle is gone" and an empty tree that means
// "your bundle is empty" have to be different values, or the first one is
// invisible. For the same reason a [Header] that could not be read is not the
// same value as a file that simply has no frontmatter.
//
// # Ordering carries no meaning
//
// Resolution in Cairn is deterministic by key; every spec collection is a set
// or a map. This package does not read spec at all, so it cannot leak an
// implied order into a UI. Enumerations are sorted by id purely so that a tree
// renders the same way twice.
package bundle
