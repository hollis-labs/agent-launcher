package boot

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
)

// safeSeed matches a seed that is already a single filesystem-safe path
// segment on its own — deliberately the same character set
// internal/launchprofile's nameRe restricts a launch profile's name to, and
// the same one a bare agent profile id already satisfies, so that both pass
// through [Key] unchanged. See this package's doc comment for why that
// identity matters.
var safeSeed = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// maxSlugLen bounds the human-readable prefix [Key] builds for a seed that
// is not already safe, so a very long composition identity string cannot
// produce an unwieldy directory name. The trailing hash is what carries the
// collision guarantee, not the slug, so truncating the slug costs nothing
// but readability.
const maxSlugLen = 48

// Key derives the stable, filesystem-safe directory key for seed: an agent
// profile id, or any canonical identity string a caller builds from a
// composition's content. The same seed always yields the same key, across
// restarts, and two different seeds never yield the same key.
//
// If seed is already a safe single path segment, Key returns it unchanged.
// Otherwise Key returns a short sanitized slug of seed, for a human
// browsing the boot root, followed by a hyphen and the full SHA-256 hex
// digest of seed — the digest, not the slug, is what makes the result
// collision-safe, since it is a lossless function of the entire seed rather
// than of the lossy slug two different seeds might sanitize down to.
func Key(seed string) string {
	if safeSeed.MatchString(seed) {
		return seed
	}

	sum := sha256.Sum256([]byte(seed))
	digest := hex.EncodeToString(sum[:])

	slug := sanitizeSlug(seed)
	if slug == "" {
		return digest
	}
	return slug + "-" + digest
}

// sanitizeSlug produces a best-effort, human-readable, filesystem-safe
// prefix from an arbitrary seed. It is not required to be unique or
// reversible — [Key] never relies on sanitizeSlug alone for the collision
// guarantee, only on the hash appended after it.
func sanitizeSlug(seed string) string {
	var b strings.Builder
	for _, r := range seed {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}

	slug := strings.Trim(b.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
	}
	return slug
}

// shortDigestLen is how many hex characters of a seed's SHA-256 [ProjectKey]
// appends to disambiguate two scopes whose last path segment agrees.
//
// Ten is 40 bits. The set it has to be unique within is one person's boot
// root — the projects they actually launch into, tens of directories rather
// than millions — and a collision there costs a shared boot directory rather
// than a security property. [Key]'s full 64-character digest is the right
// answer where the seed is arbitrary and the guarantee is absolute; it is the
// wrong answer for a directory name a person reads, and the 105-character
// segments it produced are what this replaced.
const shortDigestLen = 10

// noProject is the first path segment for a launch with no scope.
//
// Spelled rather than left empty: an empty segment would collapse
// <root>/<project>/<profile> to <root>/<profile>, putting an unscoped launch
// in a directory that could later collide with a project named after a
// profile. It is also the one segment a person will want to recognize.
const noProject = "no-project"

// ProjectKey derives the FIRST path segment of a boot directory: the project
// the launch works in.
//
// The layout is <boot-root>/<project>/<profile>/<launch profile>, and this
// is the outermost of the three. Grouping by project rather than by role is
// what makes the tree browsable — a person looks for "what is running on
// cairn", not "every scope engineer has ever been booted at".
//
// It is derived from the SCOPE and never from a project's name, even though
// the palette has one. A name and a path can diverge: someone selects a
// project and then edits the scope field, or renames a project without
// moving it. The scope is what actually reaches cairn and what the rendered
// settings document grants, so it is the only thing that can identify the
// directory without the identity and the content disagreeing.
//
// The result is the scope's last segment plus a short digest of the whole
// path: readable, and unique because two projects can share a basename
// (~/dev/projects/frag and ~/dev/other/frag) and sharing a boot directory
// would hand a running session a grant naming the other one.
func ProjectKey(scope string) string {
	if scope == "" {
		return noProject
	}
	digest := sha256.Sum256([]byte(scope))
	short := hex.EncodeToString(digest[:])[:shortDigestLen]

	slug := sanitizeSlug(filepath.Base(filepath.Clean(scope)))
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
	}
	if slug == "" {
		// A scope whose last segment sanitizes to nothing — "/" itself, or
		// a name of only punctuation. The digest alone still identifies it.
		return short
	}
	return slug + "-" + short
}

// SessionKey derives the LAST path segment: the launch profile this
// composition runs under.
//
// It is the third of the three axes, and the other two are already segments
// of their own — the project above and the agent profile in between, which
// is cairn's own <target>. So this carries exactly what is left.
//
// A launch profile's name is already a safe path segment
// (internal/launchprofile's nameRe is [Key]'s safeSeed), so in practice this
// returns it unchanged and the leaf reads as "codex" or "default" rather
// than as a hash. [Key] is still what does it, because a name that somehow
// is not safe must not be joined onto a path raw.
//
// Empty means no launch profile, which cairn refuses for want of a provider.
// It still gets a stable segment rather than an empty one: a refused launch
// plants nothing, but [Prepare] runs first and must not be handed "".
//
// The result is stable forever for a given launch profile, which — with the
// two segments above it — is the property ~/.claude.json's per-path trust
// entries depend on. See [DefaultSession].
func SessionKey(launchProfile string) string {
	if launchProfile == "" {
		return DefaultSession
	}
	return Key(launchProfile)
}

// ProjectRoot is the boot root cairn is given: bootRoot with the project
// segment appended.
//
// The layout is <boot-root>/<project>/<profile>/<launch profile>, and cairn
// plants only the last two — its own layout is <boot-root>/<target>/<session>
// — so the project segment has to be folded into what it is handed as its
// root.
//
// An EMPTY bootRoot stays empty, and that guard is the whole reason this is a
// function rather than a filepath.Join at each call site. Joining "" with a
// project key yields a RELATIVE path, which compose.Build would accept: an
// empty boot root has to stay empty so its own refusal (compose.ErrNoBootRoot)
// still fires. D9's hazard is a boot root a caller never chose, and a relative
// one resolved against whatever the process's cwd happens to be is exactly
// that, with the additional charm of being silent.
func ProjectRoot(bootRoot, scope string) string {
	if bootRoot == "" {
		return ""
	}
	return filepath.Join(bootRoot, ProjectKey(scope))
}
