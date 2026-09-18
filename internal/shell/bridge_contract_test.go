package shell

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/launch"
	"github.com/hollis-labs/tachyon/internal/launchcomposer"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
	"github.com/hollis-labs/tachyon/internal/manager"
	"github.com/hollis-labs/tachyon/internal/preview"
	"github.com/hollis-labs/tachyon/internal/project"
)

// The frontend calls Go through three hops, and until these tests existed
// only the middle one was checked:
//
//	Palette.jsx        Launch.Targets()
//	bridge.js          Targets: () => callService(LAUNCH_SERVICE, "Targets")
//	internal/launch    func (s *Service) Targets() ([]Target, error)
//
// A break at hop one is a TypeError in a rendered window
// ("Kf.Targets is not a function"), and a break at hop two is a rejected
// promise at the moment someone clicks — both at runtime, in a build that
// compiled and whose whole test suite passed. That happened: Launch.Targets
// was wired into the palette and never added to the bridge, and the
// source-level contract test asserted the CALL existed without ever asking
// whether anything answered it.
//
// These two tests close both hops by deriving one side from the other rather
// than by listing either, so a method added to a component, the bridge, or a
// service is covered the moment it is written.

// bridgeExportPattern finds `export const Name = {` in bridge.js.
var bridgeExportPattern = regexp.MustCompile(`export const (\w+) = \{`)

// bridgeMethodPattern finds a method key inside one of those objects:
// `  Targets: () => ...`. Anchored to the two-space indent bridge.js uses for
// object members, so a nested arrow body cannot be mistaken for one.
var bridgeMethodPattern = regexp.MustCompile(`(?m)^  (\w+): `)

// bridgeImportPattern finds what a component imported from the bridge:
// `import { Launch, LaunchProfile, Manager } from "./bridge.js"`.
var bridgeImportPattern = regexp.MustCompile(`import \{([^}]*)\} from "\./bridge\.js"`)

// readFrontendFile reads one file under frontend/src.
func readFrontendFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "frontend", "src", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// bridgeExports maps each exported object in bridge.js to its method names.
func bridgeExports(t *testing.T) map[string]map[string]bool {
	t.Helper()
	source := readFrontendFile(t, "bridge.js")

	locs := bridgeExportPattern.FindAllStringSubmatchIndex(source, -1)
	if len(locs) == 0 {
		t.Fatal("no `export const X = {` found in bridge.js; this test's parser has drifted from the file's shape")
	}

	out := map[string]map[string]bool{}
	for i, loc := range locs {
		name := source[loc[2]:loc[3]]
		// The object's body runs to the next export, or to end of file.
		end := len(source)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		methods := map[string]bool{}
		for _, m := range bridgeMethodPattern.FindAllStringSubmatch(source[loc[1]:end], -1) {
			methods[m[1]] = true
		}
		out[name] = methods
	}
	return out
}

