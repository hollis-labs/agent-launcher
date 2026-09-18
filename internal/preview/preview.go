// Package preview asks Cairn for the effective result of a launch
// composition without planting a boot directory.
package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/compose"
	"github.com/hollis-labs/tachyon/internal/launch"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
	"github.com/hollis-labs/tachyon/internal/state"
)

// CompositionInput is exactly the input used to launch a composition —
// the same type, not a copy of its shape, so a field added to a launch
// cannot be forgotten here. Cairn, rather than Tachyon, resolves the launch
// profile's own cascade and the target profile's inherited state.
type CompositionInput = launch.CompositionInput

// Result is the read-only subset of `cairn show --json` presented by Tachyon.
// Skills retain Cairn's order unchanged. Advisory is exit-zero stderr and
// never turns the preview into a failed composition or launch.
type Result struct {
	Skills   []string `json:"skills"`
	Advisory string   `json:"advisory"`
}

// Runner is the process seam used by tests. Production uses ExecRunner with
// the cairn executable resolved from PATH for each request.
type Runner func(context.Context, []string) (stdout, stderr []byte, err error)

// ExecRunner runs one cairn command with the request context. Cancelling the
// Wails raw call therefore terminates the corresponding `cairn show` process.
func ExecRunner(cairnPath string) Runner {
	return func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, cairnPath, argv...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
}

// InvocationError retains both the exact preview argv and Cairn's stderr for
// start failures and nonzero exits.
type InvocationError struct {
	Argv   []string
	Stderr string
	Err    error
}

func (e *InvocationError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	if stderr == "" {
		return fmt.Sprintf("preview: cairn %s: %v", strings.Join(e.Argv, " "), e.Err)
	}
	return fmt.Sprintf("preview: cairn %s: %v: %s", strings.Join(e.Argv, " "), e.Err, stderr)
}

func (e *InvocationError) Unwrap() error { return e.Err }

// Options provides test seams. The application uses the zero value.
type Options struct {
	CairnPath string
	Runner    Runner

	// LaunchDir overrides where launch profiles are resolved from. Empty
	// means [state.LaunchDir].
	LaunchDir string
}

// Service is the single preview service shared by the palette and Manager.
type Service struct {
	store     bundle.RootStore
	cairnPath string
	runner    Runner
	launchDir string
}

func NewService(store bundle.RootStore, options Options) *Service {
	return &Service{
		store:     store,
		cairnPath: options.CairnPath,
		runner:    options.Runner,
		launchDir: options.LaunchDir,
	}
}

// launchProfilePath resolves the input's launch profile to a path, or ""
// when it names none.
//
// A preview MUST resolve it the same way a launch does. The launch profile
// is what declares the provider, and `cairn show` reports a different
// resolved skill set per provider — so a preview that skipped it would show
// the wrong answer for the launch it is previewing, which is the one thing
// a preview must never do.
//
// A name that does not resolve is an error rather than a silently dropped
// part, for the same reason: the preview would be of a composition nobody
// is about to run.
func (s *Service) launchProfilePath(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	dir := s.launchDir
	if dir == "" {
		var err error
		if dir, err = state.LaunchDir(); err != nil {
			return "", fmt.Errorf("preview: %w", err)
		}
	}
	p, err := launchprofile.Open(dir).Get(name)
	if err != nil {
		return "", fmt.Errorf("preview: resolving launch profile %q: %w", name, err)
	}
	return p.Path, nil
}

