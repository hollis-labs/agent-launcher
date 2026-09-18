// Package launchcomposer turns a compose-form selection into a saved launch
// profile, and launches it.
//
// It replaces the binding composer, which wrote <bundle>/bindings/<name>.yaml
// in a format of Tachyon's own that cairn could not read. This one writes an
// ordinary cairn part into Tachyon's launch store, so the thing saved is the
// thing cairn consumes — see internal/launchprofile.
//
// # Only the durable half is saved
//
// A compose form holds two kinds of field, and the difference is not
// cosmetic:
//
//	provider, skills, prompts   facts about how this agent runs    SAVED
//	target, scope, parts, sets  facts about this one launch        NOT SAVED
//
// The second group is not omitted for want of a place to put it. A target is
// the agent profile, which is chosen beside a launch profile rather than
// inside one. A scope cannot be saved at all — cairn refuses `scope:` as
// frontmatter. `--set` is documented as a one-off inline literal for this
// materialization. And a `--with` part is already an ordinary profile: a
// composition worth reusing is a profile worth naming, which is what saving
// one here produces.
//
// [Save] returns what it did not carry ([Result.Dropped]) rather than
// dropping it silently. A person who added three parts and a set, saved, and
// found none of it in the file would reasonably conclude the save was
// broken.
package launchcomposer

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hollis-labs/tachyon/internal/launch"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
	"github.com/hollis-labs/tachyon/internal/state"
	"gopkg.in/yaml.v3"
)

// Input is the compose form's whole state, as the frontend sends it.
type Input struct {
	// Name is the launch profile to write. Required by [Save].
	Name string `json:"name"`

	// Provider is the harness — the one key a launch profile must carry,
	// since no profile in agent-setup declares one and cairn refuses to
	// render without it.
	Provider string `json:"provider"`

	// Skills and Prompts become spec.skills and spec.prompts. Both are
	// additive in cairn, which is why they are safe to store: a saved
	// profile that carries them adds them, and can never be read as the
	// complete set.
	Skills  []string `json:"skills"`
	Prompts []string `json:"prompts"`

	// Target is the agent profile to boot. Used by [Launch] and
	// [SaveAndLaunch]; never written into the launch profile.
	Target string `json:"target"`

	// Scope is the selected project's path. Used by the launching methods;
	// cannot be written into a launch profile at all.
	Scope string `json:"scope"`

	// Parts and Sets are one-off additions for this launch. Used by the
	// launching methods; not written.
	Parts []string          `json:"parts"`
	Sets  []launch.SetInput `json:"sets"`
}

// Result is a written launch profile, plus what was left out of it.
type Result struct {
	Name string `json:"name"`
	Path string `json:"path"`

	// Dropped names the compose-form fields that carried a value and were
	// not saved, so the frontend can say so. Empty when everything the
	// person filled in was durable.
	Dropped []string `json:"dropped,omitempty"`
}

type launcher interface {
	LaunchComposition(launch.CompositionInput) error
}

// Service is bound to the frontend as a Wails service.
type Service struct {
	launcher launcher

	// dir overrides the launch store. Empty means [state.LaunchDir].
	dir string
}

// NewService returns a Service writing to the default launch store.
func NewService(l launcher) *Service { return &Service{launcher: l} }

// NewServiceAt returns a Service writing to dir. Tests use it.
func NewServiceAt(l launcher, dir string) *Service { return &Service{launcher: l, dir: dir} }

func (s *Service) store() (launchprofile.Store, error) {
	if s.dir != "" {
		return launchprofile.Open(s.dir), nil
	}
	dir, err := state.LaunchDir()
	if err != nil {
		return launchprofile.Store{}, fmt.Errorf("launchcomposer: %w", err)
	}
	return launchprofile.Open(dir), nil
}

// Save writes the launch profile, refusing a name that is already taken.
func (s *Service) Save(input Input) (Result, error) {
	name := strings.TrimSpace(input.Name)
	if err := launchprofile.ValidateName(name); err != nil {
		return Result{}, err
	}
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		// Refused rather than defaulted. A launch profile with no provider
		// is refused by cairn at every boot, so writing one produces a file
		// that looks saved and never works; and picking a provider on
		// someone's behalf is the inference three components just agreed
		// nobody makes.
		return Result{}, fmt.Errorf("launchcomposer: %q needs a provider: cairn refuses to render a profile that declares none", name)
	}

	doc, err := marshal(name, provider, input.Skills, input.Prompts)
	if err != nil {
		return Result{}, err
	}

	st, err := s.store()
	if err != nil {
		return Result{}, err
	}
	p, err := st.Create(name, doc)
	if err != nil {
		return Result{}, err
	}
	return Result{Name: p.Name, Path: p.Path, Dropped: dropped(input)}, nil
}

