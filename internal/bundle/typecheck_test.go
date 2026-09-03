package bundle_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestProfileAndRoleProseAreNotInterchangeable is the compile-time half of the
// distinction. Every one of the eight role prose files in the live bundle
// shares a basename with a profile, so no name-derived check can tell them
// apart — the type system has to. This builds testdata/typecheck/mixup.go and
// fails if the compiler accepts it.
func TestProfileAndRoleProseAreNotInterchangeable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}
	out, err := exec.Command("go", "build", "-o", "/dev/null", "testdata/typecheck/mixup.go").CombinedOutput()
	if err == nil {
		t.Fatal("testdata/typecheck/mixup.go compiled; a RoleProse can be passed where a Profile is wanted")
	}
	text := string(out)
	for _, want := range []string{
		"cannot use prose",
		"bundle.RoleProse",
		"bundle.Profile",
		"bundle.RoleProseID",
		"bundle.ProfileID",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the build failed, but not for the expected reason (%q missing):\n%s", want, text)
		}
	}
}
