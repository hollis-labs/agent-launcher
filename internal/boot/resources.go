package boot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// homeDefaults names, per environment variable, where that provider's home
// lives when the operator has not exported the variable themselves. It is
// keyed by the variable Cairn's own env_amendments names, so the only thing
// Tachyon adds is the default path — the variable's spelling is still read
// from the report.
//
// Each value is relative to the operator's home directory. A variable absent
// from this table is a refusal, not a guess: pointing a provider's home at a
// boot directory without knowing where its real home was is how a launcher
// silently produces an unauthenticated session.
var homeDefaults = map[string]string{
	"CODEX_HOME": ".codex",
}

// ErrHomeResource is the class of failure [PrepareHomeResources] reports:
// the operator-owned resources Cairn named cannot be provided as they stand.
// Every error it wraps names the exact path involved and what to do about it.
// Test for it with errors.Is.
var ErrHomeResource = errors.New("boot: provider home resource")

// HomeRedirectKey returns the one environment variable result's amendments
// repoint at the boot directory — "CODEX_HOME" for a Codex boot — or "" when
// the provider redirects no home at all (Claude Code amends nothing).
//
// It is read off the amendments rather than off Provider, so the variable's
// spelling stays Cairn's and this package keeps exactly one hardcoded fact
// about Codex: where its real home lives by default (see [homeDefaults]).
// An amendment redirects a home when its value is [BootDirPlaceholder] and
// nothing else — that is precisely the statement "this variable now names
// the boot directory," which is what makes the operator's own value for it
// the source the resources must come from.
//
// More than one such amendment is an error rather than a choice: two homes
// redirected at one directory is a provider shape this package has never
// seen, and picking the first would be a guess about which one owns
// HomeResourcePaths.
func HomeRedirectKey(result Result) (string, error) {
	var keys []string
	for _, amendment := range result.EnvAmendments {
		key, value, found := strings.Cut(amendment, "=")
		if found && strings.TrimSpace(value) == BootDirPlaceholder {
			keys = append(keys, key)
		}
	}
	switch len(keys) {
	case 0:
		return "", nil
	case 1:
		return keys[0], nil
	default:
		return "", fmt.Errorf("%w: cairn reported %d environment amendments pointing a provider home at the boot directory (%s); tachyon cannot tell which one owns home_resource_paths",
			ErrHomeResource, len(keys), strings.Join(keys, ", "))
	}
}

// ResolveHome returns the operator's own provider home for the environment
// variable key: whatever they have exported, and otherwise this machine's
// default for that provider.
//
// The environment is read BEFORE anything amends it, which is the whole
// reason this is its own function called from the launch path rather than
// something the spawn step works out afterwards. Once CODEX_HOME has been
// set to the boot directory there is no longer any way to ask what it used
// to be, and a launcher that resolved the source home after applying its own
// amendment would link every resource to itself.
//
// A key with no default and no exported value is refused, naming the key. A
// launcher that fell back to the boot directory here would produce exactly
// the self-link this function exists to make impossible.
func ResolveHome(key string) (string, error) {
	if key == "" {
		return "", nil
	}
	if exported := strings.TrimSpace(os.Getenv(key)); exported != "" {
		abs, err := filepath.Abs(exported)
		if err != nil {
			return "", fmt.Errorf("%w: %s=%q is not a usable path: %w", ErrHomeResource, key, exported, err)
		}
		return abs, nil
	}
	rel, known := homeDefaults[key]
	if !known {
		return "", fmt.Errorf("%w: cairn asks tachyon to repoint %s at the boot directory, but tachyon knows no default location for that provider's real home; export %s to the real one before launching",
			ErrHomeResource, key, key)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: resolving the home directory for %s: %w", ErrHomeResource, key, err)
	}
	return filepath.Join(home, rel), nil
}

