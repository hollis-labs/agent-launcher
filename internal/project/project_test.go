package project_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/project"
	"github.com/hollis-labs/tachyon/internal/state"
)

func TestDefaultStoreLivesUnderConfigDir(t *testing.T) {
	t.Setenv(state.DirEnv, t.TempDir())
	root, _ := state.ConfigDir()
	store, err := project.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Path != filepath.Join(root, "projects.json") {
		t.Fatalf("path = %q", store.Path)
	}
}

func TestCRUDPersistsAndToleratesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "projects.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"version":2,"projects":[{"name":"Tachyon","path":"/old","torque_id":"PRJ-27","docs":["README.md"]}]}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	store := project.Store{Path: path}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Tachyon" {
		t.Fatalf("List = %+v", got)
	}
	if err := store.Update("Tachyon", project.Project{Name: "tachyon", Path: "/new"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(project.Project{Name: "Cairn", Path: "/cairn"}); err != nil {
		t.Fatal(err)
	}
	// A newly constructed store is the restart boundary: there is no cache or
	// process-only state involved in recovering these records.
	reloaded, err := (project.Store{Path: path}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded) != 2 {
		t.Fatalf("len = %d", len(reloaded))
	}
	data, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"torque_id": "PRJ-27"`) || !strings.Contains(string(data), `"docs"`) {
		t.Fatalf("unknown fields lost: %s", data)
	}
	if !strings.Contains(string(data), `"version": 2`) {
		t.Fatalf("unknown document field lost: %s", data)
	}
	if err := store.Delete("Cairn"); err != nil {
		t.Fatal(err)
	}
	final, _ := store.List()
	if len(final) != 1 || final[0].Name != "tachyon" {
		t.Fatalf("final = %+v", final)
	}
}

// TestProjectsNeverReachTheBundle keeps the property the binding-shaped
// version of this test was really about: a project is Tachyon's own record,
// and nothing about it is ever written into the bundle.
//
// What it no longer checks is the association between a project and the
// bindings whose scope matched its path — bindings are gone, and a launch
// profile deliberately carries no scope to match against. See project.View.
func TestProjectsNeverReachTheBundle(t *testing.T) {
	stateDir, bundleA, bundleB := t.TempDir(), t.TempDir(), t.TempDir()
	for _, root := range []string{bundleA, bundleB} {
		if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "profiles", "engineer.md"), []byte("---\nid: engineer\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	projectStore := project.Store{Path: filepath.Join(stateDir, "projects.json")}
	svc := project.NewService(projectStore)
	const projectName = "NAME-MUST-NEVER-REACH-BUNDLE"
	if _, err := svc.Create(project.Project{Name: projectName, Path: "/work/a"}); err != nil {
		t.Fatal(err)
	}
	views, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Project.Name != projectName {
		t.Fatalf("views = %+v", views)
	}
	if _, err := svc.Update(projectName, project.Project{Name: projectName, Path: "/work/new"}); err != nil {
		t.Fatal(err)
	}
	views, err = svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Project.Path != "/work/new" {
		t.Fatalf("after update = %+v", views)
	}

	for _, root := range []string{bundleA, bundleB} {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(path, projectName) || strings.Contains(string(data), projectName) {
				t.Fatalf("project name reached bundle write path %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectCRUDLeavesAgentSetupUntouched(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentSetup := filepath.Join(home, "dev", "projects", "agent-setup")
	if err := os.MkdirAll(agentSetup, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(agentSetup, "sentinel")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := project.Store{Path: filepath.Join(t.TempDir(), "Tachyon", "projects.json")}
	if err := store.Create(project.Project{Name: "Safe", Path: "/tmp/safe"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Update("Safe", project.Project{Name: "Safer", Path: "/tmp/safer"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("Safer"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("agent-setup changed: %q, %v", data, err)
	}
}
