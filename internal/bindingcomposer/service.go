// Package bindingcomposer owns the manager's create-only binding workflow.
// It deliberately sits above bundle and launch: a composed binding has a
// richer shape than internal/binding's profile/scope editor model, while its
// one-off sets belong only to the launch that follows a save.
package bindingcomposer

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/launch"
	"gopkg.in/yaml.v3"
)

var ErrAlreadyExists = errors.New("binding composer: binding already exists")

var bindingNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type Input struct {
	Name    string            `json:"name"`
	Profile string            `json:"profile"`
	Parts   []string          `json:"parts"`
	Skills  []string          `json:"skills"`
	Prompts []string          `json:"prompts"`
	Sets    []launch.SetInput `json:"sets"`
	Scope   string            `json:"scope"`
}

type Result struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	RelPath string `json:"relPath"`
}

type launcher interface {
	LaunchComposition(launch.CompositionInput) error
}

type Service struct {
	store    bundle.RootStore
	launcher launcher
}

func NewService(store bundle.RootStore, launcher launcher) *Service {
	return &Service{store: store, launcher: launcher}
}

func (s *Service) Save(input Input) (Result, error) {
	root, err := s.store.Resolve()
	if err != nil {
		return Result{}, fmt.Errorf("binding composer: resolving bundle root: %w", err)
	}
	name, err := bindingName(input.Name)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(input.Profile) == "" {
		return Result{}, errors.New("binding composer: profile is required")
	}
	data, err := marshal(input)
	if err != nil {
		return Result{}, err
	}
	dir := filepath.Join(root, "bindings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, fmt.Errorf("binding composer: creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, name+".yaml")
	if err := createAtomic(path, data); err != nil {
		return Result{}, err
	}
	return Result{Name: name, Path: path, RelPath: filepath.ToSlash(filepath.Join("bindings", name+".yaml"))}, nil
}

func (s *Service) Launch(input Input) error {
	return s.launcher.LaunchComposition(launchInput(input, input.Profile))
}

func (s *Service) SaveAndLaunch(input Input) (Result, error) {
	result, err := s.Save(input)
	if err != nil {
		return Result{}, err
	}
	// Launch the composition the person built, not the binding just saved.
	// Targeting result.Name while also forwarding Parts/Skills/Prompts would
	// apply those additions once from the binding and then a second time from
	// the launch flags. It would also make this action behave differently from
	// Launch for no user-visible reason.
	if err := s.launcher.LaunchComposition(launchInput(input, input.Profile)); err != nil {
		return result, fmt.Errorf("binding composer: saved %s, but launch failed: %w", result.RelPath, err)
	}
	return result, nil
}

func launchInput(input Input, target string) launch.CompositionInput {
	return launch.CompositionInput{
		Target: target, Skills: input.Skills, Prompts: input.Prompts,
		Scope: input.Scope, Sets: input.Sets, Parts: input.Parts,
	}
}

type bindingFile struct {
	Profile string   `yaml:"profile"`
	Parts   []string `yaml:"parts,omitempty"`
	Skills  []string `yaml:"skills,omitempty"`
	Prompts []string `yaml:"prompts,omitempty"`
	Scope   string   `yaml:"scope,omitempty"`
}

func marshal(input Input) ([]byte, error) {
	parts, err := listOf(input.Name, "parts", input.Parts)
	if err != nil {
		return nil, err
	}
	skills, err := listOf(input.Name, "skills", input.Skills)
	if err != nil {
		return nil, err
	}
	prompts, err := listOf(input.Name, "prompts", input.Prompts)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err = enc.Encode(bindingFile{
		Profile: strings.TrimSpace(input.Profile),
		Parts:   parts, Skills: skills,
		Prompts: prompts, Scope: strings.TrimSpace(input.Scope),
	})
	if err == nil {
		err = enc.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("binding composer: rendering %q: %w", input.Name, err)
	}
	return buf.Bytes(), nil
}

func listOf(name, key string, values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("binding composer: binding %q %s[%d] names nothing", name, key, i)
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func bindingName(raw string) (string, error) {
	name := strings.TrimSuffix(strings.TrimSpace(raw), ".yaml")
	if !bindingNamePattern.MatchString(name) {
		return "", fmt.Errorf("binding composer: invalid binding name %q: must match %s", raw, bindingNamePattern.String())
	}
	return name, nil
}

// createAtomic publishes a fully-written inode with link(2). Link is both
// atomic and exclusive: unlike rename it cannot replace an existing target.
func createAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tachyon-binding-*.tmp")
	if err != nil {
		return fmt.Errorf("binding composer: creating temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("binding composer: writing temporary file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("binding composer: setting file mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("binding composer: syncing temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("binding composer: closing temporary file: %w", err)
	}
	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", ErrAlreadyExists, path)
		}
		return fmt.Errorf("binding composer: publishing %s: %w", path, err)
	}
	// Best-effort directory sync makes the new entry durable. Publication has
	// already succeeded, so a platform refusing directory sync is not allowed
	// to turn a successful create into a misleading failure.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
