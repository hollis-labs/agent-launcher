package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/config"
	"github.com/hollis-labs/tachyon/internal/state"
)

func main() {
	os.Exit(mainRun(os.Args[1:], os.Stdout, os.Stderr))
}

// mainRun is main's body, taking args and its two streams explicitly so a
// test can drive it without touching the real os.Args or process streams.
// It returns the process exit code rather than calling os.Exit itself.
func mainRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tachyon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bundleFlag := fs.String("bundle", "", "bundle root to boot from (default: the app's own active bundle, same as bundle.DefaultRootStore resolves)")
	bootRootFlag := fs.String("boot-root", "", "where boot directories are planted (default: internal/state.BootRoot(), never ~/dev/agent-os — see D9)")
	scopeFlag := fs.String("scope", "", "override the binding's own scope (cairn boot --scope)")
	providerFlag := fs.String("provider", "", "the harness to materialize into (cairn boot --provider); empty renders whatever the resolved profile declares")
	cairnFlag := fs.String("cairn", "", "path to the cairn binary (default: internal/config's cairnPath, then PATH)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: tachyon [flags] <target>\n\n")
		fmt.Fprintf(stderr, "Flags must come before <target> -- this is the standard library's flag\n")
		fmt.Fprintf(stderr, "package, which stops parsing flags at the first non-flag argument.\n\n")
		fmt.Fprintf(stderr, "Resolves <target> (a saved binding's name, or a bare profile id) into a\n")
		fmt.Fprintf(stderr, "composition, runs the real `cairn boot --json`, and prints the invocation\n")
		fmt.Fprintf(stderr, "and the resulting harness argv. Spawns nothing else.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	target := fs.Arg(0)

	bundleRoot := *bundleFlag
	if bundleRoot == "" {
		store, err := bundle.DefaultRootStore()
		if err != nil {
			fmt.Fprintf(stderr, "tachyon: resolving default bundle store: %v\n", err)
			return 1
		}
		bundleRoot, err = store.Resolve()
		if err != nil {
			fmt.Fprintf(stderr, "tachyon: resolving active bundle root: %v\n", err)
			return 1
		}
	}

	bootRoot := *bootRootFlag
	if bootRoot == "" {
		root, err := state.BootRoot()
		if err != nil {
			fmt.Fprintf(stderr, "tachyon: resolving boot root: %v\n", err)
			return 1
		}
		bootRoot = root
	}

	cairnPath := *cairnFlag
	if cairnPath == "" {
		p, err := config.ResolveCairnPath()
		if err != nil {
			fmt.Fprintf(stderr, "tachyon: %v (pass --cairn to point at one explicitly)\n", err)
			return 1
		}
		cairnPath = p
	}

	cfg := Config{
		Target:   target,
		Bundle:   bundleRoot,
		BootRoot: bootRoot,
		Scope:    *scopeFlag,
		Provider: *providerFlag,
	}
	runner := boot.ExecRunner(cairnPath)

	report, err := Run(context.Background(), cfg, runner)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	printReport(stdout, cairnPath, report)

	if report.InvokeErr != nil {
		return 1
	}
	return 0
}

// printReport writes report in a human-readable form answering the four
// questions this command exists to answer (see the package doc): the
// boot-root and --settings hazards are both printed plainly, by name, so
// they are inspectable by eye rather than by grepping a data structure.
//
// This has no stability promise — see the package doc's "not a product
// contract" section. Its shape may change freely.
func printReport(w io.Writer, cairnPath string, r Report) {
	fmt.Fprintf(w, "target:    %s\n", r.Composition.Target)
	fmt.Fprintf(w, "bundle:    %s\n", r.Composition.Bundle)
	fmt.Fprintf(w, "boot-root: %s\n", r.Composition.BootRoot)
	if r.Composition.Scope != "" {
		fmt.Fprintf(w, "scope (--scope override): %s\n", r.Composition.Scope)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "cairn invocation:\n  %s %s\n", cairnPath, strings.Join(r.Argv, " "))
	fmt.Fprintf(w, "boot directory (expected, from boot.Key/CurrentPath): %s\n", r.ExpectedBootDir)
	fmt.Fprintln(w)

	if r.InvokeErr != nil {
		fmt.Fprintf(w, "cairn boot FAILED: %v\n", r.InvokeErr)
		if len(r.Stderr) > 0 {
			fmt.Fprintf(w, "cairn stderr:\n%s\n", r.Stderr)
		}
		return
	}

	fmt.Fprintf(w, "cairn --json reported:\n")
	fmt.Fprintf(w, "  boot_dir:        %s\n", r.Result.BootDir)
	fmt.Fprintf(w, "  provider:        %s\n", r.Result.Provider)
	fmt.Fprintf(w, "  scope:           %s\n", derefOrNone(r.Result.Scope))
	fmt.Fprintf(w, "  settings_path:   %s\n", derefOrNone(r.Result.SettingsPath))
	fmt.Fprintf(w, "  cwd_preference:  %s\n", r.Result.CwdPreference)
	fmt.Fprintf(w, "  project_dir_arg: %s\n", argvOrNone(r.Result.ProjectDirArg))
	fmt.Fprintf(w, "  env_amendments:  %s\n", argvOrNone(r.Result.EnvAmendments))
	fmt.Fprintf(w, "  home_resource_paths: %s\n", argvOrNone(r.Result.HomeResourcePaths))
	if r.Result.BootDir != r.ExpectedBootDir {
		fmt.Fprintf(w, "  WARNING: boot_dir does not match the expected boot directory above — boot.Key/CurrentPath has drifted from what cairn actually planted\n")
	}
	fmt.Fprintln(w)

	if r.HarnessArgvErr != nil {
		fmt.Fprintf(w, "harness argv FAILED: %v\n", r.HarnessArgvErr)
	} else {
		fmt.Fprintf(w, "harness argv: %s\n", strings.Join(r.HarnessArgv, " "))
		fmt.Fprintf(w, "--settings present: %v\n", argvHasFlag(r.HarnessArgv, "--settings"))
	}

	if r.EnvErr != nil {
		fmt.Fprintf(w, "environment FAILED: %v\n", r.EnvErr)
	} else {
		fmt.Fprintf(w, "environment added to the terminal command: %s\n", argvOrNone(r.Env))
	}

	switch {
	case r.ProviderHomeErr != nil:
		fmt.Fprintf(w, "provider home FAILED: %v\n", r.ProviderHomeErr)
	case r.ProviderHome != "":
		fmt.Fprintf(w, "provider home resources would be linked from: %s\n", r.ProviderHome)
	}
}

// derefOrNone renders one of Result's nil-able string fields: the string it
// points to, or the literal "(none)" for nil — Result's own contract for
// "cairn had nothing to say" (see internal/boot's doc on Scope,
// SettingsPath and ProjectDirArg).
func derefOrNone(s *string) string {
	if s == nil {
		return "(none)"
	}
	return *s
}

// argvOrNone renders one of Result's nil-able string-slice fields — its
// elements space-joined, or "(none)" for a nil/empty slice, which is
// Cairn's own spelling of an absent value (per internal/boot's doc on
// ProjectDirArg, EnvAmendments and HomeResourcePaths).
func argvOrNone(argv []string) string {
	if len(argv) == 0 {
		return "(none)"
	}
	return strings.Join(argv, " ")
}
