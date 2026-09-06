package boot_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/boot"
)

// TestEnvironment_SubstitutesTheRealCodexAmendment is the whole point of
// this function against the actual document Cairn prints: Cairn leaves
// {{.BootDir}} standing because it launches nothing, and Tachyon — which
// does — is what substitutes it.
func TestEnvironment_SubstitutesTheRealCodexAmendment(t *testing.T) {
	result := mustDecode(t, realCodexBootReportFixture)

	env, err := boot.Environment(result)
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	want := []string{"CODEX_HOME=/tmp/tachyon-t14-scratch-boot/codex-coord-agent-setup/current"}
	if !slices.Equal(env, want) {
		t.Fatalf("Environment = %v; want %v", env, want)
	}
	if strings.Contains(env[0], "{{") {
		t.Fatalf("Environment left a placeholder standing: %q", env[0])
	}
}

// TestEnvironment_NilForClaude: no amendments means nil, not an empty
// slice — the shape internal/boot.SpawnITerm2 turns into byte-for-byte the
// command line it built before Codex existed.
func TestEnvironment_NilForClaude(t *testing.T) {
	env, err := boot.Environment(mustDecode(t, realClaudeBootReportFixture))
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if env != nil {
		t.Fatalf("Environment = %v; want nil for a provider that declares no amendments", env)
	}
}

// TestEnvironment_SubstitutesProjectDirToo covers the second placeholder
// Cairn's report documents, which no implemented provider uses in an
// amendment today. It is here so that the day one does, this function is
// already right rather than silently exporting a template.
func TestEnvironment_SubstitutesProjectDirToo(t *testing.T) {
	scope := "/Users/chrispian/dev/projects/agent setup"
	env, err := boot.Environment(boot.Result{
		BootDir:       "/state/boot/x/current",
		Scope:         &scope,
		EnvAmendments: []string{"SOME_HOME=" + boot.BootDirPlaceholder, "SOME_PROJECT=" + boot.ProjectDirPlaceholder},
	})
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	want := []string{"SOME_HOME=/state/boot/x/current", "SOME_PROJECT=/Users/chrispian/dev/projects/agent setup"}
	if !slices.Equal(env, want) {
		t.Fatalf("Environment = %v; want %v", env, want)
	}
}

// TestEnvironment_ProjectDirWithNoScopeIsARefusal follows
// [boot.Result.ProjectDirArgv]'s own rule one layer over: an amendment
// naming a directory Cairn resolved none for has no correct value, and ""
// is the one value that would look like an answer.
func TestEnvironment_ProjectDirWithNoScopeIsARefusal(t *testing.T) {
	_, err := boot.Environment(boot.Result{
		BootDir:       "/state/boot/x/current",
		Scope:         nil,
		EnvAmendments: []string{"SOME_PROJECT=" + boot.ProjectDirPlaceholder},
	})
	if err == nil {
		t.Fatal("Environment substituted a nil scope instead of refusing")
	}
}

// TestEnvironment_RefusesAPlaceholderItDoesNotKnow is the guard against the
// quietest possible failure here: an amendment carrying a placeholder
// Tachyon has never heard of would not fail on export. It would point the
// harness at a directory literally named "{{.Whatever}}", and the operator
// would get a session with no credentials and nothing anywhere saying why.
func TestEnvironment_RefusesAPlaceholderItDoesNotKnow(t *testing.T) {
	_, err := boot.Environment(boot.Result{
		BootDir:       "/state/boot/x/current",
		EnvAmendments: []string{"CODEX_HOME={{.ProviderHome}}"},
	})
	if err == nil {
		t.Fatal("Environment exported an unknown placeholder literally instead of refusing")
	}
	if !strings.Contains(err.Error(), "{{.ProviderHome}}") {
		t.Errorf("refusal does not name the placeholder it could not expand: %v", err)
	}
}

// TestEnvironment_RefusesAMalformedAmendment: Cairn builds these from a
// provider adapter's own declaration, so a malformed one means the adapter
// changed shape underneath both of us — which is exactly when a launcher
// must stop rather than improvise.
func TestEnvironment_RefusesAMalformedAmendment(t *testing.T) {
	cases := []struct {
		name      string
		amendment string
	}{
		{name: "no equals sign at all", amendment: "CODEX_HOME"},
		{name: "empty key", amendment: "=/tmp"},
		{name: "key with a space", amendment: "CODEX HOME=/tmp"},
		{name: "key starting with a digit", amendment: "1CODEX=/tmp"},
		{name: "key with a hyphen", amendment: "CODEX-HOME=/tmp"},
		{name: "a whole command as the key", amendment: "x; rm -rf ~=/tmp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, err := boot.Environment(boot.Result{BootDir: "/b", EnvAmendments: []string{tc.amendment}})
			if err == nil {
				t.Fatalf("Environment accepted %q, returning %v", tc.amendment, env)
			}
		})
	}
}

// --- provider home resolution ---------------------------------------------

