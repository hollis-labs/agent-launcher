package bundle_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestProfileAndTemplateAreNotInterchangeable is the compile-time half of
// the distinction. A template shares a basename with a profile as readily as
// role prose used to, so no name-derived check can tell them apart — the type
// system has to. This builds testdata/typecheck/mixup.go and fails if the
// compiler accepts it.
func TestProfileAndTemplateAreNotInterchangeable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}
	out, err := exec.Command("go", "build", "-o", "/dev/null", "testdata/typecheck/mixup.go").CombinedOutput()
	if err == nil {
		t.Fatal("testdata/typecheck/mixup.go compiled; a Template can be passed where a Profile is wanted")
	}
	text := string(out)
	for _, want := range []string{
		"cannot use tpl",
		"bundle.Template",
		"bundle.Profile",
		"bundle.TemplateID",
		"bundle.ProfileID",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the build failed, but not for the expected reason (%q missing):\n%s", want, text)
		}
	}
}