// Launch runs the composition without saving anything.
func (s *Service) Launch(input Input) error {
	return s.launcher.LaunchComposition(launch.CompositionInput{
		Target:        input.Target,
		LaunchProfile: strings.TrimSpace(input.Name),
		Skills:        input.Skills,
		Prompts:       input.Prompts,
		Scope:         input.Scope,
		Sets:          input.Sets,
		Parts:         input.Parts,
	})
}

// SaveAndLaunch writes the launch profile and then launches through it.
//
// It launches through the profile it just saved rather than replaying the
// form's fields, which is the opposite of what the binding composer did and
// is correct for the same reason that one was: the saved artifact must be
// what runs. A binding was a format cairn could not read, so launching "the
// binding just saved" would have applied its parts twice — once from the
// file and once from the flags. A launch profile IS what cairn reads, so
// launching through it applies each thing exactly once, and any drift
// between what was saved and what runs shows up here rather than on the
// next launch.
//
// Skills and prompts are therefore not passed again: they are in the file.
// Parts, sets and scope still are — they were never saved, and a person who
// composed them expects this launch to have them.
func (s *Service) SaveAndLaunch(input Input) (Result, error) {
	result, err := s.Save(input)
	if err != nil {
		return Result{}, err
	}
	err = s.launcher.LaunchComposition(launch.CompositionInput{
		Target:        input.Target,
		LaunchProfile: result.Name,
		Scope:         input.Scope,
		Sets:          input.Sets,
		Parts:         input.Parts,
	})
	if err != nil {
		return result, fmt.Errorf("launchcomposer: saved %s, but launch failed: %w", result.Path, err)
	}
	return result, nil
}

// profileDoc is the frontmatter [marshal] writes. It is deliberately a
// narrow subset of what cairn accepts: this composer authors the handful of
// keys a form can express, and the manager's text editor is where anything
// else is written.
type profileDoc struct {
	ID       string   `yaml:"id"`
	Provider string   `yaml:"provider"`
	Spec     *specDoc `yaml:"spec,omitempty"`
}

type specDoc struct {
	Skills  []string `yaml:"skills,omitempty"`
	Prompts []string `yaml:"prompts,omitempty"`
}

// marshal renders the launch profile: YAML frontmatter between two fences,
// which is the shape cairn parses.
func marshal(name, provider string, skills, prompts []string) ([]byte, error) {
	cleanSkills, err := cleanList(name, "skills", skills)
	if err != nil {
		return nil, err
	}
	cleanPrompts, err := cleanList(name, "prompts", prompts)
	if err != nil {
		return nil, err
	}

	doc := profileDoc{ID: name, Provider: provider}
	if len(cleanSkills) > 0 || len(cleanPrompts) > 0 {
		doc.Spec = &specDoc{Skills: cleanSkills, Prompts: cleanPrompts}
	}

	var body bytes.Buffer
	enc := yaml.NewEncoder(&body)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("launchcomposer: rendering %q: %w", name, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("launchcomposer: rendering %q: %w", name, err)
	}

	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(body.Bytes())
	out.WriteString("---\n")
	return out.Bytes(), nil
}

// cleanList trims each value and refuses one that names nothing: a blank
// entry becomes a blank id in a collection cairn keys by id, which is a
// lookup failure at boot rather than here.
func cleanList(name, key string, values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for i, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, fmt.Errorf("launchcomposer: %q %s[%d] names nothing", name, key, i)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// dropped names the fields that held a value and were not written.
func dropped(input Input) []string {
	var out []string
	if strings.TrimSpace(input.Scope) != "" {
		out = append(out, "scope")
	}
	if len(input.Parts) > 0 {
		out = append(out, "parts")
	}
	if len(input.Sets) > 0 {
		out = append(out, "sets")
	}
	return out
}
