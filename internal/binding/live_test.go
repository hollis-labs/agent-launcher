package binding_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/bundle"
)

// TestLiveBundleBindingsCensus is the acceptance test for CW-20260904-0002's
// (T23) read side: enumerate ~/dev/projects/agent-setup/bindings/ for real,
// through the same Store path the palette calls, and confirm every one of
// the eight live bindings surfaces with a resolved path — never a
// scopes.yaml alias key — as its scope.
//
// Skipped unless TACHYON_LIVE_BUNDLE is set, matching internal/bundle and
// internal/manager's identical gate: the suite must not depend on, or ever
// touch by default, a git repo Chrispian edits for real. This test is
// read-only; it fingerprints the whole bindings/ directory (and
// scopes.yaml) before and after and fails if a single byte moved.
//
//	TACHYON_LIVE_BUNDLE=1 go test ./internal/binding/ -run TestLiveBundleBindingsCensus -v
func TestLiveBundleBindingsCensus(t *testing.T) {
	if os.Getenv("TACHYON_LIVE_BUNDLE") == "" {
		t.Skip("set TACHYON_LIVE_BUNDLE=1 to read the real bundle; the suite reads testdata/ by default")
	}
	root := liveRoot(t)
	dir := filepath.Join(root, "bindings")
	scopesPath := filepath.Join(root, "scopes.yaml")
	t.Logf("bindings dir: %s", dir)

	before := hashPaths(t, dir, scopesPath)

	s := binding.NewFileStore(dir)
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	const wantCount = 8
	if len(got) != wantCount {
		t.Errorf("List returned %d bindings; want %d: %+v"+
			"; if the live bundle has genuinely changed, that is not a defect in this package — update this test",
			len(got), wantCount, got)
	}

	aliasKeys, err := liveAliasKeys(scopesPath)
	if err != nil {
		t.Fatalf("reading alias keys for the leak check: %v", err)
	}
	for _, b := range got {
		t.Logf("%-12s profile=%-12s scope=%s", b.Name, b.Profile, b.Scope)
		if b.Name == "" || b.Profile == "" || b.Scope == "" {
			t.Errorf("binding %+v has an empty field", b)
		}
		if aliasKeys[b.Scope] {
			t.Errorf("binding %q surfaced a raw scopes.yaml alias key as its scope: %q", b.Name, b.Scope)
		}
	}

	if after := hashPaths(t, dir, scopesPath); after != before {
		t.Fatalf("the live bindings/ directory changed across a read-only List():\nbefore %s\nafter  %s", before, after)
	}
	t.Logf("bindings/ fingerprint unchanged: %s", before)
}

// TestLiveBundleCreateDeleteRoundTrip is the acceptance test for
// CW-20260904-0002's write side, run directly against the live bundle:
// create one throwaway binding file through this package's own Create,
// verify it is visible through List/Get, delete it through this package's
// own Delete, and confirm the directory returns to its exact original
// contents — proven by a SHA-256 fingerprint taken immediately before
// Create and compared after Delete, the same discipline
// TestCreateThenDeleteLeavesNoTrace establishes against the fixture in
// filestore_test.go.
//
// Skipped unless TACHYON_LIVE_BUNDLE is set. If this test ever fails after
// the Create half has already run, DO NOT leave it: cd into the bundle
// root, run `git status --porcelain` and `git diff`, and remove the
// leftover bindings/tachyon-t23-roundtrip-probe.yaml file by hand — then
// treat the failure as a real bug in this package, not something to route
// around.
//
//	TACHYON_LIVE_BUNDLE=1 go test ./internal/binding/ -run TestLiveBundleCreateDeleteRoundTrip -v
func TestLiveBundleCreateDeleteRoundTrip(t *testing.T) {
	if os.Getenv("TACHYON_LIVE_BUNDLE") == "" {
		t.Skip("set TACHYON_LIVE_BUNDLE=1 to round-trip the real bundle; the suite reads testdata/ by default")
	}
	root := liveRoot(t)
	dir := filepath.Join(root, "bindings")
	scopesPath := filepath.Join(root, "scopes.yaml")
	t.Logf("bindings dir: %s", dir)

	before := hashPaths(t, dir, scopesPath)

	s := binding.NewFileStore(dir)
	probe := binding.Binding{
		Name:    "tachyon-t23-roundtrip-probe",
		Profile: "engineer",
		Scope:   "~/dev/projects/tachyon-t23-roundtrip-probe-does-not-exist",
	}

	if err := s.Create(probe); err != nil {
		t.Fatalf("Create(%s): %v", probe.Name, err)
	}
	t.Logf("created %s — if this test fails from here on, clean it up by hand: rm %s",
		probe.Name, filepath.Join(dir, probe.Name+".yaml"))

	got, err := s.Get(probe.Name)
	if err != nil {
		t.Fatalf("Get(%s) after Create: %v", probe.Name, err)
	}
	if got != probe {
		t.Fatalf("Get(%s) = %+v; want %+v", probe.Name, got, probe)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List after Create: %v", err)
	}
	found := false
	for _, b := range all {
		if b.Name == probe.Name {
			found = true
		}
	}
	if !found {
		t.Fatalf("List after Create does not include %s", probe.Name)
	}

	if err := s.Delete(probe.Name); err != nil {
		t.Fatalf("Delete(%s): %v — the probe file is still on disk, clean it up by hand", probe.Name, err)
	}

	after := hashPaths(t, dir, scopesPath)
	if after != before {
		t.Fatalf("bindings/ did not return to its original contents after create+delete:\n"+
			"before %s\nafter  %s\nCheck git status/git diff in %s immediately.", before, after, root)
	}
	t.Logf("bindings/ fingerprint restored: %s", before)
}

func liveRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("TACHYON_BUNDLE_ROOT")
	if root != "" {
		abs, err := bundle.ExpandRoot(root)
		if err != nil {
			t.Fatalf("ExpandRoot(%s): %v", root, err)
		}
		return abs
	}
	abs, err := bundle.DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot: %v", err)
	}
	return abs
}

// hashPaths fingerprints scopesPath plus every file directly under dir, so
// a live test can prove nothing moved across a read-only call or a
// create+delete round trip.
func hashPaths(t *testing.T, dir, scopesPath string) string {
	t.Helper()
	type entry struct {
		name string
		sum  [32]byte
	}
	var entries []entry
	add := func(name, path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			t.Fatalf("reading %s: %v", path, err)
		}
		entries = append(entries, entry{name: name, sum: sha256.Sum256(data)})
	}
	add("scopes.yaml", scopesPath)
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		add(e.Name(), filepath.Join(dir, e.Name()))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.name))
		h.Write([]byte{0})
		h.Write(e.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// liveAliasKeys re-reads scopes.yaml's own keys directly (not through
// binding.Store, which never exposes them) so the census test can assert
// that no returned Scope equals one. It reuses nothing internal to the
// package on purpose: an independent read is a stronger check for this one
// property than trusting the same code path being tested.
func liveAliasKeys(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, raw := range splitOnNewline(string(data)) {
		if raw == "" || raw[0] == '#' || raw[0] == ' ' || raw[0] == '\t' {
			continue
		}
		for i := 0; i < len(raw); i++ {
			if raw[i] == ':' {
				keys[raw[:i]] = true
				break
			}
		}
	}
	return keys, nil
}

func splitOnNewline(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, trimCR(s[start:i]))
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, trimCR(s[start:]))
	}
	return out
}

func trimCR(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\r' {
		return s[:len(s)-1]
	}
	return s
}
