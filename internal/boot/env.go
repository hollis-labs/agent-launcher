package boot

import (
	"fmt"
	"regexp"
	"strings"
)

// BootDirPlaceholder is the token Cairn's --json report leaves standing in
// env_amendments wherever the boot directory goes — "CODEX_HOME={{.BootDir}}"
// is the only amendment any implemented provider declares today. Named to
// match go-providers' own spelling, exactly as [ProjectDirPlaceholder] is.
const BootDirPlaceholder = "{{.BootDir}}"

// envKeyRe is what the key half of an amendment must match to be safe to
// place, unquoted, in front of a command in the shell line [shellCommand]
// builds: a POSIX shell variable name and nothing else. A key that does not
// match is refused rather than quoted, because there is no quoting that makes
// `FOO BAR=x cmd` mean "set FOO BAR" — the shell would read it as a command
// named FOO. See [Environment].
var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// placeholderRe matches any Go-template-shaped placeholder, so [Environment]
// can refuse one it does not know rather than export it literally. A launched
// process carrying CODEX_HOME={{.SomethingNew}} would not fail: it would point
// the harness at a directory named after the template, and the operator would
// get a session with no credentials and no explanation.
var placeholderRe = regexp.MustCompile(`\{\{[^}]*\}\}`)

// Environment expands result.EnvAmendments into the KEY=VALUE entries a
// launcher must add to the environment of the process it spawns — the actual
// terminal process, not the osascript subprocess that asks for it. It returns
// nil, not an empty slice, when the provider declares no amendments, matching
// Cairn's own spelling of "nothing to add" (env_amendments is null for Claude
// Code, and a slice of one entry for Codex).
//
// Cairn deliberately does not substitute these itself: it writes a directory
// and describes it, and the process that has a child to put a variable into is
// the one that spawns the harness. So substitution is Tachyon's, and it is
// performed here — once, from one Result — rather than at the spawn site,
// where it would have to be repeated for every future provider.
//
// Two placeholders are known, and they are exactly the two Cairn's report
// documents: [BootDirPlaceholder] expands to result.BootDir, and
// [ProjectDirPlaceholder] to result.Scope. A {{...}} this function does not
// know is a refusal and never a literal export — see [placeholderRe]. A
// {{.ProjectDir}} with a nil Scope is a refusal too, for the same reason
// [Result.ProjectDirArgv] returns nil there rather than substituting "": an
// amendment naming a directory Cairn resolved none for has no correct value,
// and the empty string is the one value that looks like an answer.
//
// A malformed amendment — no "=", or a key that is not a shell variable name
// — is refused rather than passed through. Cairn builds these from a provider
// adapter's own declaration, so a malformed one means the adapter changed
// shape underneath both of us, which is precisely when a launcher must stop
// rather than improvise.
func Environment(result Result) ([]string, error) {
	if len(result.EnvAmendments) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(result.EnvAmendments))
	for _, amendment := range result.EnvAmendments {
		key, value, found := strings.Cut(amendment, "=")
		if !found {
			return nil, fmt.Errorf("boot: env amendment %q has no %q", amendment, "=")
		}
		if !envKeyRe.MatchString(key) {
			return nil, fmt.Errorf("boot: env amendment %q: %q is not a shell variable name", amendment, key)
		}

		value = strings.ReplaceAll(value, BootDirPlaceholder, result.BootDir)
		if strings.Contains(value, ProjectDirPlaceholder) {
			if result.Scope == nil {
				return nil, fmt.Errorf("boot: env amendment %q names %s but cairn reported no scope",
					amendment, ProjectDirPlaceholder)
			}
			value = strings.ReplaceAll(value, ProjectDirPlaceholder, *result.Scope)
		}
		if left := placeholderRe.FindString(value); left != "" {
			return nil, fmt.Errorf("boot: env amendment %q carries a placeholder tachyon does not know: %s",
				amendment, left)
		}

		out = append(out, key+"="+value)
	}
	return out, nil
}
