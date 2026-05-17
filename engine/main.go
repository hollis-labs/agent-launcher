// Command tachyon-engine is the Go sidecar of Tachyon: the sole
// public-seam consumer of go-agent-launch. The Swift/AppKit app shells
// out to this binary and parses the JSON it emits on stdout.
//
// It implements the FROZEN engine⇄app CLI contract (see CONTRACT.md):
//
//	tachyon-engine list    [--facet KEY=VAL ...]
//	tachyon-engine describe --spec <id>
//	tachyon-engine launch   --spec <id> [--input KEY=VAL ...]
//	                                    [--on-error-choice retry|proceed_cached]
//
// All three subcommands are local-first (D1): the launch corpus is
// embedded in the binary, so the engine works fully offline with no
// directory service and no on-disk catalog.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hollis-labs/tachyon/engine/internal/launch"
)

// exit codes — the FROZEN contract's launch exit-code triple, applied
// uniformly across every subcommand.
const (
	exitReady    = 0 // success
	exitVarError = 2 // launch: a var_error the app must mediate
	exitFatal    = 1 // any fatal error
)

// stringSlice collects a repeatable string flag (--facet, --input).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: %s <list|describe|launch> [flags]", filepath.Base(os.Args[0]))
	}

	switch os.Args[1] {
	case "list":
		runList(os.Args[2:])
	case "describe":
		runDescribe(os.Args[2:])
	case "launch":
		runLaunch(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Fprintf(os.Stdout, "usage: %s <list|describe|launch> [flags]\n", filepath.Base(os.Args[0]))
	default:
		fatalf("unknown subcommand %q", os.Args[1])
	}
}

// runList implements `list [--facet KEY=VAL ...]`.
//
//	stdout: {"specs":[{"id","name","project","role","summary","facets":{...}}, ...]}
func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	// --catalog-root is accepted for forward-compatibility with a future
	// directory service. The engine has no directory client today, so an
	// absent or unreachable root transparently falls back to the embedded
	// corpus — local-first per D1 (see internal/launch.LoadCatalog).
	root := fs.String("catalog-root", defaultCatalogRoot(), "on-disk catalog root (falls back to the bundled corpus when unreachable)")
	var facets stringSlice
	fs.Var(&facets, "facet", "facet filter KEY=VAL (repeatable)")
	if err := fs.Parse(args); err != nil {
		os.Exit(exitFatal)
	}

	if !launch.CatalogRootUsable(expandHome(*root)) {
		fmt.Fprintf(os.Stderr, "tachyon-engine: catalog root %q unreachable; using bundled corpus (local-first)\n", *root)
	}

	cat, err := launch.LoadCatalog()
	if err != nil {
		fatalErr(err)
	}
	specs := cat.Filter(parsePairs(facets))
	if specs == nil {
		specs = []launch.Spec{}
	}
	emitJSON(map[string]any{"specs": specs})
}

// runDescribe implements `describe --spec <id>`.
//
//	stdout: {"id","name","inputs":[...],"runners":[...]}
func runDescribe(args []string) {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	spec := fs.String("spec", "", "launch-spec id (required)")
	root := fs.String("catalog-root", defaultCatalogRoot(), "on-disk catalog root (falls back to the bundled corpus when unreachable)")
	if err := fs.Parse(args); err != nil {
		os.Exit(exitFatal)
	}
	if strings.TrimSpace(*spec) == "" {
		fatalf("describe requires --spec <id>")
	}

	if !launch.CatalogRootUsable(expandHome(*root)) {
		fmt.Fprintf(os.Stderr, "tachyon-engine: catalog root %q unreachable; using bundled corpus (local-first)\n", *root)
	}

	cat, err := launch.LoadCatalog()
	if err != nil {
		fatalErr(err)
	}
	desc, err := launch.Describe(cat, *spec)
	if err != nil {
		fatalErr(err)
	}
	emitJSON(desc)
}

// runLaunch implements
// `launch --spec <id> [--input KEY=VAL ...] [--on-error-choice ...]`.
//
// It runs Compile → PrepareAndPlant → ToSessionLaunch and EMITS the
// runnable command — it never execs the agent (the Swift app spawns it
// in iTerm2).
//
//	stdout (success):   {"status":"ready","binary","args":[...],"env":{...},"workdir"}
//	stdout (var error): {"status":"var_error","var","message","options":[...]}
//	exit codes: 0 = ready, 2 = var_error, 1 = fatal
func runLaunch(args []string) {
	fs := flag.NewFlagSet("launch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	spec := fs.String("spec", "", "launch-spec id (required)")
	root := fs.String("catalog-root", defaultCatalogRoot(), "on-disk catalog root (falls back to the bundled corpus when unreachable)")
	onErr := fs.String("on-error-choice", "", "app-mediated var_error decision: retry|proceed_cached")
	var inputs stringSlice
	fs.Var(&inputs, "input", "launch input KEY=VAL (repeatable)")
	if err := fs.Parse(args); err != nil {
		os.Exit(exitFatal)
	}
	if strings.TrimSpace(*spec) == "" {
		fatalf("launch requires --spec <id>")
	}
	switch *onErr {
	case "", "retry", "proceed_cached":
		// ok — "" is the first call, the others are app re-invocations.
	default:
		fatalf("invalid --on-error-choice %q (want retry|proceed_cached)", *onErr)
	}

	if !launch.CatalogRootUsable(expandHome(*root)) {
		fmt.Fprintf(os.Stderr, "tachyon-engine: catalog root %q unreachable; using bundled corpus (local-first)\n", *root)
	}

	cat, err := launch.LoadCatalog()
	if err != nil {
		fatalErr(err)
	}

	ready, verr, err := launch.Launch(cat, launch.LaunchParams{
		ID:            *spec,
		Overrides:     parsePairs(inputs),
		OnErrorChoice: *onErr,
	})
	if err != nil {
		fatalErr(err)
	}
	if verr != nil {
		emitJSON(verr)
		os.Exit(exitVarError)
	}
	emitJSON(ready)
}

// defaultCatalogRoot is the conventional on-disk catalog location. The
// engine probes it only to decide whether to log a fallback notice; the
// embedded corpus is always the data source.
func defaultCatalogRoot() string { return "~/.tether/catalog" }

// parsePairs splits repeated KEY=VAL flag values into a map.
func parsePairs(items []string) map[string]string {
	out := map[string]string{}
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(in string) string {
	if in == "" || !strings.HasPrefix(in, "~") {
		return in
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return in
	}
	if in == "~" {
		return home
	}
	if strings.HasPrefix(in, "~/") {
		return filepath.Join(home, in[2:])
	}
	return in
}

// emitJSON writes v as indented JSON to stdout (the engine⇄app seam).
func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatalErr(err)
	}
}

// fatalErr prints err to stderr and exits with the fatal code.
func fatalErr(err error) {
	fmt.Fprintln(os.Stderr, "tachyon-engine:", err)
	os.Exit(exitFatal)
}

// fatalf prints a formatted message to stderr and exits fatally.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tachyon-engine: "+format+"\n", args...)
	os.Exit(exitFatal)
}
