package bindingcomposer

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/launch"
)

type fakeLauncher struct {
	calls  int
	inputs []launch.CompositionInput
	err    error
}

func (f *fakeLauncher) LaunchComposition(input launch.CompositionInput) error {
	f.calls++
	f.inputs = append(f.inputs, input)
	return f.err
}

func testService(t *testing.T, root string, l launcher) (*Service, bundle.RootStore) {
	t.Helper()
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	if err := store.Save(root); err != nil {
		t.Fatal(err)
	}
	return NewService(store, l), store
}

func TestSaveMatchesCairnBindingFormatByteForByte(t *testing.T) {
	root := t.TempDir()
	svc, _ := testService(t, root, &fakeLauncher{})
	result, err := svc.Save(Input{
		Name: "writer-stack", Profile: "writer", Parts: []string{"docs-only", "nanite-conventions"},
		Skills: []string{"qhealth", "adr"}, Prompts: []string{"handoff", "reset-scope"}, Scope: "~",
		Sets: []launch.SetInput{{Slot: "model", Value: "opus"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := "profile: writer\nparts:\n  - docs-only\n  - nanite-conventions\nskills:\n  - qhealth\n  - adr\nprompts:\n  - handoff\n  - reset-scope\nscope: \"~\"\n"
	if string(got) != want {
		t.Fatalf("saved bytes = %q\nwant = %q", got, want)
	}
	if strings.Contains(string(got), "model") || strings.Contains(string(got), "opus") {
		t.Fatal("launch-only sets leaked into binding")
	}
}

func TestSaveOmitsEmptyListsAndAppearsImmediatelyInBindingList(t *testing.T) {
	root := t.TempDir()
	svc, store := testService(t, root, &fakeLauncher{})
	if _, err := svc.Save(Input{Name: "plain", Profile: "engineer"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "bindings", "plain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "profile: engineer\n" {
		t.Fatalf("saved bytes = %q", got)
	}
	listed, err := binding.NewService(store).List()
	if err != nil {
		t.Fatal(err)
	}
	if listed.State != "ok" || len(listed.Bindings) != 1 || listed.Bindings[0].Name != "plain" {
		t.Fatalf("List after save = %+v", listed)
	}
}

func TestSaveRejectsBlankListEntriesLikeCairn(t *testing.T) {
	root := t.TempDir()
	svc, _ := testService(t, root, &fakeLauncher{})
	for _, input := range []Input{
		{Name: "bad-part", Profile: "engineer", Parts: []string{"docs", " "}},
		{Name: "bad-skill", Profile: "engineer", Skills: []string{""}},
		{Name: "bad-prompt", Profile: "engineer", Prompts: []string{"handoff", "\t"}},
	} {
		if _, err := svc.Save(input); err == nil || !strings.Contains(err.Error(), "names nothing") {
			t.Fatalf("Save(%q) error = %v; want names-nothing validation", input.Name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "bindings")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid saves changed bundle: %v", err)
	}
}

func TestSaveRejectsNamesThePaletteCannotLaunch(t *testing.T) {
	root := t.TempDir()
	svc, _ := testService(t, root, &fakeLauncher{})
	for _, name := range []string{"my binding", ".hidden", "nested/binding", "bad:name"} {
		if _, err := svc.Save(Input{Name: name, Profile: "engineer"}); err == nil {
			t.Fatalf("Save accepted invalid binding name %q", name)
		}
	}
	result, err := svc.Save(Input{Name: "launchable.yaml ", Profile: "engineer"})
	if err != nil {
		t.Fatalf("Save with a trimmed .yaml suffix: %v", err)
	}
	if result.Name != "launchable" || result.RelPath != "bindings/launchable.yaml" {
		t.Fatalf("Save result = %+v", result)
	}
}

func TestActionsHaveExactSaveAndLaunchCounts(t *testing.T) {
	root := t.TempDir()
	fake := &fakeLauncher{}
	svc, _ := testService(t, root, fake)
	input := Input{Name: "once", Profile: "engineer", Parts: []string{"docs"}, Sets: []launch.SetInput{{Slot: "x", Value: "y"}}}

	if err := svc.Launch(input); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("Launch calls = %d; want 1", fake.calls)
	}
	if _, err := os.Stat(filepath.Join(root, "bindings")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Launch wrote to bundle: %v", err)
	}
	if fake.inputs[0].Target != "engineer" {
		t.Fatalf("Launch target = %q", fake.inputs[0].Target)
	}

	if _, err := svc.SaveAndLaunch(input); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 {
		t.Fatalf("total launch calls = %d; want 2", fake.calls)
	}
	if fake.inputs[1].Target != "engineer" {
		t.Fatalf("SaveAndLaunch target = %q; want original profile", fake.inputs[1].Target)
	}
	if len(fake.inputs[1].Parts) != 1 || fake.inputs[1].Parts[0] != "docs" ||
		len(fake.inputs[1].Sets) != 1 || fake.inputs[1].Sets[0].Slot != "x" {
		t.Fatalf("SaveAndLaunch composition = %+v; want original parts and launch-only sets exactly once", fake.inputs[1])
	}
	entries, err := os.ReadDir(filepath.Join(root, "bindings"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "once.yaml" {
		t.Fatalf("saved entries = %v", entries)
	}
	if _, err := svc.Save(input); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate save error = %v", err)
	}
}

func TestSaveAndLaunchReportsPartialSuccess(t *testing.T) {
	root := t.TempDir()
	fake := &fakeLauncher{err: errors.New("boot unavailable")}
	svc, _ := testService(t, root, fake)
	result, err := svc.SaveAndLaunch(Input{Name: "saved-first", Profile: "engineer"})
	if err == nil || !strings.Contains(err.Error(), "saved bindings/saved-first.yaml, but launch failed") {
		t.Fatalf("SaveAndLaunch error = %v; want explicit partial-success report", err)
	}
	if result.RelPath != "bindings/saved-first.yaml" {
		t.Fatalf("SaveAndLaunch result = %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(root, "bindings", "saved-first.yaml")); statErr != nil {
		t.Fatalf("saved binding missing after launch failure: %v", statErr)
	}
}

func TestSaveWritesOneNewGitFileAndLiteralProjectPath(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Test"}, {"commit", "--allow-empty", "-qm", "baseline"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	svc, _ := testService(t, root, &fakeLauncher{})
	projectName := "Cairn Project"
	projectPath := "/worktrees/cairn"
	if _, err := svc.Save(Input{Name: "eng-cairn", Profile: "engineer", Scope: projectPath}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "?? bindings/eng-cairn.yaml\n" {
		t.Fatalf("git status = %q; want exactly one new binding file", out)
	}
	data, _ := os.ReadFile(filepath.Join(root, "bindings", "eng-cairn.yaml"))
	if !strings.Contains(string(data), "scope: "+projectPath+"\n") || strings.Contains(string(data), projectName) {
		t.Fatalf("binding did not contain only literal project path: %q", data)
	}
}
