package bindingcomposer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerComposerUsesSharedTabCompletion(t *testing.T) {
	source := readFrontendSource(t, "BindingComposer.jsx")
	for _, required := range []string{
		`import { acceptTopSuggestion } from "./autocomplete.js"`,
		`acceptTopSuggestion(e, draft, suggestions, setDraft)`,
		`acceptTopSuggestion(e, profile, profiles, setProfile)`,
		`search: [view.project.name]`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("BindingComposer.jsx is missing Tab-completion contract %q", required)
		}
	}
}

func TestScrollbarChromeIsHiddenAppWide(t *testing.T) {
	styles := readFrontendSource(t, "styles.css")
	for _, required := range []string{
		"scrollbar-width: none",
		"*::-webkit-scrollbar",
		"display: none",
	} {
		if !strings.Contains(styles, required) {
			t.Errorf("styles.css is missing app-wide hidden-scrollbar rule %q", required)
		}
	}
}

func readFrontendSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "frontend", "src", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
