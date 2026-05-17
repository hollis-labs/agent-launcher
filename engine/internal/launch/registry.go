package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hollis-labs/go-agent-launch/agentlaunch"

	"github.com/hollis-labs/tachyon/engine/internal/corpus"
)

// Spec is one launchable entry surfaced by `list`. It is a launch bag
// (one concrete invocation of the canonical LaunchSpec) flattened with
// the facets the Swift app filters and renders on.
type Spec struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Project string            `json:"project"`
	Role    string            `json:"role"`
	Summary string            `json:"summary"`
	Facets  map[string]string `json:"facets"`
}

// Catalog is a resolved view over the local-first launch corpus: the one
// canonical LaunchSpec, every launch bag registered against it, and the
// runtime-binding registrar that resolves `runner` tokens.
type Catalog struct {
	Spec  agentlaunch.LaunchSpec
	Specs []Spec
	bags  map[string]agentlaunch.LaunchBag

	// registrar serves runtime-binding queries — it is the file-backed
	// registrar populated from the corpus providers/ subdir. ResolveRunner
	// queries it through agentlaunch.ResolveRuntimeBinding.
	registrar agentlaunch.Registrar
	// registrarDesc is the registrar descriptor the query envelope
	// requires (file-backed mode + the materialized catalog root).
	registrarDesc agentlaunch.RegistryRegistrar
	// runners is every runner token the providers/ subdir registered a
	// runtime-binding for — the `describe` contract's `runners` array.
	runners []string
	// cleanup frees the materialized corpus root; see Close.
	cleanup func()
}

// launchesSubdir is the catalog subdirectory the S4.4 launch bags live
// under. go-agent-launch's file-backed registrar maps it to the
// execution-template registry kind (see CatalogSubdirKind).
const launchesSubdir = "launches"

// CatalogRootUsable reports whether dir is a reachable directory that
// could serve as an on-disk catalog root. An empty string, a missing
// path, or a non-directory all return false — the signal the engine uses
// to fall back to the embedded corpus (local-first, D1).
func CatalogRootUsable(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// LoadCatalog resolves the local-first launch corpus: the canonical S4.4
// LaunchSpec plus every launch bag registered against it.
//
// This is the LOCAL-FIRST fallback the FROZEN contract requires (D1):
// with no directory service reachable, the engine walks the embedded
// S4.4 corpus — materialized to a temp catalog root — exactly like
// go-agent-launch's file-backed registrar mode, a permanent first-class
// network-free path. The launches/ subdir is the execution-template
// kind per agentlaunch.CatalogSubdirKind; each file is one LaunchBag
// loaded with the schema-correct agentlaunch.LoadLaunchBag.
//
// The engine has no directory-service client: the embedded corpus is the
// single source. An on-disk ~/.tether/catalog is therefore always
// "unreachable" from the engine's standpoint and the corpus fallback is
// always taken — which is exactly why `list` works fully offline.
//
// (The file-backed registrar's RegistrationRecord ingest keys on a
// legacy `id:` field; S4.4 launch bags carry `spec:` + `name:` instead,
// so the bag bodies are loaded directly with LoadLaunchBag — the
// registrar's own bag loader — rather than through the id-keyed record
// walk. The catalog-root layout and the launches→execution-template
// mapping are still the go-agent-launch file-backed contract.)
func LoadCatalog() (*Catalog, error) {
	root, cleanup, err := corpus.Materialize()
	if err != nil {
		return nil, err
	}
	// The materialized root must SURVIVE LoadCatalog: agentlaunch.
	// ResolveRuntimeBinding reads the runtime-binding source file off disk
	// at resolve-time (the registry holds handles, not content — D2). The
	// Catalog therefore owns the temp root and frees it via Close(). The
	// engine is a short-lived CLI process, so a missing Close() only leaks
	// one temp dir for the process lifetime.
	keep := false
	defer func() {
		if !keep {
			cleanup()
		}
	}()

	spec, err := agentlaunch.LoadLaunchSpec(joinCorpus(root, corpus.AssemblyFile))
	if err != nil {
		return nil, fmt.Errorf("load launch spec: %w", err)
	}

	// launches/ must map to the execution-template kind — assert the
	// go-agent-launch file-backed catalog contract holds.
	if kind, ok := agentlaunch.CatalogSubdirKind(launchesSubdir); !ok ||
		kind != agentlaunch.RegistryKindExecutionTemplate {
		return nil, fmt.Errorf("corpus: %q is not the execution-template catalog subdir", launchesSubdir)
	}

	launchesDir := filepath.Join(root, launchesSubdir)
	entries, err := os.ReadDir(launchesDir)
	if err != nil {
		return nil, fmt.Errorf("read launches: %w", err)
	}

	cat := &Catalog{Spec: spec, bags: map[string]agentlaunch.LaunchBag{}}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		bag, berr := agentlaunch.LoadLaunchBag(filepath.Join(launchesDir, e.Name()))
		if berr != nil {
			// A malformed bag is skipped, not fatal — list stays useful.
			continue
		}
		if err := agentlaunch.ValidateMinimumConfig(spec, bag); err != nil {
			continue
		}
		cat.bags[bag.Name] = bag
		cat.Specs = append(cat.Specs, bagToSpec(spec, bag))
	}
	sort.Slice(cat.Specs, func(i, j int) bool { return cat.Specs[i].ID < cat.Specs[j].ID })

	// Ingest the runtime-binding corpus: providers/ maps to the
	// runtime-binding kind under the go-agent-launch file-backed registrar
	// (see CatalogSubdirKind). ResolveRunner resolves `runner` tokens
	// against this registrar via agentlaunch.ResolveRuntimeBinding.
	if err := cat.ingestRuntimeBindings(root); err != nil {
		return nil, err
	}

	keep = true
	cat.cleanup = cleanup
	return cat, nil
}

