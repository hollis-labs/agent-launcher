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
	const launchPath = "/config/tachyon/launch/default.md"
	input := CompositionInput{
		Target:        "engineer",
		LaunchProfile: "default",
		Parts:         []string{"observability", "review-policy"},
		Skills:        []string{"test-first", "commit"},
		Prompts:       []string{"report", "handoff"},
		Sets: []launch.SetInput{
			{Slot: "tone", Value: "terse"},
			{Slot: "audience", Value: "team"},
		},
		Scope: "/work/tachyon",
	}
	got, err := buildArgv(input, "/active/bundle", launchPath)
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	want := []string{
		"show", "engineer", "--profile", "/active/bundle",
		// The launch profile first, exactly where a launch puts it: a
		// preview that resolved a different set of parts than the launch
		// would be a preview of a different composition.
		"--with", launchPath,
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
	const launchPath = "/config/tachyon/launch/default.md"
	show, err := buildArgv(input, "/bundle", launchPath)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := compose.Build(compose.Composition{
		Target: input.Target, Bundle: "/bundle", BootRoot: "/state/boot",
		Parts:  launch.PartsWith(launchPath, input.Parts),
		Skills: input.Skills, Prompts: input.Prompts,
		Sets: []compose.Set{{Slot: "role", Value: "reviewer"}}, Scope: input.Scope,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both argvs are <verb> <common...> --json, with boot inserting
	// --boot-root in the middle. Compare the common parts by dropping the
	// verb and the trailing --json from each, and --boot-root's pair from
	// boot -- found by name rather than by index, so an added boot-only
	// flag does not silently shift this comparison onto the wrong tokens.
	showCommon := show[1 : len(show)-1]
	bootCommon := dropFlagPair(boot[1:len(boot)-1], "--boot-root")
	if !reflect.DeepEqual(showCommon, bootCommon) {
		t.Fatalf("show common args = %#v\nboot common args = %#v", showCommon, bootCommon)
	}
	for _, forbidden := range []string{"--boot-root", "--session", "--save-as", "--provider"} {
		if contains(show, forbidden) {
			t.Errorf("preview argv contains boot/save-only flag %s: %v", forbidden, show)
		}
	}
}

// dropFlagPair removes flag and the value after it from argv.
func dropFlagPair(argv []string, flag string) []string {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		if argv[i] == flag {
			i++ // skip its value too
			continue
		}
		out = append(out, argv[i])
	}
	return out
}

// TestPreviewSendsOnlyWhatTheFormAdded: the preview forwards the launch
// profile and the modal's own additions, and never flattens what either of
// them already resolves to. cairn resolves the cascade; a launcher that
// pre-expanded it would be sending a second, necessarily-stale copy.
func TestPreviewSendsOnlyWhatTheFormAdded(t *testing.T) {
	const launchPath = "/config/tachyon/launch/codex.md"
	got, err := buildArgv(CompositionInput{
		Target: "engineer", LaunchProfile: "codex",
		Parts: []string{"modal-part"}, Skills: []string{"modal-skill"},
	}, "/bundle", launchPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"show", "engineer", "--profile", "/bundle",
		"--with", launchPath, "--with", "modal-part",
		"--skill", "modal-skill", "--json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v; want %v", got, want)
	}
	for _, resolved := range []string{"base", "search-first", "surface-discovery", "claude", "codex"} {
		if contains(got, resolved) {
			t.Errorf("preview flattened/resubmitted %q, which cairn resolves itself: %v", resolved, got)
		}
	}
}

func TestBuildArgvOmitsEveryEmptyOptional(t *testing.T) {
	got, err := buildArgv(CompositionInput{Target: "engineer"}, "/bundle", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"show", "engineer", "--profile", "/bundle", "--json"}
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
