package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/launch"
)

type runnerResult struct {
	stdout []byte
	stderr []byte
	err    error
	argv   []string
}

func (r *runnerResult) run(_ context.Context, argv []string) ([]byte, []byte, error) {
	r.argv = append([]string(nil), argv...)
	return r.stdout, r.stderr, r.err
}

func previewService(t *testing.T, runner Runner) *Service {
	t.Helper()
	root := t.TempDir()
	store := bundle.RootStore{Path: filepath.Join(t.TempDir(), "root.json")}
	if err := store.Save(root); err != nil {
		t.Fatalf("RootStore.Save: %v", err)
	}
	return NewService(store, Options{Runner: runner})
}

func validShow(skills string) []byte {
	return []byte(`{"profile":"engineer","spec":` + skills + `}`)
}

func TestBuildArgvExactAndOrdered(t *testing.T) {
	input := CompositionInput{
		Target:  "saved-binding",
		Parts:   []string{"observability", "review-policy"},
		Skills:  []string{"test-first", "commit"},
		Prompts: []string{"report", "handoff"},
		Sets: []launch.SetInput{
			{Slot: "tone", Value: "terse"},
			{Slot: "audience", Value: "team"},
		},
		Scope: "/work/tachyon",
	}
	got, err := buildArgv(input, "/active/bundle")
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	want := []string{
		"show", "saved-binding", "--profile", "/active/bundle",
		"--with", "observability", "--with", "review-policy",
		"--skill", "test-first,commit", "--prompt", "report,handoff",
		"--set", "tone=terse", "--set", "audience=team",
		"--scope", "/work/tachyon", "--json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildArgv = %#v\nwant %#v", got, want)
	}
}

func TestPreviewAndBootShareCanonicalCompositionSubsequence(t *testing.T) {
	input := CompositionInput{
		Target: "engineer", Parts: []string{"one", "two"},
		Skills: []string{"s1", "s2"}, Prompts: []string{"p1", "p2"},
		Sets: []launch.SetInput{{Slot: "role", Value: "reviewer"}}, Scope: "/scope",
	}
	show, err := buildArgv(input, "/bundle")
	if err != nil {
		t.Fatal(err)
	}
	boot, err := compose.Build(compose.Composition{
		Target: input.Target, Bundle: "/bundle", BootRoot: "/state/boot",
		Parts: input.Parts, Skills: input.Skills, Prompts: input.Prompts,
		Sets: []compose.Set{{Slot: "role", Value: "reviewer"}}, Scope: input.Scope,
	})
	if err != nil {
		t.Fatal(err)
	}
	showCommon := show[1 : len(show)-1]
	bootCommon := append([]string{}, boot[1:4]...)
	bootCommon = append(bootCommon, boot[8:len(boot)-1]...)
	if !reflect.DeepEqual(showCommon, bootCommon) {
		t.Fatalf("show common args = %#v\nboot common args = %#v", showCommon, bootCommon)
	}
	for _, forbidden := range []string{"--boot-root", "--session", "--save-as"} {
		if contains(show, forbidden) {
			t.Errorf("preview argv contains boot/save-only flag %s: %v", forbidden, show)
		}
	}
}

func TestSavedBindingPreviewTargetsBindingAndSendsOnlyModalAdditions(t *testing.T) {
	got, err := buildArgv(CompositionInput{
		Target: "eng-tachyon", Parts: []string{"modal-part"}, Skills: []string{"modal-skill"},
	}, "/bundle")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"show", "eng-tachyon", "--profile", "/bundle", "--with", "modal-part", "--skill", "modal-skill", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved-binding argv = %v; want %v", got, want)
	}
	for _, savedMember := range []string{"engineer", "saved-part", "saved-skill"} {
		if contains(got, savedMember) {
			t.Errorf("saved-binding preview flattened/resubmitted %q: %v", savedMember, got)
		}
	}
}

func TestBuildArgvOmitsEveryEmptyOptional(t *testing.T) {
	got, err := buildArgv(CompositionInput{Target: "saved-default-scope"}, "/bundle")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"show", "saved-default-scope", "--profile", "/bundle", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("minimal preview = %v; want %v", got, want)
	}
}