// ingestRuntimeBindings walks the corpus providers/ subdir with the
// go-agent-launch file-backed registrar, populating the catalog's
// runtime-binding registrar and the sorted runner-token set.
func (c *Catalog) ingestRuntimeBindings(root string) error {
	// providers/ must map to the runtime-binding kind — assert the
	// go-agent-launch file-backed catalog contract holds.
	if kind, ok := agentlaunch.CatalogSubdirKind(corpus.ProvidersSubdir); !ok ||
		kind != agentlaunch.RegistryKindRuntimeBinding {
		return fmt.Errorf("corpus: %q is not the runtime-binding catalog subdir", corpus.ProvidersSubdir)
	}

	fbr := agentlaunch.NewFileBackedRegistrar(root)
	report, err := fbr.IngestCatalog()
	if err != nil {
		return fmt.Errorf("ingest runtime-binding corpus: %w", err)
	}
	if report.RegisteredByKind[agentlaunch.RegistryKindRuntimeBinding] == 0 {
		return fmt.Errorf("corpus: no runtime-binding contracts registered from %q", corpus.ProvidersSubdir)
	}

	c.registrar = fbr.Registrar()
	c.registrarDesc = agentlaunch.RegistryRegistrar{
		Mode:     agentlaunch.RegistrarModeFileBacked,
		FileRoot: root,
	}
	for _, rec := range report.Records {
		if rec.Meta.Ref.Kind == agentlaunch.RegistryKindRuntimeBinding {
			c.runners = append(c.runners, rec.Meta.Ref.Name)
		}
	}
	return nil
}

// Close frees the materialized corpus root. It is safe to call on a nil
// or already-closed Catalog. The engine is a short-lived CLI process, so
// callers may also rely on process exit to reclaim the temp dir.
func (c *Catalog) Close() {
	if c != nil && c.cleanup != nil {
		c.cleanup()
		c.cleanup = nil
	}
}

// Bag returns the launch bag with the given id (its Name) and whether it
// was found.
func (c *Catalog) Bag(id string) (agentlaunch.LaunchBag, bool) {
	b, ok := c.bags[id]
	return b, ok
}

// Filter returns the subset of Specs matching every supplied facet. An
// empty facet map matches everything.
func (c *Catalog) Filter(facets map[string]string) []Spec {
	if len(facets) == 0 {
		return append([]Spec(nil), c.Specs...)
	}
	var out []Spec
	for _, s := range c.Specs {
		if matchesFacets(s, facets) {
			out = append(out, s)
		}
	}
	return out
}

// matchesFacets reports whether spec satisfies every facet filter.
func matchesFacets(s Spec, facets map[string]string) bool {
	for k, v := range facets {
		if s.Facets[k] != v {
			return false
		}
	}
	return true
}

// bagToSpec flattens a launch bag into the list-contract Spec shape,
// resolving facet values through the LaunchSpec's declared input
// defaults so a minimum-config bag still surfaces a project/role.
func bagToSpec(spec agentlaunch.LaunchSpec, bag agentlaunch.LaunchBag) Spec {
	project := inputString(spec, bag, "project")
	role := inputString(spec, bag, "agent_role")
	runner := inputString(spec, bag, LaunchInputRunner)
	display := inputString(spec, bag, "display_name")
	if display == "" {
		display = bag.Name
	}
	return Spec{
		ID:      bag.Name,
		Name:    display,
		Project: project,
		Role:    role,
		Summary: display,
		Facets: map[string]string{
			"project": project,
			"role":    role,
			"runner":  runner,
		},
	}
}

// inputString resolves an input value for a bag: the bag value wins,
// otherwise the LaunchSpec's declared default, otherwise "".
func inputString(spec agentlaunch.LaunchSpec, bag agentlaunch.LaunchBag, name string) string {
	if v, ok := bag.Inputs[name]; ok {
		return toStr(v)
	}
	for i := range spec.Inputs {
		if spec.Inputs[i].Name == name {
			return toStr(spec.Inputs[i].Default)
		}
	}
	return ""
}

func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// joinCorpus joins a corpus-relative path onto the materialized root.
func joinCorpus(root, rel string) string {
	return root + "/" + rel
}

// LaunchInputRunner and LaunchInputWorkDir re-export the go-agent-launch
// launch-input names so the rest of this package does not import
// agentlaunch just for the constants.
const (
	LaunchInputRunner  = agentlaunch.LaunchInputRunner
	LaunchInputWorkDir = agentlaunch.LaunchInputWorkDir
)
