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
	Scope string
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

// Build renders c as the argv for `cairn boot`, in the fixed order:
//
//	boot <target> --profile <bundle> --boot-root <root> --session current
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
	switch {
	case c.Target == "":
		return nil, ErrNoTarget
	case c.Bundle == "":
		return nil, ErrNoBundle
	case c.BootRoot == "":
		return nil, ErrNoBootRoot
	}

	args := []string{
		"boot", c.Target,
		"--profile", c.Bundle,
		"--boot-root", c.BootRoot,
		"--session", "current",
	}

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

	args = append(args, "--json")

	return args, nil
}