func TestPreviewReturnsCairnsCompleteOrderedSkillListUnchanged(t *testing.T) {
	runner := &runnerResult{stdout: validShow(`{"skills":{"value":["base-skill","part-skill","binding-skill","added-skill"],"contributors":["base","part-a","binding \"eng\"","--skill"]}}`)}
	result, err := previewService(t, runner.run).Preview(context.Background(), CompositionInput{Target: "eng", Skills: []string{"added-skill"}})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	want := []string{"base-skill", "part-skill", "binding-skill", "added-skill"}
	if !reflect.DeepEqual(result.Skills, want) {
		t.Fatalf("skills = %#v; want Cairn order %#v", result.Skills, want)
	}
}

func TestParseSkillsMissingAndNullAreEmpty(t *testing.T) {
	for name, document := range map[string][]byte{
		"missing": validShow(`{}`),
		"null":    validShow(`{"skills":{"value":null,"contributors":[]}}`),
		"empty":   validShow(`{"skills":{"value":[],"contributors":[]}}`),
	} {
		t.Run(name, func(t *testing.T) {
			skills, err := parseSkills(document)
			if err != nil {
				t.Fatalf("parseSkills: %v", err)
			}
			if skills == nil || len(skills) != 0 {
				t.Fatalf("skills = %#v; want non-nil empty list", skills)
			}
		})
	}
}

func TestParseSkillsRejectsWrongValueShapes(t *testing.T) {
	for name, value := range map[string]string{
		"string":        `"commit"`,
		"object":        `{}`,
		"number":        `1`,
		"mixed array":   `["commit",null]`,
		"missing value": `__MISSING__`,
	} {
		t.Run(name, func(t *testing.T) {
			entry := `{"skills":{"value":` + value + `}}`
			if value == "__MISSING__" {
				entry = `{"skills":{"contributors":[]}}`
			}
			if _, err := parseSkills(validShow(entry)); err == nil {
				t.Fatal("parseSkills accepted invalid spec.skills.value")
			}
		})
	}
}

func TestParseSkillsRejectsMalformedOrInvalidDocument(t *testing.T) {
	for name, document := range map[string][]byte{
		"malformed":       []byte(`{"spec":`),
		"second value":    []byte(`{"spec":{}} {"spec":{}}`),
		"null document":   []byte(`null`),
		"missing spec":    []byte(`{}`),
		"non-object spec": []byte(`{"spec":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSkills(document); err == nil {
				t.Fatal("parseSkills accepted invalid show document")
			}
		})
	}
}

func TestPreviewReturnsExitZeroStderrAsAdvisory(t *testing.T) {
	runner := &runnerResult{
		stdout: validShow(`{"skills":{"value":null,"contributors":[]}}`),
		stderr: []byte("warning: scope could not be resolved\n"),
	}
	result, err := previewService(t, runner.run).Preview(context.Background(), CompositionInput{Target: "engineer"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if result.Advisory != string(runner.stderr) {
		t.Fatalf("advisory = %q; want captured stderr %q", result.Advisory, runner.stderr)
	}
}

func TestPreviewProcessFailureRetainsStderr(t *testing.T) {
	runner := &runnerResult{stderr: []byte("profile not found\n"), err: errors.New("exit status 1")}
	_, err := previewService(t, runner.run).Preview(context.Background(), CompositionInput{Target: "missing"})
	var invocation *InvocationError
	if !errors.As(err, &invocation) {
		t.Fatalf("error = %T %v; want InvocationError", err, err)
	}
	if invocation.Stderr != string(runner.stderr) || !strings.Contains(err.Error(), "profile not found") {
		t.Fatalf("error did not retain stderr: %+v", invocation)
	}
}

func TestPreviewProcessStartFailure(t *testing.T) {
	svc := previewService(t, ExecRunner(filepath.Join(t.TempDir(), "missing-cairn")))
	_, err := svc.Preview(context.Background(), CompositionInput{Target: "engineer"})
	var invocation *InvocationError
	if !errors.As(err, &invocation) {
		t.Fatalf("error = %T %v; want InvocationError", err, err)
	}
}

func TestPreviewCancellationTerminatesCommandContext(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	script := filepath.Join(dir, "cairn")
	body := "#!/bin/sh\n: > \"" + started + "\"\nexec /bin/sleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := previewService(t, ExecRunner(script))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := svc.Preview(ctx, CompositionInput{Target: "engineer"})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cairn test process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Preview cancellation error = %v; want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Preview did not terminate its command after context cancellation")
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
