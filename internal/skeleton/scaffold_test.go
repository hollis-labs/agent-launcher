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
// github.com/chrispian/cairn/bootdir/template.go, confirmed against that
// file directly while this scaffold was written (2026-09-03):
//
//	var markerPattern = regexp.MustCompile(`<!--\s*cairn:(.*?)-->`)
//
// Tachyon has no dependency on Cairn (D3, and the plan's "zero Cairn
// dependency" fence) so this cannot import it; it is copied here as a
// regression guard so this package's own scaffold can be checked against the
// exact rule that governs it in production, not against this package's own
// possibly-wrong idea of that rule.
var cairnMarkerPattern = regexp.MustCompile(`<!--\s*cairn:(.*?)-->`)

// TestTemplateScaffoldMarkersMatchCairnExactly proves two things at once
// about the template scaffold's big explanatory HTML comment: that it
// documents markers without becoming one (the comment mentions "cairn:slot"
// and "cairn:value" in prose, and a naive marker scanner could misfire on
// that), and that the two markers it actually places afterward — the slot
// example and the value example — are the only two things cairn's own
// pattern would find.
func TestTemplateScaffoldMarkersMatchCairnExactly(t *testing.T) {
	root := t.TempDir()
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindTemplate, ID: "markertest"}); err != nil {
		t.Fatalf("New: %v", err)
	}
	content := readFile(t, root, "templates", "markertest.md")

	matches := cairnMarkerPattern.FindAllString(content, -1)
	want := []string{"<!-- cairn:slot example -->", "<!-- cairn:value scope -->"}
	if len(matches) != len(want) {
		t.Fatalf("cairn's marker pattern found %d markers in the scaffold, want %d: %v", len(matches), len(want), matches)
	}
	for i, m := range matches {
		if m != want[i] {
			t.Errorf("marker %d = %q; want %q", i, m, want[i])
		}
	}
}

// TestTemplateScaffoldShowsBothMarkerLineForms confirms the scaffold
// literally shows both forms CW-20260903-0010 requires: a slot marker alone
// on its own line (which vanishes entirely, newline included, if the slot is
// never filled) and a value marker sharing its line with other content
// (which keeps the line, per templates/agents.md's own
// "- scope: <!-- cairn:value scope -->").
func TestTemplateScaffoldShowsBothMarkerLineForms(t *testing.T) {
	root := t.TempDir()
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindTemplate, ID: "linerules"}); err != nil {
		t.Fatalf("New: %v", err)
	}
	content := readFile(t, root, "templates", "linerules.md")

	lines := strings.Split(content, "\n")
	sawAloneMarker := false
	sawSharedLineMarker := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "<!-- cairn:slot example -->" {
			sawAloneMarker = true
		}
		if strings.HasPrefix(trimmed, "-") && strings.Contains(line, "<!-- cairn:value scope -->") && trimmed != "<!-- cairn:value scope -->" {
			sawSharedLineMarker = true
		}
	}
	if !sawAloneMarker {
		t.Error("no line has a cairn:slot marker alone on it")
	}
	if !sawSharedLineMarker {
		t.Error("no line shares a cairn:value marker with other content")
	}
	if !strings.Contains(content, "- scope: <!-- cairn:value scope -->") {
		t.Error(`scaffold does not contain the exact "- scope: <!-- cairn:value scope -->" example the task requires verbatim`)
	}
}

// TestTemplateScaffoldNamesAllSixInstanceFacts pins the six names
// individually, in the order cairn's ValueNames() returns them, so a future
// edit that drops or misorders one fails loudly here rather than only being
// noticed by a human rereading the comment.
func TestTemplateScaffoldNamesAllSixInstanceFacts(t *testing.T) {
	root := t.TempDir()
	if _, err := skeleton.New(root, skeleton.Spec{Kind: bundle.KindTemplate, ID: "sixnames"}); err != nil {
		t.Fatalf("New: %v", err)
	}
	content := readFile(t, root, "templates", "sixnames.md")
	want := []string{"binding", "model", "profile", "provider", "scope", "session"}
	for _, name := range want {
		if !strings.Contains(content, name) {
			t.Errorf("template scaffold does not mention instance fact %q", name)
		}
	}
	if !strings.Contains(content, strings.Join(want, ", ")) {
		t.Errorf("template scaffold does not list the six instance facts together, in cairn's own ValueNames() order")
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
