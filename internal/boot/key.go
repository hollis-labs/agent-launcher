package boot

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// safeSeed matches a seed that is already a single filesystem-safe path
// segment on its own — deliberately the same character set
// internal/binding's nameRe restricts a binding's Name to, so that a saved
// binding's name always passes through [Key] unchanged. See this package's
// doc comment for why that identity matters.
var safeSeed = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// maxSlugLen bounds the human-readable prefix [Key] builds for a seed that
// is not already safe, so a very long composition identity string cannot
// produce an unwieldy directory name. The trailing hash is what carries the
// collision guarantee, not the slug, so truncating the slug costs nothing
// but readability.
const maxSlugLen = 48

// Key derives the stable, filesystem-safe directory key for seed: a saved
// binding's name, or, for an unsaved composition, a canonical identity
// string the caller builds from the composition's content (its base
// profile, parts, scope, skills — whatever combination of fields identifies
// "the same composition" to the caller). The same seed always yields the
// same key, across restarts, and two different seeds never yield the same
// key.
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
