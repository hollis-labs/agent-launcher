// Package project owns Tachyon's saved project records. Projects are UI
// conveniences: their paths are copied into bindings as literals, and neither
// Cairn nor the bundle ever sees a project name.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/state"
)

const fileName = "projects.json"

var (
	ErrNotFound = errors.New("project: not found")
	ErrExists   = errors.New("project: already exists")
)

// Project is deliberately an object rather than a positional pair: project
// records are expected to gain fields. Unknown fields are retained across an
// edit so opening this file with an older Tachyon cannot erase newer data.
type Project struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	extra map[string]json.RawMessage
}

func (p *Project) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["name"]; ok {
		if err := json.Unmarshal(raw, &p.Name); err != nil {
			return fmt.Errorf("project: name: %w", err)
		}
		delete(fields, "name")
	}
	if raw, ok := fields["path"]; ok {
		if err := json.Unmarshal(raw, &p.Path); err != nil {
			return fmt.Errorf("project: path: %w", err)
		}
		delete(fields, "path")
	}
	p.extra = fields
	return nil
}

func (p Project) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(p.extra)+2)
	for key, value := range p.extra {
		fields[key] = value
	}
	name, _ := json.Marshal(p.Name)
	path, _ := json.Marshal(p.Path)
	fields["name"], fields["path"] = name, path
	return json.Marshal(fields)
}

func (p Project) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("project: name is required")
	}
	if strings.TrimSpace(p.Path) == "" {
		return errors.New("project: path is required")
	}
	return nil
}

type document struct {
	Projects []Project `json:"projects"`
	extra    map[string]json.RawMessage
}

func (d *document) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["projects"]; ok {
		if err := json.Unmarshal(raw, &d.Projects); err != nil {
			return fmt.Errorf("project: projects: %w", err)
		}
		delete(fields, "projects")
	}
	if d.Projects == nil {
		d.Projects = []Project{}
	}
	d.extra = fields
	return nil
}

func (d document) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(d.extra)+1)
	for key, value := range d.extra {
		fields[key] = value
	}
	projects, err := json.Marshal(d.Projects)
	if err != nil {
		return nil, err
	}
	fields["projects"] = projects
	return json.Marshal(fields)
}

// Store persists all records in one forward-compatible JSON document.
type Store struct{ Path string }

func DefaultStore() (Store, error) {
	root, err := state.Root()
	if err != nil {
		return Store{}, fmt.Errorf("project: locating state root: %w", err)
	}
	return Store{Path: filepath.Join(root, fileName)}, nil
}

func (s Store) List() ([]Project, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	sort.Slice(doc.Projects, func(i, j int) bool { return doc.Projects[i].Name < doc.Projects[j].Name })
	return doc.Projects, nil
}

func (s Store) load() (document, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return document{Projects: []Project{}}, nil
	}
	if err != nil {
		return document{}, fmt.Errorf("project: reading %s: %w", s.Path, err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("project: parsing %s: %w", s.Path, err)
	}
	return doc, nil
}

func (s Store) Create(p Project) error {
	if err := p.validate(); err != nil {
		return err
	}
	doc, err := s.load()
	if err != nil {
		return err
	}
	for _, existing := range doc.Projects {
		if existing.Name == p.Name {
			return ErrExists
		}
	}
	doc.Projects = append(doc.Projects, p)
	return s.save(doc)
}

// Update identifies the existing record by oldName, allowing a rename while
// preserving any fields this build does not understand.
func (s Store) Update(oldName string, p Project) error {
	if err := p.validate(); err != nil {
		return err
	}
	doc, err := s.load()
	if err != nil {
		return err
	}
	found := -1
	for i, existing := range doc.Projects {
		if existing.Name == oldName {
			found = i
		}
		if existing.Name == p.Name && existing.Name != oldName {
			return ErrExists
		}
	}
	if found < 0 {
		return ErrNotFound
	}
	p.extra = doc.Projects[found].extra
	doc.Projects[found] = p
	return s.save(doc)
}

func (s Store) Delete(name string) error {
	doc, err := s.load()
	if err != nil {
		return err
	}
	for i, p := range doc.Projects {
		if p.Name == name {
			doc.Projects = append(doc.Projects[:i], doc.Projects[i+1:]...)
			return s.save(doc)
		}
	}
	return ErrNotFound
}

func (s Store) save(doc document) error {
	sort.Slice(doc.Projects, func(i, j int) bool { return doc.Projects[i].Name < doc.Projects[j].Name })
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("project: encoding records: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("project: creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.Path)+".*")
	if err != nil {
		return fmt.Errorf("project: creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("project: writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("project: closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("project: replacing %s: %w", s.Path, err)
	}
	return nil
}

type View struct {
	Project  Project           `json:"project"`
	Bindings []binding.Binding `json:"bindings"`
}

type Service struct {
	projects Store
	bundles  bundle.RootStore
}

func NewService(projects Store, bundles bundle.RootStore) *Service {
	return &Service{projects: projects, bundles: bundles}
}

// List associates bindings by literal scope equality only. It performs no
// path cleaning, expansion, symlink resolution, or closest-wins lookup.
func (s *Service) List() ([]View, error) {
	projects, err := s.projects.List()
	if err != nil {
		return nil, err
	}
	root, err := s.bundles.Resolve()
	if err != nil {
		return nil, err
	}
	bindings, err := binding.Open(root).List()
	if errors.Is(err, binding.ErrBindingsDirMissing) {
		bindings, err = []binding.Binding{}, nil
	}
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(projects))
	for _, p := range projects {
		v := View{Project: p, Bindings: []binding.Binding{}}
		for _, b := range bindings {
			if b.Scope == p.Path {
				v.Bindings = append(v.Bindings, b)
			}
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *Service) Create(p Project) (Project, error) { return p, s.projects.Create(p) }
func (s *Service) Update(oldName string, p Project) (Project, error) {
	return p, s.projects.Update(oldName, p)
}
func (s *Service) Delete(name string) error { return s.projects.Delete(name) }
