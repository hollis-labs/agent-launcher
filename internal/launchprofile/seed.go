package launchprofile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Scaffold is the starting text for a new launch profile: valid frontmatter
// Cairn will accept, a provider filled in, and comments saying what belongs
// here and what does not.
//
// The scaffold declares a provider and nothing else, because a provider is
// the one thing a launch profile MUST carry — no profile in agent-setup
// names one since 2026-09-10, and Cairn refuses to render without one
// rather than writing one harness's files into another's directory.
// Everything else is optional and the comments point at it.
func Scaffold(name, provider string) []byte {
	if provider == "" {
		provider = "claude"
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("id: " + name + "\n")
	b.WriteString("provider: " + provider + "\n")
	b.WriteString("---\n")
	b.WriteString(`
<!-- This is a Cairn part, not a format of Tachyon's own. Tachyon passes it
     to cairn as ` + "`--with <this file>`" + `, and cairn folds it in after the
     agent profile's own extends chain resolves — closest wins, so anything
     declared here overrides what the bundle declared.

     ` + "`provider`" + ` above is the one key that has to be here. No profile in
     agent-setup names a provider any more (a runtime is a launch's to
     choose, not an agent's to carry), and cairn refuses to render without
     one rather than guessing.

     What else belongs in a launch profile — everything about HOW this runs:

       spec:
         settings:
           claude:
             permissions: { defaultMode: auto }
           codex:
             approval_policy: never
             sandbox_mode: workspace-write
         skills: [some-skill]

     ` + "`spec.settings`" + ` is keyed by provider and cairn writes the matching
     document into the boot directory verbatim, beside the access grant it
     computes from the scope. One launch profile can carry settings for
     several providers; only the one being rendered contributes.

     What does NOT belong here:

       scope   cairn refuses it as frontmatter — it is not one of the eight
               keys — because a scope belongs to the launch, not the agent.
               Tachyon passes --scope from the project you pick beside this
               profile.
       model   it parses and renders nothing at boot (Torque
               CW-20260910-0071). Use spec.settings.<provider> instead.

     Preview any of this before launching:

       cairn show <agent-profile> --profile <bundle> --with <this file> --json

     which reports this file as a chain member and a settings contributor. -->
`)
	return []byte(b.String())
}

// seed is one profile [EnsureSeeds] plants on a store that has none.
type seed struct {
	name    string
	content string
}

// seeds are the launch profiles a fresh store starts with: one per provider
// Tachyon can launch, so that a first run has something that works.
//
// Neither seed invents a posture. `default` declares a provider and nothing
// else, so a Claude launch renders exactly the bytes it rendered before
// launch profiles existed — base.md's own settings, untouched. `codex`
// carries two keys and they are a RESTORATION rather than a new stance:
// agent-setup's profiles/parts/codex-cli.md carried them until it was
// removed at 55e980d, and they are pinned by the rendered config.toml backed
// up beside that removal (see Torque CW-20260910-0038).
//
// Why Codex needs them at all, per Tesseract codex_boot_sandbox_posture: a
// boot directory inherits none of ~/.codex's per-project trust once
// CODEX_HOME points at it, so cwd is a fresh directory nothing has trusted,
// the effective sandbox falls back to read-only, and Codex then refuses the
// `--add-dir <scope>` grant — additional roots are writable roots, and
// read-only allows none. sandbox_mode widens nothing that cairn had not
// already computed into writable_roots; it makes that grant take effect.
//
// The Claude side of the same posture is deliberately NOT seeded here.
// `approval_policy: never` is a general stance — do not prompt, fail rather
// than stop the session — and its Claude spelling would be
// permissions.defaultMode, which agent-setup currently renders as "auto".
// Changing that is a real change to how every Claude session behaves, and
// restoring a deleted Codex file is not. One of those is this seed's to make.
var seeds = []seed{
	{
		name: "default",
		content: `---
id: default
provider: claude
description: Claude, with the bundle's own settings
---

<!-- The provider, and deliberately nothing else. A Claude boot through this
     profile renders exactly what it rendered before launch profiles existed:
     agent-setup's base.md supplies the settings document, and this file
     contributes no key that could override it.

     Add spec.settings.claude here to change that. -->
`,
	},
	{
		name: "codex",
		content: `---
id: codex
provider: codex
description: Codex, with the sandbox posture a boot directory needs
spec:
  settings:
    codex:
      approval_policy: never
      sandbox_mode: workspace-write
---

<!-- The two keys are a restoration, not a new stance: agent-setup's
     profiles/parts/codex-cli.md carried them until it was removed, and
     without them a Codex session boots and then cannot write to its own
     scope.

     CODEX_HOME points at the boot directory, so Codex reads THAT config.toml
     and inherits none of ~/.codex's per-project trust. cwd is a fresh
     directory nothing has trusted, the effective sandbox falls back to
     read-only, and Codex refuses the --add-dir grant, because additional
     roots are writable roots and read-only allows none.

     sandbox_mode widens nothing. cairn already computes
     [sandbox_workspace_write] writable_roots from the scope and spec.access;
     this is what lets that grant take effect. -->
`,
	},
}

// EnsureSeeds plants the default launch profiles when the store holds none,
// reporting each on w. It is idempotent and safe on every startup.
//
// "Holds none" is the whole condition, and it is deliberately not per-file.
// A person who deletes `codex` because they do not use it should not find it
// back on the next launch — that is the store fighting them. A person who
// has never written one should not face an empty palette on a fresh machine.
// Only the empty case is unambiguous, so only the empty case is seeded.
func EnsureSeeds(st Store, w io.Writer) error {
	existing, err := st.List()
	switch {
	case err == nil:
		if len(existing) > 0 {
			return nil
		}
	case errors.Is(err, ErrDirMissing):
		// First run. Fall through and plant.
	default:
		return fmt.Errorf("launchprofile: checking %s before seeding: %w", st.Dir, err)
	}

	for _, s := range seeds {
		if _, err := st.Create(s.name, []byte(s.content)); err != nil {
			if errors.Is(err, ErrExists) {
				continue
			}
			return fmt.Errorf("launchprofile: seeding %s: %w", s.name, err)
		}
		if w != nil {
			_, _ = fmt.Fprintf(w, "tachyon: wrote a starting launch profile at %s\n", st.Dir+string(os.PathSeparator)+s.name+Ext)
		}
	}
	return nil
}
