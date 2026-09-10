package compose

import (
	"errors"
	"strings"
)

// Set is one one-off slot override — Composition.Sets renders each as its
// own --set <Slot>=<Value> flag, in the order given.
type Set struct {
	Slot  string
	Value string
}

// Composition is the palette's composition state, shaped to match the
// mapping table in the package doc. There is deliberately no field for the
// template control: a template is authoring-time only (D4) and contributes
// nothing to argv.
type Composition struct {
	// Target is the boot target: a saved binding's name or a bare profile
	// id. It becomes the positional argument to `cairn boot`. Required.
	Target string

	// Bundle is the active bundle root. It always becomes --profile.
	// Required.
	Bundle string

	// There is deliberately no Provider field.
	//
	// A provider used to render as --provider, defaulting to whatever the
	// resolved profile declared. No profile in agent-setup declares one
	// since 2026-09-10 — a runtime is a launch's to choose, not an agent's
	// to carry — so that default resolves to nothing and Cairn refuses the
	// render rather than writing one harness's files into another's
	// directory.
	//
	// The provider now comes from a launch profile in [Composition.Parts]:
	// an ordinary Cairn part, declaring `provider:` in its frontmatter,
	// which Cairn folds in through the same cascade as any other profile.
	// So the provider is still composed, one layer further out, and it is
	// still never inferred: it is DECLARED, in a file, by the launcher that
	// owns the launch. A flag here would be a second source for one value.
	//
	// What that costs, named so it is not rediscovered: a composition with
	// no launch profile in Parts has no provider at all, and Cairn refuses
	// it. That is the intended shape (see internal/launchprofile, and the
	// seeds it plants so a first run has one) rather than an omission.

	// BootRoot becomes --boot-root's value, unchanged. Required, and never
	// defaulted — see the package doc's hazard section (D9). A caller
	// computes this under Tachyon's own state directory; this package only
	// refuses to proceed without it.
	BootRoot string

	// Skills renders as one comma-joined --skill flag, in the order given.
	// Omitted entirely when empty.
	Skills []string

	// Prompts renders as one comma-joined --prompt flag, in the order
	// given — the exact same shape Skills renders as, matching --prompt's
	// own --help text: "Comma-separated and repeatable, the two forms
	// equivalent and composing. Additive only, for the reason --skill is."
	// Omitted entirely when empty. There is deliberately no field here for
	// a prompt's own content — Tachyon hands Cairn a name, never bytes; see
	// this package's own doc, "no delivery."
	Prompts []string

	// Parts are the additional pieces layered over what the user authored.
	// Each becomes its own --with flag, in order.
	Parts []string

	// Sets are one-off slot overrides. Each becomes its own --set flag, in
	// order.
	Sets []Set

	// Scope is the project/path directory. Empty omits --scope.
	//
	// It is the launch's, and it can only arrive here: Cairn refuses
	// `scope:` as frontmatter — it is not one of the eight keys — so a
	// launch profile cannot carry one and this flag is the only source.
	// Tachyon fills it from the project selected beside the launch profile,
	// and leaves it empty when none is, which Cairn accepts and which is
	// right for a profile that holds no scope of its own.
	Scope string

	// Session is the --session segment: the leaf of the directory Cairn
	// plants at, <boot-root>/<target>/<session>. Empty omits the flag,
	// which lets Cairn choose its own (a timestamp and a random suffix).
	//
	// A caller that wants a STABLE boot directory must supply one, and
	// Tachyon does — see internal/boot.SessionKey. Cairn's own default
	// varies per launch, and every boot directory a harness opens leaves a
	// permanent trust entry in ~/.claude.json keyed to its path.
	Session string
}

// Sentinel errors for the fields Build refuses to proceed without. Test with
// errors.Is.
var (
	// ErrNoTarget is returned when Composition.Target is empty: there is
	// nothing for `cairn boot` to boot.
	ErrNoTarget = errors.New("compose: target is required")
	// ErrNoBundle is returned when Composition.Bundle is empty: --profile
	// must always name the active bundle root (see the package doc).
	ErrNoBundle = errors.New("compose: bundle is required")
	// ErrNoBootRoot is returned when Composition.BootRoot is empty. Build
	// does not default this — see D9 in the package doc. A generated argv
	// missing --boot-root would silently depend on whatever
	// CAIRN_BOOT_ROOT happens to be set to in the process that runs it.
	ErrNoBootRoot = errors.New("compose: boot root is required")
)

// Arguments renders the portion of a Cairn composition invocation shared by
// `cairn boot` and `cairn show`:
//
//	<target> --profile <bundle> [--with <part>]...
//	  [--skill <a,b,c>] [--prompt <a,b,c>]
//	  [--set <slot>=<value>]... [--scope <path>]
//
// This is the one canonical encoder for composition options. Callers add only
// their subcommand-specific flags around it: Build adds boot-root/session and
// both callers add --json. BootRoot is intentionally not validated here,
// because `show` must never receive it.
func Arguments(c Composition) ([]string, error) {
	switch {
	case c.Target == "":
		return nil, ErrNoTarget
	case c.Bundle == "":
		return nil, ErrNoBundle
	}

	args := []string{c.Target, "--profile", c.Bundle}

	for _, part := range c.Parts {
		args = append(args, "--with", part)
	}

	if len(c.Skills) > 0 {
		args = append(args, "--skill", strings.Join(c.Skills, ","))
	}

	if len(c.Prompts) > 0 {
		args = append(args, "--prompt", strings.Join(c.Prompts, ","))
	}

	for _, s := range c.Sets {
		args = append(args, "--set", s.Slot+"="+s.Value)
	}

	if c.Scope != "" {
		args = append(args, "--scope", c.Scope)
	}

	return args, nil
}

// Build renders c as the argv for `cairn boot`, in the fixed order:
//
//	boot <target> --profile <bundle> --boot-root <root> [--session <name>]
//	     [--with <part>]... [--skill <a,b,c>] [--prompt <a,b,c>]
//	     [--set <slot>=<value>]... [--scope <path>] --json
//
// It does not include the program name ("cairn") itself — only the
// arguments a caller passes to whatever runs that binary, which is a
// different package's concern (T10).
//
// Build returns an error, and no argv, rather than produce an invocation
// missing Target, Bundle or BootRoot. Every other field is optional and
// simply omitted from argv when empty or nil.
func Build(c Composition) ([]string, error) {
	common, err := Arguments(c)
	if err != nil {
		return nil, err
	}
	if c.BootRoot == "" {
		return nil, ErrNoBootRoot
	}

	args := []string{
		"boot", common[0],
		common[1], common[2],
		"--boot-root", c.BootRoot,
	}
	if c.Session != "" {
		args = append(args, "--session", c.Session)
	}
	args = append(args, common[3:]...)
	args = append(args, "--json")

	return args, nil
}
