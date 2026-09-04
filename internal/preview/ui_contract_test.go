package preview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBothCompositionSurfacesUseSharedPreviewPresentation(t *testing.T) {
	for _, name := range []string{"Palette.jsx", "BindingComposer.jsx"} {
		source := readFrontend(t, name)
		for _, required := range []string{
			`import EffectiveSkills from "./EffectiveSkills.jsx"`,
			`<EffectiveSkills input={previewInput} />`,
		} {
			if !strings.Contains(source, required) {
				t.Errorf("%s is missing shared effective-skills presentation %q", name, required)
			}
		}
		if strings.Contains(source, "contributors") {
			t.Errorf("%s interprets contributor labels; they belong only to Cairn's whole manifest key", name)
		}
	}

	presentation := readFrontend(t, "EffectiveSkills.jsx")
	for _, required := range []string{
		`CompositionPreview.Preview`,
		`Effective skills`,
		`read-only · resolved by Cairn`,
		`Launch remains available.`,
		`Cairn advisory:`,
		`window.addEventListener("blur", pause)`,
		`controller.current.dispose()`,
	} {
		if !strings.Contains(presentation, required) {
			t.Errorf("EffectiveSkills.jsx is missing %q", required)
		}
	}
}

func TestBridgePreservesRawCancellableWailsCall(t *testing.T) {
	bridge := readFrontend(t, "bridge.js")
	want := `Preview: (input) => callService(COMPOSITION_PREVIEW_SERVICE, "Preview", input)`
	if !strings.Contains(bridge, want) {
		t.Fatalf("preview bridge is missing raw Call.ByName path %q", want)
	}
	if strings.Contains(bridge, "async Preview") {
		t.Fatal("preview bridge wraps the cancellable Wails promise in async")
	}

	shell := readRepo(t, "internal", "shell", "shell.go")
	for _, required := range []string{
		`preview.NewService(rootStore, preview.Options{})`,
		`application.NewService(compositionPreview)`,
	} {
		if !strings.Contains(shell, required) {
			t.Errorf("shell does not register shared preview service: missing %q", required)
		}
	}
}

func readFrontend(t *testing.T, name string) string {
	t.Helper()
	return readRepo(t, "frontend", "src", name)
}

func readRepo(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