// TestHomeRedirectKey_ReadsTheKeyOffTheAmendments proves the variable's
// spelling stays Cairn's: nothing in this package hardcodes "CODEX_HOME"
// except the one default path it has to know, and this is read off the
// report.
func TestHomeRedirectKey_ReadsTheKeyOffTheAmendments(t *testing.T) {
	key, err := boot.HomeRedirectKey(mustDecode(t, realCodexBootReportFixture))
	if err != nil {
		t.Fatalf("HomeRedirectKey: %v", err)
	}
	if key != "CODEX_HOME" {
		t.Fatalf("HomeRedirectKey = %q; want CODEX_HOME", key)
	}
}

// TestHomeRedirectKey_EmptyWhenNoHomeIsRedirected: Claude Code amends
// nothing, so nothing is redirected and there is no source home to resolve.
func TestHomeRedirectKey_EmptyWhenNoHomeIsRedirected(t *testing.T) {
	key, err := boot.HomeRedirectKey(mustDecode(t, realClaudeBootReportFixture))
	if err != nil {
		t.Fatalf("HomeRedirectKey: %v", err)
	}
	if key != "" {
		t.Fatalf("HomeRedirectKey = %q; want empty for claude", key)
	}
}

// TestHomeRedirectKey_OnlyAmendmentsThatNameTheBootDirectory: an amendment
// whose value is something other than the boot-dir placeholder is not a
// home redirect and must not be mistaken for one.
func TestHomeRedirectKey_OnlyAmendmentsThatNameTheBootDirectory(t *testing.T) {
	key, err := boot.HomeRedirectKey(boot.Result{
		EnvAmendments: []string{"SOME_FLAG=1", "SOME_PATH=" + boot.BootDirPlaceholder + "/sub"},
	})
	if err != nil {
		t.Fatalf("HomeRedirectKey: %v", err)
	}
	if key != "" {
		t.Fatalf("HomeRedirectKey = %q; want empty — neither amendment names the boot directory itself", key)
	}
}

// TestHomeRedirectKey_TwoRedirectsIsARefusal: picking the first would be a
// guess about which one owns home_resource_paths.
func TestHomeRedirectKey_TwoRedirectsIsARefusal(t *testing.T) {
	_, err := boot.HomeRedirectKey(boot.Result{
		EnvAmendments: []string{"CODEX_HOME=" + boot.BootDirPlaceholder, "OTHER_HOME=" + boot.BootDirPlaceholder},
	})
	if err == nil {
		t.Fatal("HomeRedirectKey chose between two redirected homes instead of refusing")
	}
	if !errors.Is(err, boot.ErrHomeResource) {
		t.Errorf("error does not wrap ErrHomeResource: %v", err)
	}
}

// TestResolveHome_PrefersTheOperatorsOwnExportedValue: the operator's own
// environment wins over Tachyon's default, and it is read here — before
// anything amends it.
func TestResolveHome_PrefersTheOperatorsOwnExportedValue(t *testing.T) {
	t.Setenv("CODEX_HOME", "/Users/chrispian/elsewhere/.codex")
	got, err := boot.ResolveHome("CODEX_HOME")
	if err != nil {
		t.Fatalf("ResolveHome: %v", err)
	}
	if got != "/Users/chrispian/elsewhere/.codex" {
		t.Fatalf("ResolveHome = %q; want the exported value", got)
	}
}

// TestResolveHome_FallsBackToThisMachinesDefault covers the ordinary case:
// a GUI Tachyon started by LaunchServices inherits launchd's environment,
// which does not carry CODEX_HOME at all.
func TestResolveHome_FallsBackToThisMachinesDefault(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	got, err := boot.ResolveHome("CODEX_HOME")
	if err != nil {
		t.Fatalf("ResolveHome: %v", err)
	}
	if !strings.HasSuffix(got, "/.codex") {
		t.Fatalf("ResolveHome = %q; want a path ending in /.codex", got)
	}
}

// TestResolveHome_EmptyKeyResolvesNothing: Claude's case, and it must not
// invent a home for a provider that redirects none.
func TestResolveHome_EmptyKeyResolvesNothing(t *testing.T) {
	got, err := boot.ResolveHome("")
	if err != nil || got != "" {
		t.Fatalf("ResolveHome(\"\") = %q, %v; want \"\", nil", got, err)
	}
}

// TestResolveHome_UnknownVariableIsARefusal: falling back to the boot
// directory here would produce exactly the self-link the whole ordering of
// this function exists to make impossible.
func TestResolveHome_UnknownVariableIsARefusal(t *testing.T) {
	t.Setenv("SOME_FUTURE_HOME", "")
	_, err := boot.ResolveHome("SOME_FUTURE_HOME")
	if err == nil {
		t.Fatal("ResolveHome invented a default for a provider tachyon knows nothing about")
	}
	if !strings.Contains(err.Error(), "SOME_FUTURE_HOME") {
		t.Errorf("refusal does not name the variable: %v", err)
	}
}
