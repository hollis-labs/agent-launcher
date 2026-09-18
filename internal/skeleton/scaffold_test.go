package skeleton_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/skeleton"
)

// cairnMarkerPattern is a verbatim copy of markerPattern from
// github.com/chrispian/cairn/bootdir/template.go, the RETIRED marker
// engine's own pattern:
//
//	var markerPattern = regexp.MustCompile(`<!--\s*cairn:(.*?)-->`)
//
// It is kept as a negative guard rather than deleted. Three tests here used
// to assert the template scaffold placed exactly two markers, in both line
// forms, naming six instance facts. agent-setup moved off that engine on
// 2026-09-10 -- profiles became templates in their own right, with
// `{{ extends }}` / `{{ section }}` / `{{ yield }}` in the profile and
// `{{ file: ... }}` naming a template -- so a scaffold placing a marker now
// hands someone a document the live bundle has no reader for.
//
// Tachyon has no dependency on Cairn (D3, and the plan's "zero Cairn
// dependency" fence), so this is copied rather than imported.
var cairnMarkerPattern = regexp.MustCompile(`<!--\s*cairn:(.*?)-->`)

// TestScaffoldsPlaceNoRetiredMarkers is the negative form of the three
// marker tests it replaced: no scaffold, for any kind, may emit something
// cairn's old marker pattern would match.
func TestScaffoldsPlaceNoRetiredMarkers(t *testing.T) {
	for _, kind := range skeleton.SupportedKinds() {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			root := t.TempDir()
			if _, err := skeleton.New(root, skeleton.Spec{Kind: kind, ID: "markertest"}); err != nil {
				t.Fatalf("New(%s): %v", kind, err)
			}
			var found []string
			err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				found = append(found, cairnMarkerPattern.FindAllString(string(data), -1)...)
				return nil
			})
			if err != nil {
				t.Fatalf("walking the generated tree: %v", err)
			}
			if len(found) > 0 {
				t.Errorf("the %s scaffold places retired cairn markers: %v", kind, found)
			}
		})
	}
}

// TestTemplateScaffoldPointsAtTheEngineThatIsRunning is the positive half:
// the scaffold has to say where the directives actually live, or someone
// writes a template full of syntax that renders verbatim.
func TestTemplateScaffoldPointsAtTheEngineThatIsRunning(t *testing.T) {
	root := t.TempDir()
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindTemplate, ID: "engine"}); err != nil {
		t.Fatalf("New: %v", err)
	}
	content := readFile(t, root, "templates", "engine.md")

	for _, want := range []string{"{{ section", "{{ file:", "{{ end }}", "profiles/base.md"} {
		if !strings.Contains(content, want) {
			t.Errorf("template scaffold does not mention %q:\n%s", want, content)
		}
	}
	// And it must say a subdirectory is allowed, since that is the whole
	// reason a template id carries a path.
	if !strings.Contains(content, "Subdirectories") {
		t.Errorf("template scaffold does not mention that subdirectories are allowed:\n%s", content)
	}
}

// TestProfileScaffoldLeavesAnOrdinaryDescriptionUnquoted confirms an
// everyday description — no colon, no leading indicator character — renders
// the same unquoted way this bundle's real profiles already do, rather than
// always over-quoting.
func TestProfileScaffoldLeavesAnOrdinaryDescriptionUnquoted(t *testing.T) {
	root := t.TempDir()
	desc := "Reviews a diff with no shared context on how it was built."
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindProfile, ID: "plain", Description: desc}); err != nil {
		t.Fatalf("New: %v", err)
	}
	content := readFile(t, root, "profiles", "plain.md")
	if !strings.Contains(content, "description: "+desc+"\n") {
		t.Errorf("an ordinary description was quoted when it did not need to be:\n%s", content)
	}
}

// TestSkillScaffoldFrontmatterHasExactlyNameAndDescription confirms the
// scaffold carries only the two fields every one of the bundle's 17 real
// skills carries — no extra, no fewer.
func TestSkillScaffoldFrontmatterHasExactlyNameAndDescription(t *testing.T) {
	root := t.TempDir()
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindSkill, ID: "keys-test", Description: "x"}); err != nil {
		t.Fatalf("New: %v", err)
	}
	content := readFile(t, root, "skills", "keys-test", "SKILL.md")
	keys := frontmatterKeys(t, content)
	want := map[string]bool{"name": true, "description": true}
	if len(keys) != len(want) {
		t.Fatalf("skill scaffold frontmatter keys = %v; want exactly %v", keys, want)
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected frontmatter key %q in skill scaffold", k)
		}
	}
}

// frontmatterKeys does a minimal, test-only extraction of the top-level keys
// in a "---\n...\n---\n" frontmatter block, deliberately no more capable than
// that -- this package has no YAML dependency (matching internal/bundle's
// own deliberately shallow Header scan) and does not need one just to assert
// a key set in a test.
func frontmatterKeys(t *testing.T, content string) []string {
	t.Helper()
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || lines[0] != "---" {
		t.Fatalf("content has no frontmatter delimiter:\n%s", content)
	}
	var keys []string
	for _, line := range lines[1:] {
		if line == "---" {
			break
		}
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		keys = append(keys, line[:i])
	}
	return keys
}

func readFile(t *testing.T, elem ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(elem...))
	if err != nil {
		t.Fatalf("reading %v: %v", elem, err)
	}
	return string(data)
}