// PrepareHomeResources provides every resource in result.HomeResourcePaths
// inside result.BootDir, as a symbolic link to the operator's own copy under
// sourceHome. It returns the destination paths, in the order Cairn named
// them, or an error that names the exact path that stopped it.
//
// It links and never copies, and that is the ownership boundary Cairn draws
// carried one layer up rather than erased. auth.json is a live credential and
// hooks.json/hooks are a live hook registration; copying them would make a
// disposable boot directory a second home for the operator's real state, one
// that goes stale the moment they re-authenticate and that a sweep could
// delete. A link exposes the same files to one boot-local home without
// duplicating a byte, and nothing here ever opens them: this function stats
// and links, so no credential can reach a log through it.
//
// # All or nothing
//
// Every source is checked before any link is made, so a boot directory is
// never left half-prepared and the error names everything missing at once
// rather than one thing per attempt. A missing resource is a refusal, not a
// skip: Cairn names these because the provider needs them, and a launch that
// quietly proceeded without hooks.json would open a session whose hooks
// simply do not run, with nothing anywhere saying so.
//
// # What it refuses to overwrite
//
// A destination that already exists is left exactly as it is. A link already
// pointing at the same source is the one accepted case — that makes the whole
// function idempotent — and anything else is refused, including a link
// pointing somewhere else and a real file Cairn rendered. Replacing a real
// file at a path the operator's provider is about to read is destructive in
// the one direction that cannot be undone, and it can only mean this package
// and Cairn disagree about what belongs in that directory.
//
// # Never at itself
//
// sourceHome resolving to result.BootDir, or to anything inside it, is
// refused before any work happens. That is the failure mode of resolving the
// source home too late — after the launch environment already carries
// CODEX_HOME=<bootdir> — and it would produce a link named auth.json pointing
// at itself.
func PrepareHomeResources(result Result, sourceHome string) ([]string, error) {
	if len(result.HomeResourcePaths) == 0 {
		return nil, nil
	}
	if sourceHome == "" {
		return nil, fmt.Errorf("%w: cairn named %d resources to provide (%s) but tachyon resolved no provider home to take them from",
			ErrHomeResource, len(result.HomeResourcePaths), strings.Join(result.HomeResourcePaths, ", "))
	}
	if result.BootDir == "" {
		return nil, fmt.Errorf("%w: cairn reported no boot directory to provide resources in", ErrHomeResource)
	}
	if err := refuseSelfHome(sourceHome, result.BootDir); err != nil {
		return nil, err
	}

	type pair struct{ src, dst string }
	planned := make([]pair, 0, len(result.HomeResourcePaths))
	var missing []string

	for _, name := range result.HomeResourcePaths {
		if !filepath.IsLocal(name) {
			return nil, fmt.Errorf("%w: cairn named %q, which does not stay inside the boot directory", ErrHomeResource, name)
		}
		src := filepath.Join(sourceHome, name)
		if _, err := os.Lstat(src); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				missing = append(missing, src)
				continue
			}
			return nil, fmt.Errorf("%w: reading %s: %w", ErrHomeResource, src, err)
		}
		planned = append(planned, pair{src: src, dst: filepath.Join(result.BootDir, filepath.FromSlash(name))})
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: the %s provider needs these, and they are not in %s: %s — create them there (or point that home elsewhere) and launch again",
			ErrHomeResource, result.Provider, sourceHome, strings.Join(missing, ", "))
	}

	prepared := make([]string, 0, len(planned))
	for _, p := range planned {
		if err := linkHomeResource(p.src, p.dst); err != nil {
			return prepared, err
		}
		prepared = append(prepared, p.dst)
	}
	return prepared, nil
}

// linkHomeResource makes dst a symbolic link to src, or confirms that it
// already is one. See [PrepareHomeResources] for what it refuses and why.
func linkHomeResource(src, dst string) error {
	info, err := os.Lstat(dst)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%w: %s already exists and is not a link; tachyon will not replace it", ErrHomeResource, dst)
		}
		existing, readErr := os.Readlink(dst)
		if readErr != nil {
			return fmt.Errorf("%w: reading the link at %s: %w", ErrHomeResource, dst, readErr)
		}
		if existing != src {
			return fmt.Errorf("%w: %s already links to %s, not %s; tachyon will not repoint it", ErrHomeResource, dst, existing, src)
		}
		return nil
	case errors.Is(err, os.ErrNotExist):
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return fmt.Errorf("%w: creating %s: %w", ErrHomeResource, filepath.Dir(dst), mkErr)
		}
		if linkErr := os.Symlink(src, dst); linkErr != nil {
			return fmt.Errorf("%w: linking %s to %s: %w", ErrHomeResource, dst, src, linkErr)
		}
		return nil
	default:
		return fmt.Errorf("%w: reading %s: %w", ErrHomeResource, dst, err)
	}
}

// refuseSelfHome refuses a source home that is the boot directory, or sits
// inside it. Symlinks are resolved as far as they resolve, so a source home
// that reaches the boot directory through a link is caught too; a path that
// cannot be resolved at all is compared as written rather than waved through.
func refuseSelfHome(sourceHome, bootDir string) error {
	src := resolvedPath(sourceHome)
	boot := resolvedPath(bootDir)
	if src == boot || strings.HasPrefix(src, boot+string(os.PathSeparator)) {
		return fmt.Errorf("%w: the provider home tachyon resolved (%s) is the boot directory itself; every resource would link to itself. This is what resolving the home after amending the environment looks like — resolve it before",
			ErrHomeResource, sourceHome)
	}
	return nil
}

// resolvedPath returns path with symlinks resolved and made absolute, or the
// cleaned path when it cannot be resolved (it may not exist yet).
func resolvedPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if evaluated, err := filepath.EvalSymlinks(abs); err == nil {
		return evaluated
	}
	return abs
}