// TestEveryBridgeCallInTheFrontendExists is hop one: every `X.Method(` a
// component makes on something it imported from bridge.js must be a method
// bridge.js actually defines.
//
// It is derived rather than listed. A hand-written list of expected calls is
// what the palette contract test already had, and it passed while the app was
// broken: it asserted the call was PRESENT, which is a different question
// from whether anything answers it.
func TestEveryBridgeCallInTheFrontendExists(t *testing.T) {
	exports := bridgeExports(t)

	dir := filepath.Join("..", "..", "frontend", "src")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsx") {
			continue
		}
		source := readFrontendFile(t, name)

		imported := bridgeImportPattern.FindStringSubmatch(source)
		if imported == nil {
			continue // this component does not talk to the bridge at all
		}
		for _, raw := range strings.Split(imported[1], ",") {
			// `LaunchComposer as ComposerAPI` — the local name is what the
			// calls below use, the exported name is what bridge.js defines.
			local, exported := raw, raw
			if before, after, found := strings.Cut(raw, " as "); found {
				exported, local = before, after
			}
			exported, local = strings.TrimSpace(exported), strings.TrimSpace(local)
			methods, ok := exports[exported]
			if !ok {
				t.Errorf("%s imports %q from bridge.js, which exports no such object", name, exported)
				continue
			}

			callPattern := regexp.MustCompile(regexp.QuoteMeta(local) + `\.(\w+)\(`)
			for _, call := range callPattern.FindAllStringSubmatch(source, -1) {
				method := call[1]
				checked++
				if !methods[method] {
					t.Errorf(
						"%s calls %s.%s(), which bridge.js does not define on %s.\n"+
							"This is the shape of the break that shipped: the call compiles, the\n"+
							"bundle builds, and the window throws at render.",
						name, local, method, exported,
					)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no bridge calls found in any component; this test's parser has drifted and is proving nothing")
	}
	t.Logf("checked %d bridge calls across the frontend", checked)
}

// TestEveryBridgeMethodReachesAGoMethod is hop two: every method bridge.js
// defines must name a real exported method on the Go service it routes to.
//
// Wails binds by name at runtime — Call.ByName("<pkg>.<Type>.<Method>") — so
// a typo, or a Go method renamed out from under the bridge, is a rejected
// promise at the moment someone clicks rather than anything the compiler or
// the bundler sees.
//
// The service types are listed here because that mapping genuinely is a fact
// about this file's own registration (see New, which constructs exactly
// these), but the METHOD names on both sides are derived — so adding one to
// either side and forgetting the other fails here.
func TestEveryBridgeMethodReachesAGoMethod(t *testing.T) {
	exports := bridgeExports(t)

	// bridge.js's export name -> the Go service it calls through. Shell's own
	// Service is included: the palette reaches it through the bare `call`
	// helper, so its methods are exported under `Shell`.
	services := map[string]reflect.Type{
		"Shell":              reflect.TypeOf(&Service{}),
		"Manager":            reflect.TypeOf(&manager.Service{}),
		"Launch":             reflect.TypeOf(&launch.Service{}),
		"LaunchProfile":      reflect.TypeOf(&launchprofile.Service{}),
		"LaunchComposer":     reflect.TypeOf(&launchcomposer.Service{}),
		"CompositionPreview": reflect.TypeOf(&preview.Service{}),
		"Project":            reflect.TypeOf(&project.Service{}),
	}

	// Where the bridge deliberately renames: the JS name on the left, the Go
	// method on the right. Kept explicit and tiny — a rename that is not
	// listed here is a mistake, not a convention.
	renamed := map[string]string{
		"Launch.Composition":         "LaunchComposition",
		"CompositionPreview.Preview": "Preview",
	}

	for exportName, methods := range exports {
		typ, ok := services[exportName]
		if !ok {
			t.Errorf("bridge.js exports %q, which this test has no Go service for; add it to `services` or the frontend is calling something unregistered", exportName)
			continue
		}
		names := make([]string, 0, len(methods))
		for m := range methods {
			names = append(names, m)
		}
		sort.Strings(names)

		for _, jsName := range names {
			goName := jsName
			if mapped, ok := renamed[exportName+"."+jsName]; ok {
				goName = mapped
			}
			if _, ok := typ.MethodByName(goName); !ok {
				t.Errorf(
					"bridge.js defines %s.%s, but %s has no exported method %q.\n"+
						"Wails binds by name at runtime, so this is a rejected promise on click,\n"+
						"not a compile error.",
					exportName, jsName, typ, goName,
				)
			}
		}
	}

	// And the reverse for the one service whose whole surface the palette
	// depends on: a Go method the bridge never exposes is dead to the
	// frontend, which is worth knowing when it was added FOR the frontend.
	launchType := services["Launch"]
	for i := 0; i < launchType.NumMethod(); i++ {
		goName := launchType.Method(i).Name
		exposed := exports["Launch"][goName]
		for js, mapped := range renamed {
			if mapped == goName && strings.HasPrefix(js, "Launch.") {
				exposed = exposed || exports["Launch"][strings.TrimPrefix(js, "Launch.")]
			}
		}
		if !exposed {
			t.Errorf("internal/launch.Service.%s is exported but bridge.js exposes no way to call it", goName)
		}
	}
}
