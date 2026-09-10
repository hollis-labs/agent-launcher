package manager_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/manager"
)

// newFixture builds a small bundle covering all seven artifact kinds,
// including the named conflation trap: profiles/architect.md and
// templates/roles/architect.md share a basename.
func newFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	write("profiles/architect.md", "---\nid: architect\nname: Architect\nextends: base\n---\n\nProfile body.\n")
	write("profiles/base.md", "---\n# A comment interleaved with real keys, as in the live base.md.\nid: base\nname: Base\nspec:\n  # nested, must not be read as a header key\n  name: not-this\n---\n\nAbstract floor body.\n")
	write("templates/lenses/architect.md", "Prose for the architect lens. Not a profile.\n")
	write("templates/standing.md", "Standing prose a profile pulls in whole.\n")
	write("prompts/report.md", "Report the outcome.\n")
	write("skills/commit/SKILL.md", "---\nid: commit\nname: Commit\n---\n\nHow to commit.\n")
	write("skills/no-skill-file/NOTES.md", "not a skill file\n")
	write("hooks/session-start.sh", "#!/bin/sh\necho hi\n")
	return root
}

func newService(t *testing.T, root string) *manager.Service {
	t.Helper()
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save(root); err != nil {
		t.Fatalf("RootStore.Save: %v", err)
	}
	return manager.New(store)
}

func TestTreeGroupsAllFiveKindsInOrder(t *testing.T) {
	svc := newService(t, newFixture(t))
	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	wantKinds := bundle.Kinds()
	if len(tr.Groups) != len(wantKinds) {
		t.Fatalf("Tree returned %d groups; want %d (one per bundle.Kinds())", len(tr.Groups), len(wantKinds))
	}
	for i, g := range tr.Groups {
		if g.Kind != wantKinds[i] {
			t.Errorf("group %d kind = %q; want %q (bundle.Kinds() order)", i, g.Kind, wantKinds[i])
		}
		if g.Label == "" {
			t.Errorf("group %q has no label", g.Kind)
		}
		if g.Count != len(g.Nodes) {
			t.Errorf("group %q Count = %d; want len(Nodes) = %d", g.Kind, g.Count, len(g.Nodes))
		}
	}
}

// TestTreeDistinguishesProfileFromTemplate is the class-audit case: the tree
// must not conflate profiles/architect.md and templates/lenses/architect.md,
// and every node carries enough to tell them apart without relying on which
// group the viewer remembers clicking into.
//
// The pair changed when templates/roles/ retired (it used to be profile
// versus role prose) and the hazard did not: templates/ holds lenses/ and
// projects/ now, and a template's id carries its path precisely so a nested
// one stays distinguishable.
func TestTreeDistinguishesProfileFromTemplate(t *testing.T) {
	svc := newService(t, newFixture(t))
	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	var profileNode, templateNode *manager.Node
	for gi := range tr.Groups {
		g := &tr.Groups[gi]
		for ni := range g.Nodes {
			n := &g.Nodes[ni]
			switch {
			case n.Kind == bundle.KindProfile && n.ID == "architect":
				profileNode = n
			case n.Kind == bundle.KindTemplate && n.ID == "lenses/architect":
				templateNode = n
			}
		}
	}
	if profileNode == nil || templateNode == nil {
		t.Fatalf("expected an architect profile node and a lenses/architect template node; got profile=%v template=%v", profileNode, templateNode)
	}
	if profileNode.Kind == templateNode.Kind {
		t.Fatalf("profile and template nodes for %q have the same Kind %q", "architect", profileNode.Kind)
	}
	if profileNode.RelPath == templateNode.RelPath {
		t.Fatalf("profile and template nodes for %q have the same RelPath %q", "architect", profileNode.RelPath)
	}
	if profileNode.RelPath != "profiles/architect.md" {
		t.Errorf("profile RelPath = %q; want profiles/architect.md", profileNode.RelPath)
	}
	if templateNode.RelPath != "templates/lenses/architect.md" {
		t.Errorf("template RelPath = %q; want templates/lenses/architect.md", templateNode.RelPath)
	}
	// The profile alone carries frontmatter; a template is plain markdown
	// with no header concept, so its Header must be nil rather than a
	// misleadingly-present zero value.
	if profileNode.Header == nil || !profileNode.Header.Present {
		t.Errorf("profile Header = %+v; want Present", profileNode.Header)
	}
	if templateNode.Header != nil {
		t.Errorf("template Header = %+v; want nil", templateNode.Header)
	}
}

