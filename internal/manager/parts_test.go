package manager_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/manager"
)

func TestNewPartFlowsThroughTreeOpenAndSaveAsAProfile(t *testing.T) {
	root := newFixture(t)
	svc := newService(t, root)

	created, err := svc.NewPart("observability")
	if err != nil {
		t.Fatalf("NewPart: %v", err)
	}
	if created.Kind != bundle.KindProfile || created.ID != "observability" {
		t.Fatalf("NewPart content = %+v; want KindProfile with bare id", created)
	}
	if got, want := created.RelPath, "profiles/parts/observability.md"; got != want {
		t.Fatalf("NewPart RelPath = %q; want %q", got, want)
	}

	opened, err := svc.Open(string(bundle.KindProfile), "observability")
	if err != nil {
		t.Fatalf("Open nested profile: %v", err)
	}
	if opened.RelPath != created.RelPath || !bytes.Equal(opened.Bytes, created.Bytes) {
		t.Fatalf("Open = %+v; want same nested path and bytes as NewPart", opened)
	}
	saved, err := svc.Save(string(bundle.KindProfile), "observability", opened.Bytes)
	if err != nil {
		t.Fatalf("Save nested profile: %v", err)
	}
	if saved.RelPath != created.RelPath || !bytes.Equal(saved.Bytes, created.Bytes) {
		t.Fatalf("unedited Save = %+v; want byte-stable nested profile", saved)
	}
	onDisk, err := os.ReadFile(created.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(onDisk, created.Bytes) {
		t.Fatalf("unedited open/save changed bytes\nbefore: %q\nafter:  %q", created.Bytes, onDisk)
	}

	tree, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	found := false
	for _, group := range tree.Groups {
		if group.Kind != bundle.KindProfile {
			continue
		}
		for _, node := range group.Nodes {
			if node.ID == "observability" {
				found = true
				if node.Kind != bundle.KindProfile || node.RelPath != created.RelPath {
					t.Errorf("part tree node = %+v; want ordinary profile at actual nested RelPath", node)
				}
			}
		}
	}
	if !found {
		t.Fatal("new part did not appear in the Profiles group")
	}
}

func TestManagerTreeObservesExternalPartAdditionAndRemoval(t *testing.T) {
	root := newFixture(t)
	svc := newService(t, root)
	part := filepath.Join(root, "profiles", "parts", "fresh.md")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(part, []byte("---\nid: fresh\nextends: base\nspec: {}\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !treeHasProfile(t, svc, "fresh") {
		t.Fatal("Tree did not observe nested addition")
	}
	if err := os.Remove(part); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if treeHasProfile(t, svc, "fresh") {
		t.Fatal("Tree did not observe nested removal")
	}
}

func treeHasProfile(t *testing.T, svc *manager.Service, id string) bool {
	t.Helper()
	tree, err := svc.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	for _, group := range tree.Groups {
		if group.Kind != bundle.KindProfile {
			continue
		}
		for _, node := range group.Nodes {
			if node.ID == id {
				return true
			}
		}
	}
	return false
}

func TestPartCreationAndAutocompleteFrontendContract(t *testing.T) {
	newArtifact := readManagerFrontend(t, "NewArtifact.jsx")
	for _, want := range []string{
		`["profile", "part", "role-prose"`,
		`k === "part" ? "profile" : k`,
		`kind === "part"`,
		`ManagerAPI.NewPart(id.trim())`,
	} {
		if !strings.Contains(newArtifact, want) {
			t.Errorf("NewArtifact.jsx is missing part-creation contract %q", want)
		}
	}

	bridge := readManagerFrontend(t, "bridge.js")
	if !strings.Contains(bridge, `NewPart: (id) => callService(MANAGER_SERVICE, "NewPart", id)`) {
		t.Error("bridge.js does not expose Manager.NewPart")
	}

	palette := readManagerFrontend(t, "Palette.jsx")
	for _, want := range []string{`const profileSuggestions = catalog.profile ?? []`, `list="compose-part-suggestions"`} {
		if !strings.Contains(palette, want) {
			t.Errorf("Palette.jsx is missing unified profile/part autocomplete contract %q", want)
		}
	}
	composer := readManagerFrontend(t, "BindingComposer.jsx")
	for _, want := range []string{`const profiles = groups.profile ?? []`, `suggestions={profiles}`} {
		if !strings.Contains(composer, want) {
			t.Errorf("BindingComposer.jsx is missing unified profile/part autocomplete contract %q", want)
		}
	}
}

func readManagerFrontend(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "frontend", "src", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}
