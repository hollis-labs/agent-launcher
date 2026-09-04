package apply

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kinds are the three top-level bundle directories `make install-system`
// mirrors into AGENTS_HOME, in the Makefile's own order — see agent-setup's
// Makefile, the install-system target. Nothing outside these three is
// Apply's concern: profiles, bindings, scopes.yaml and hooks are read from
// the bundle directly and need no staging (CW-20260904-0023's own table).
var Kinds = []string{"templates", "skills", "prompts"}

// dsStore is the one file `make install-system`'s own rsync invocations
// exclude everywhere in the tree (--exclude='.DS_Store', reinforced by
// --delete-excluded so a stray one at the destination is cleaned up too).
// It is skipped on both sides of every comparison below, at any depth, so
// its mere presence or absence never counts as a difference — matching
// what rsync itself would actually transfer or delete.
const dsStore = ".DS_Store"

// KindDiff is one kind's (templates, skills or prompts) content-based
// comparison between the bundle and the installed layer, expressed as
// exactly what `make install-system`'s rsync would do to reconcile them:
//
//   - Added: a bundle-relative path present in the bundle, absent from the
//     installed layer — rsync would copy it there.
//   - Removed: a path present in the installed layer, absent from the
//     bundle — rsync's own --delete would remove it. This is the set the
//     Apply confirmation dialog's deletion disclosure is built from.
//   - Changed: a path present on both sides whose content differs — a
//     same-named file with different bytes counts here, not as "no
//     difference" the way a filename-only comparison would wrongly read
//     it.
//
// All three are relative to the kind's own directory (e.g. "roles/foo.md"
// under "templates"), slash-separated, sorted.
type KindDiff struct {
	Kind    string   `json:"kind"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
}

// Count is how many paths this kind's diff touches in total — Added plus
// Removed plus Changed.
func (d KindDiff) Count() int { return len(d.Added) + len(d.Removed) + len(d.Changed) }

// Summary is one full [Compare] result across all three [Kinds], plus the
// two paths it was computed for — carried along so a caller (the
// confirmation dialog, in particular) never has to ask a second service
// for "which bundle, staged where" after already having the diff that
// answers it.
type Summary struct {
	BundleRoot string `json:"bundleRoot"`
	AgentsHome string `json:"agentsHome"`
	// Kinds is always exactly len([Kinds]) entries, in [Kinds]' own order,
	// even when a kind's own diff is empty — the same "always all groups"
	// discipline internal/manager.Tree already follows for its seven kinds,
	// for the identical reason: a UI iterating this should never have to
	// guess whether a missing entry means "no differences" or "not
	// computed".
	Kinds []KindDiff `json:"kinds"`
	// Differs is true when Apply has anything to do — the enablement gate
	// itself. Computed here, not left for a caller to re-derive from Kinds,
	// so there is exactly one place "does this count as a difference"
	// means.
	Differs bool `json:"differs"`
	// Description is a short human summary of what differs, e.g. "3
	// prompts, 1 template" — only the kinds with a nonzero count, in
	// [Kinds]' own order. Empty when Differs is false.
	Description string `json:"description"`
}

// Compare reads bundleRoot/{templates,skills,prompts} and
// agentsHome/{templates,skills,prompts} and reports how they differ, by
// content — never by filename alone, and never by a dirty flag left over
// from an edit that was since undone. It is read-only: nothing under
// either root is written, touched or created.
//
// A kind's directory missing on either side is not an error — it reads as
// zero files on that side, which for a bundle-side kind with content
// naturally surfaces as every one of its files under Added (a genuine
// difference: nothing is staged there yet), and for an installed-layer
// side with content as every one of its files under Removed. Only a path
// that exists but is not a directory is a real error.
func Compare(bundleRoot, agentsHome string) (Summary, error) {
	kinds := make([]KindDiff, 0, len(Kinds))
	for _, kind := range Kinds {
		kd, err := compareKind(kind, filepath.Join(bundleRoot, kind), filepath.Join(agentsHome, kind))
		if err != nil {
			return Summary{}, err
		}
		kinds = append(kinds, kd)
	}
	differs, description := summarize(kinds)
	return Summary{
		BundleRoot:  bundleRoot,
		AgentsHome:  agentsHome,
		Kinds:       kinds,
		Differs:     differs,
		Description: description,
	}, nil
}

func summarize(kinds []KindDiff) (differs bool, description string) {
	var parts []string
	for _, k := range kinds {
		n := k.Count()
		if n == 0 {
			continue
		}
		differs = true
		label := strings.TrimSuffix(k.Kind, "s")
		if n != 1 {
			label += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, label))
	}
	return differs, strings.Join(parts, ", ")
}

func compareKind(kind, bundleDir, agentsDir string) (KindDiff, error) {
	bundleFiles, err := listFiles(bundleDir)
	if err != nil {
		return KindDiff{}, err
	}
	agentsFiles, err := listFiles(agentsDir)
	if err != nil {
		return KindDiff{}, err
	}

	kd := KindDiff{Kind: kind}
	for rel := range bundleFiles {
		if _, ok := agentsFiles[rel]; !ok {
			kd.Added = append(kd.Added, rel)
		}
	}
	for rel := range agentsFiles {
		if _, ok := bundleFiles[rel]; !ok {
			kd.Removed = append(kd.Removed, rel)
		}
	}
	for rel := range bundleFiles {
		if _, ok := agentsFiles[rel]; !ok {
			continue
		}
		same, err := sameContent(
			filepath.Join(bundleDir, filepath.FromSlash(rel)),
			filepath.Join(agentsDir, filepath.FromSlash(rel)),
		)
		if err != nil {
			return KindDiff{}, err
		}
		if !same {
			kd.Changed = append(kd.Changed, rel)
		}
	}

	sort.Strings(kd.Added)
	sort.Strings(kd.Removed)
	sort.Strings(kd.Changed)
	return kd, nil
}

// listFiles returns the set of regular-file paths under root, relative to
// root and slash-separated, skipping .DS_Store at any depth. A root that
// does not exist returns an empty, non-nil set and no error — see
// [Compare]'s own doc for why that is "no files here yet", not a fault.
//
// If root itself is a symlink to a directory (bundles get symlinked
// around — see internal/bundle's own note on this for skills/<name>), it
// is resolved once, at the top, so its contents are still walked; a
// symlink found deeper in the tree is left exactly as rsync -a itself
// would treat it (copied as a link, not followed).
func listFiles(root string) (map[string]struct{}, error) {
	out := map[string]struct{}{}

	info, statErr := os.Stat(root)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return out, nil
		}
		return nil, fmt.Errorf("apply: statting %s: %w", root, statErr)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("apply: %s exists but is not a directory", root)
	}

	walkRoot := root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		walkRoot = resolved
	}

	err := filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == dsStore {
			return nil
		}
		rel, relErr := filepath.Rel(walkRoot, p)
		if relErr != nil {
			return relErr
		}
		out[filepath.ToSlash(rel)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("apply: walking %s: %w", root, err)
	}
	return out, nil
}

// sameContent reports whether a and b hold identical bytes — a real
// content comparison, not a size or modtime heuristic, so a same-named
// file with different content is never mistaken for "no difference".
func sameContent(a, b string) (bool, error) {
	ab, err := os.ReadFile(a)
	if err != nil {
		return false, fmt.Errorf("apply: reading %s: %w", a, err)
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false, fmt.Errorf("apply: reading %s: %w", b, err)
	}
	return bytes.Equal(ab, bb), nil
}