// TestOpenDistinguishesProfileFromTemplate pins the same guarantee at the
// Open boundary: Open(kind, id) with a colliding basename and a different
// Kind opens two different files with two different contents.
func TestOpenDistinguishesProfileFromTemplate(t *testing.T) {
	svc := newService(t, newFixture(t))

	profile, err := svc.Open(string(bundle.KindProfile), "architect")
	if err != nil {
		t.Fatalf("Open(profile, architect): %v", err)
	}
	template, err := svc.Open(string(bundle.KindTemplate), "lenses/architect")
	if err != nil {
		t.Fatalf("Open(template, lenses/architect): %v", err)
	}

	if profile.RelPath == template.RelPath {
		t.Fatalf("both opens resolved to the same RelPath %q", profile.RelPath)
	}
	if bytes.Equal(profile.Bytes, template.Bytes) {
		t.Fatalf("profile and template content is identical; fixture is broken")
	}
	if profile.Kind != bundle.KindProfile {
		t.Errorf("profile.Kind = %q; want %q", profile.Kind, bundle.KindProfile)
	}
	if template.Kind != bundle.KindTemplate {
		t.Errorf("template.Kind = %q; want %q", template.Kind, bundle.KindTemplate)
	}
}

// TestOpenSaveRoundTripThroughJSON proves the exact transport Wails uses:
// []byte content survives a real encoding/json Marshal/Unmarshal cycle,
// including every awkward byte bytes_test.go names for the read path (CRLF,
// a BOM, a NUL, non-ASCII, no trailing newline).
func TestOpenSaveRoundTripThroughJSON(t *testing.T) {
	const awkward = "\xEF\xBB\xBF---\r\n" +
		"id: awkward\r\n" +
		"name: Awkward   \t\r\n" +
		"---\r\n" +
		"\r\n" +
		"A line with a tab\there and trailing spaces here.   \n" +
		"\n\n\n" +
		"A lone carriage return follows.\rStill the same line.\n" +
		"Non-ASCII: éü—✓ and a NUL: \x00 after it.\n" +
		"No trailing newline on the last line."

	root := newFixture(t)
	path := filepath.Join(root, "profiles", "awkward.md")
	if err := os.WriteFile(path, []byte(awkward), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc := newService(t, root)

	opened, err := svc.Open(string(bundle.KindProfile), "awkward")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Simulate the wire: marshal to JSON exactly as Wails' messageprocessor
	// does for a call result, then unmarshal exactly as its frontend-args
	// decoder does — proving the []byte field's automatic base64 encoding
	// preserves every byte, not just the ones already read back in-process.
	wire, err := json.Marshal(opened)
	if err != nil {
		t.Fatalf("json.Marshal(opened): %v", err)
	}
	var overWire manager.Content
	if err := json.Unmarshal(wire, &overWire); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !bytes.Equal(overWire.Bytes, []byte(awkward)) {
		t.Fatalf("content changed crossing JSON:\n got: %q\nwant: %q", overWire.Bytes, awkward)
	}

	// Save without editing: the bytes that came back over the "wire" go
	// straight back in, exactly as the frontend's save button would send
	// them.
	saved, err := svc.Save(string(bundle.KindProfile), "awkward", overWire.Bytes)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !bytes.Equal(saved.Bytes, []byte(awkward)) {
		t.Fatalf("Save result content = %q; want %q", saved.Bytes, awkward)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(onDisk, []byte(awkward)) {
		t.Fatalf("file on disk after an unedited open+save = %q; want %q (unchanged)", onDisk, awkward)
	}
}

// TestPromptOpenSaveRoundTripPreservesMarkersAndAwkwardBytes is
// CW-20260904-0006's own explicit proof that T05's byte-preservation
// guarantee (D5 -- see TestOpenSaveRoundTripThroughJSON just above, which
// this mirrors) covers bundle.KindPrompt too, now that it is registered,
// rather than assuming it. The fixture is not synthetic hazard bytes alone:
// it reproduces the exact shape ~/dev/projects/agent-setup/prompts/
// report.md carries as of 2026-09-03 -- a <!-- cairn:value ... --> marker
// sharing its line with other content, the same "- key: <!-- cairn:value
// key -->" list-item form that file ends on -- with the CRLF/BOM/tab/
// trailing-space/lone-CR/NUL/non-ASCII/no-trailing-newline hazards
// bytes_test.go's awkwardBytes already names, layered on top so a single
// test proves both "grounded in a real prompt's shape" and "survives the
// worst bytes D5 has to survive" at once.
//
// D8 also applies here, implicitly: this test never inspects what the
// marker names or asserts anything about it resolving -- Open/Save simply
// do not look.
func TestPromptOpenSaveRoundTripPreservesMarkersAndAwkwardBytes(t *testing.T) {
	const promptContent = "\xEF\xBB\xBF# Report\r\n" +
		"\r\n" +
		"Write, in this order:\t\r\n" +
		"\n\n\n" +
		"A line with a lone carriage return.\rStill the same line.\n" +
		"Non-ASCII: éü—✓ and a NUL: \x00 after it.\n" +
		"\n" +
		"- binding: <!-- cairn:value binding -->   \n" +
		"- scope: <!-- cairn:value scope -->\n" +
		"- session: <!-- cairn:value session -->" // no trailing newline

	root := newFixture(t)
	path := filepath.Join(root, "prompts", "awkward-report.md")
	if err := os.WriteFile(path, []byte(promptContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc := newService(t, root)

	opened, err := svc.Open(string(bundle.KindPrompt), "awkward-report")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened.Bytes, []byte(promptContent)) {
		t.Fatalf("Open returned %q; want the exact fixture bytes %q", opened.Bytes, promptContent)
	}

	// The same JSON round trip a Wails call result and argument actually go
	// through (see TestOpenSaveRoundTripThroughJSON above).
	wire, err := json.Marshal(opened)
	if err != nil {
		t.Fatalf("json.Marshal(opened): %v", err)
	}
	var overWire manager.Content
	if err := json.Unmarshal(wire, &overWire); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !bytes.Equal(overWire.Bytes, []byte(promptContent)) {
		t.Fatalf("content changed crossing JSON:\n got: %q\nwant: %q", overWire.Bytes, promptContent)
	}

	// Save without editing -- the acceptance criterion's "open a real
	// prompt, save it unedited" step.
	saved, err := svc.Save(string(bundle.KindPrompt), "awkward-report", overWire.Bytes)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !bytes.Equal(saved.Bytes, []byte(promptContent)) {
		t.Fatalf("Save result content = %q; want %q", saved.Bytes, promptContent)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(onDisk, []byte(promptContent)) {
		t.Fatalf("file on disk after an unedited open+save = %q; want %q (unchanged -- this is the 'git diff is empty' criterion)", onDisk, promptContent)
	}
}

// TestSaveArgumentDecodingFromJSON proves the argument side of the wire, the
// direction Wails' bindings.go actually uses (json.Unmarshal of one
// json.RawMessage per argument into the Go parameter type): a base64 JSON
// string, decoded into a []byte parameter, arrives at Save unchanged.
func TestSaveArgumentDecodingFromJSON(t *testing.T) {
	const raw = "line one\r\nline two, no trailing newline\x00tail"
	svc := newService(t, newFixture(t))

	argJSON, err := json.Marshal([]byte(raw))
	if err != nil {
		t.Fatalf("json.Marshal([]byte): %v", err)
	}
	var decoded []byte
	if err := json.Unmarshal(argJSON, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, err := svc.Save(string(bundle.KindProfile), "architect", decoded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := svc.Open(string(bundle.KindProfile), "architect")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got.Bytes, []byte(raw)) {
		t.Fatalf("content after Save via the JSON-argument path = %q; want %q", got.Bytes, raw)
	}
}

// TestTreeReflectsAFileCreatedOutsideTachyon is the acceptance criterion: the
// tree is read fresh every call, so a file that appears on disk between two
// Tree() calls — exactly what happens when the user edits the bundle with
// git or another editor — shows up without restarting anything.
func TestTreeReflectsAFileCreatedOutsideTachyon(t *testing.T) {
	root := newFixture(t)
	svc := newService(t, root)

	before, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree (before): %v", err)
	}
	beforeCount := countKind(before, bundle.KindProfile)

	// A file appearing "outside Tachyon": written directly with os.WriteFile,
	// not through Save.
	newProfile := filepath.Join(root, "profiles", "engineer.md")
	if err := os.WriteFile(newProfile, []byte("---\nid: engineer\nname: Engineer\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	after, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree (after): %v", err)
	}
	afterCount := countKind(after, bundle.KindProfile)
	if afterCount != beforeCount+1 {
		t.Fatalf("profile count after external creation = %d; want %d", afterCount, beforeCount+1)
	}
	found := false
	for _, g := range after.Groups {
		if g.Kind != bundle.KindProfile {
			continue
		}
		for _, n := range g.Nodes {
			if n.ID == "engineer" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("externally created profiles/engineer.md did not appear in Tree()")
	}
}

func countKind(t manager.Tree, k bundle.Kind) int {
	for _, g := range t.Groups {
		if g.Kind == k {
			return g.Count
		}
	}
	return 0
}

func TestRootSurfacesTheActiveBundleRoot(t *testing.T) {
	root := newFixture(t)
	svc := newService(t, root)

	got, err := svc.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	// RootStore.Save/Resolve expand to an absolute path; root from t.TempDir()
	// is already absolute, so this should match exactly.
	if got != root {
		t.Fatalf("Root() = %q; want %q", got, root)
	}

	tr, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tr.Root != root {
		t.Fatalf("Tree().Root = %q; want %q", tr.Root, root)
	}
}

func TestSaveRejectsAnUnknownArtifact(t *testing.T) {
	svc := newService(t, newFixture(t))
	if _, err := svc.Save(string(bundle.KindProfile), "does-not-exist", []byte("x")); err == nil {
		t.Fatal("Save of an unknown artifact succeeded; want an error")
	}
}

func TestOpenRejectsAnUnknownKind(t *testing.T) {
	svc := newService(t, newFixture(t))
	if _, err := svc.Open("not-a-real-kind", "architect"); err == nil {
		t.Fatal("Open with an unknown kind succeeded; want an error")
	}
}