// Preview runs the exact `cairn show <composition> --json` request for the
// active bundle. The context is supplied by Wails and reaches
// exec.CommandContext through ExecRunner.
func (s *Service) Preview(ctx context.Context, input CompositionInput) (Result, error) {
	bundleRoot, err := s.store.Resolve()
	if err != nil {
		return Result{}, fmt.Errorf("preview: resolving bundle root: %w", err)
	}

	launchPath, err := s.launchProfilePath(input.LaunchProfile)
	if err != nil {
		return Result{}, err
	}

	argv, err := buildArgv(input, bundleRoot, launchPath)
	if err != nil {
		return Result{}, fmt.Errorf("preview: building cairn argv: %w", err)
	}

	runner := s.runner
	if runner == nil {
		path := s.cairnPath
		if path == "" {
			path, err = exec.LookPath("cairn")
			if err != nil {
				return Result{}, fmt.Errorf("preview: cairn not found on PATH: %w", err)
			}
		}
		runner = ExecRunner(path)
	}

	stdout, stderr, err := runner(ctx, argv)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, err
		}
		return Result{}, &InvocationError{Argv: argv, Stderr: string(stderr), Err: err}
	}

	skills, err := parseSkills(stdout)
	if err != nil {
		return Result{}, fmt.Errorf("preview: parse cairn show --json output: %w", err)
	}
	advisory := ""
	if strings.TrimSpace(string(stderr)) != "" {
		advisory = string(stderr)
	}
	return Result{Skills: skills, Advisory: advisory}, nil
}

// buildArgv uses compose.Arguments, the same pure encoder compose.Build uses
// for boot, then adds only show's subcommand and --json. It cannot emit
// --boot-root, --session, or --save-as because none is part of Arguments.
//
// The launch profile is carried through, in the same position a launch puts
// it, for the reason every other field is: a preview resolved against a
// different composition than the launch is a preview of the wrong thing.
// [launch.PartsWith] is what puts it there, so the ordering rule is written
// once.
func buildArgv(input CompositionInput, bundleRoot, launchPath string) ([]string, error) {
	sets := make([]compose.Set, len(input.Sets))
	for i, set := range input.Sets {
		sets[i] = compose.Set{Slot: set.Slot, Value: set.Value}
	}
	common, err := compose.Arguments(compose.Composition{
		Target: input.Target, Bundle: bundleRoot,
		Parts:  launch.PartsWith(launchPath, input.Parts),
		Skills: input.Skills, Prompts: input.Prompts, Sets: sets, Scope: input.Scope,
	})
	if err != nil {
		return nil, err
	}
	argv := append([]string{"show"}, common...)
	return append(argv, "--json"), nil
}

func parseSkills(stdout []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	var document map[string]json.RawMessage
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, errors.New("top-level value must be an object")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("stdout contains more than one JSON value")
		}
		return nil, err
	}

	specRaw, ok := document["spec"]
	if !ok || bytes.Equal(bytes.TrimSpace(specRaw), []byte("null")) {
		return nil, errors.New("spec must be an object")
	}
	var spec map[string]json.RawMessage
	if err := json.Unmarshal(specRaw, &spec); err != nil || spec == nil {
		if err == nil {
			err = errors.New("not an object")
		}
		return nil, fmt.Errorf("spec must be an object: %w", err)
	}

	skillsRaw, ok := spec["skills"]
	if !ok {
		return []string{}, nil
	}
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(skillsRaw, &entry); err != nil || entry == nil {
		if err == nil {
			err = errors.New("not an object")
		}
		return nil, fmt.Errorf("spec.skills must be an object: %w", err)
	}
	value, ok := entry["value"]
	if !ok {
		return nil, errors.New("spec.skills.value is missing")
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return []string{}, nil
	}
	var members []json.RawMessage
	if err := json.Unmarshal(value, &members); err != nil || members == nil {
		if err == nil {
			err = errors.New("not an array")
		}
		return nil, fmt.Errorf("spec.skills.value must be an array of strings: %w", err)
	}
	skills := make([]string, len(members))
	for i, member := range members {
		if bytes.Equal(bytes.TrimSpace(member), []byte("null")) {
			return nil, fmt.Errorf("spec.skills.value[%d] must be a string", i)
		}
		if err := json.Unmarshal(member, &skills[i]); err != nil {
			return nil, fmt.Errorf("spec.skills.value[%d] must be a string: %w", i, err)
		}
	}
	return skills, nil
}
