package launchcomposer_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/launch"
	"github.com/hollis-labs/tachyon/internal/launchcomposer"
	"github.com/hollis-labs/tachyon/internal/launchprofile"
)

// fakeLauncher records what reached LaunchComposition without launching
// anything.
type fakeLauncher struct {
	calls []launch.CompositionInput
	err   error
}

func (f *fakeLauncher) LaunchComposition(in launch.CompositionInput) error {
	f.calls = append(f.calls, in)
	return f.err
}

func newComposer(t *testing.T) (*launchcomposer.Service, launchprofile.Store, *fakeLauncher) {
	t.Helper()
	dir := t.TempDir()
	l := &fakeLauncher{}
	return launchcomposer.NewServiceAt(l, dir), launchprofile.Open(dir), l
}

func fullInput() launchcomposer.Input {
	return launchcomposer.Input{
		Name:     "codex",
		Provider: "codex",
		Skills:   []string{"search-first", "surface-discovery"},
		Prompts:  []string{"report"},
		Target:   "engineer",
		Scope:    "/work/tachyon",
		Parts:    []string{"nanite-domain"},
		Sets:     []launch.SetInput{{Slot: "role", Value: "reviewer"}},
	}
}

// TestSaveWritesACairnPart is the whole point of the package: what is saved
// must be what cairn reads. A file this composer writes is an ordinary part,
// so `cairn boot --with <it>` consumes it directly — unlike the binding
// composer it replaced, which wrote a format of Tachyon's own that cairn
// could not read at all.
func TestSaveWritesACairnPart(t *testing.T) {
	svc, store, _ := newComposer(t)

	result, err := svc.Save(fullInput())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if result.Name != "codex" {
		t.Errorf("result.Name = %q", result.Name)
	}

	data, err := store.Read("codex")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := string(data)

	// Frontmatter cairn parses, with the provider it must carry.
	if !strings.HasPrefix(got, "---\n") || !strings.Contains(got, "\n---\n") {
		t.Fatalf("saved document is not fenced frontmatter:\n%s", got)
	}
	for _, want := range []string{"id: codex", "provider: codex", "skills:", "search-first", "prompts:", "report"} {
		if !strings.Contains(got, want) {
			t.Errorf("saved document is missing %q:\n%s", want, got)
		}
	}

	// And it round-trips through the store's own reader, which is what the
	// palette lists from.
	p, err := store.Get("codex")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Provider != "codex" {
		t.Errorf("saved profile's provider reads back as %q", p.Provider)
	}
}

// TestSaveDropsThePerLaunchFieldsAndSaysWhich is the honesty half. A person
// who added three parts and a set, saved, and found none of it in the file
// would reasonably conclude the save was broken.
func TestSaveDropsThePerLaunchFieldsAndSaysWhich(t *testing.T) {
	svc, store, _ := newComposer(t)

	result, err := svc.Save(fullInput())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, _ := store.Read("codex")
	got := string(data)
	for _, gone := range []string{"engineer", "/work/tachyon", "nanite-domain", "reviewer", "scope"} {
		if strings.Contains(got, gone) {
			t.Errorf("saved document carries the per-launch value %q:\n%s", gone, got)
		}
	}

	want := map[string]bool{"scope": true, "parts": true, "sets": true}
	if len(result.Dropped) != len(want) {
		t.Fatalf("Dropped = %v; want exactly %v", result.Dropped, want)
	}
	for _, d := range result.Dropped {
		if !want[d] {
			t.Errorf("Dropped names %q, which is not a per-launch field", d)
		}
	}
}

// TestSaveReportsNothingDroppedWhenNothingWas: the notice must not fire on a
// form where every filled field was durable, or it becomes noise people
// learn to ignore.
func TestSaveReportsNothingDroppedWhenNothingWas(t *testing.T) {
	svc, _, _ := newComposer(t)

	result, err := svc.Save(launchcomposer.Input{
		Name: "plain", Provider: "claude", Skills: []string{"commit"},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(result.Dropped) != 0 {
		t.Fatalf("Dropped = %v; want nothing", result.Dropped)
	}
}

// TestSaveRefusesAProfileWithNoProvider: a launch profile with no provider
// is refused by cairn at every boot, so writing one produces a file that
// looks saved and never works. Defaulting one instead would be the launcher
// inferring exactly what three components just agreed nobody infers.
func TestSaveRefusesAProfileWithNoProvider(t *testing.T) {
	svc, store, _ := newComposer(t)

	_, err := svc.Save(launchcomposer.Input{Name: "noprovider", Provider: "  "})
	if err == nil {
		t.Fatal("Save accepted a launch profile with no provider")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Errorf("the refusal does not name the missing key: %v", err)
	}
	if _, err := store.Get("noprovider"); !errors.Is(err, launchprofile.ErrNotFound) {
		t.Error("a file was written despite the refusal")
	}
}

func TestSaveRefusesABadName(t *testing.T) {
	svc, _, _ := newComposer(t)
	for _, bad := range []string{"", "../escape", "sub/dir", ".hidden"} {
		if _, err := svc.Save(launchcomposer.Input{Name: bad, Provider: "claude"}); err == nil {
			t.Errorf("Save accepted the name %q", bad)
		}
	}
}

func TestSaveRefusesAnExistingName(t *testing.T) {
	svc, _, _ := newComposer(t)
	in := launchcomposer.Input{Name: "dupe", Provider: "claude"}
	if _, err := svc.Save(in); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if _, err := svc.Save(in); !errors.Is(err, launchprofile.ErrExists) {
		t.Fatalf("second Save = %v; want ErrExists", err)
	}
}

// TestSaveAndLaunchLaunchesThroughWhatItSaved is the correction to what the
// binding composer did, and the reason is that the artifact is now readable
// by the thing that consumes it.
//
// A binding was a format cairn could not read, so launching "the binding
// just saved" would have applied its parts twice — once from the file and
// once from the flags — and that composer deliberately replayed the form
// instead. A launch profile IS what cairn reads, so launching through it
// applies each thing exactly once, and any drift between what was saved and
// what runs shows up on this launch rather than the next one.
func TestSaveAndLaunchLaunchesThroughWhatItSaved(t *testing.T) {
	svc, _, launcher := newComposer(t)

	if _, err := svc.SaveAndLaunch(fullInput()); err != nil {
		t.Fatalf("SaveAndLaunch: %v", err)
	}
	if len(launcher.calls) != 1 {
		t.Fatalf("launched %d times; want 1", len(launcher.calls))
	}
	got := launcher.calls[0]

	if got.LaunchProfile != "codex" {
		t.Errorf("LaunchProfile = %q; want the profile just saved", got.LaunchProfile)
	}
	if got.Target != "engineer" {
		t.Errorf("Target = %q", got.Target)
	}
	// Skills and prompts are in the FILE now. Passing them again would be
	// harmless (cairn keys both by id) but it would mean two sources for one
	// list, and a divergence between them would never show.
	if len(got.Skills) != 0 || len(got.Prompts) != 0 {
		t.Errorf("skills/prompts were replayed as flags (%v / %v); they are in the saved profile", got.Skills, got.Prompts)
	}
	// The per-launch fields still travel: they were never saved, and a
	// person who composed them expects this launch to have them.
	if got.Scope != "/work/tachyon" {
		t.Errorf("Scope = %q; the scope was not saved, so it must still be passed", got.Scope)
	}
	if len(got.Parts) != 1 || got.Parts[0] != "nanite-domain" {
		t.Errorf("Parts = %v; a one-off part was not saved, so it must still be passed", got.Parts)
	}
	if len(got.Sets) != 1 {
		t.Errorf("Sets = %v", got.Sets)
	}
}

// TestSaveAndLaunchReportsTheSaveWhenTheLaunchFails: the file is on disk
// either way, and an error that only said "launch failed" would leave
// someone unsure whether to save again.
func TestSaveAndLaunchReportsTheSaveWhenTheLaunchFails(t *testing.T) {
	svc, store, launcher := newComposer(t)
	launcher.err = errors.New("cairn refused the composition")

	result, err := svc.SaveAndLaunch(fullInput())
	if err == nil {
		t.Fatal("SaveAndLaunch returned no error despite a failed launch")
	}
	if result.Name != "codex" {
		t.Errorf("the result does not name what was saved: %+v", result)
	}
	if !strings.Contains(err.Error(), "saved") {
		t.Errorf("the error does not say the file was written: %v", err)
	}
	if _, getErr := store.Get("codex"); getErr != nil {
		t.Errorf("the profile is not on disk after a failed launch: %v", getErr)
	}
}

// TestLaunchSavesNothing: the third action is launch-only, and must leave
// the store untouched.
func TestLaunchSavesNothing(t *testing.T) {
	svc, store, launcher := newComposer(t)

	if err := svc.Launch(fullInput()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if len(launcher.calls) != 1 {
		t.Fatalf("launched %d times; want 1", len(launcher.calls))
	}
	if _, err := store.Get("codex"); !errors.Is(err, launchprofile.ErrNotFound) && !errors.Is(err, launchprofile.ErrDirMissing) {
		t.Errorf("Launch wrote a launch profile: %v", err)
	}
	// Launch replays the form wholesale, because nothing was saved for the
	// launch profile to supply.
	got := launcher.calls[0]
	if len(got.Skills) != 2 || len(got.Prompts) != 1 {
		t.Errorf("Launch dropped form additions: skills=%v prompts=%v", got.Skills, got.Prompts)
	}
}

// TestBlankListEntryIsRefused: a blank entry becomes a blank id in a
// collection cairn keys by id, which is a lookup failure at boot rather than
// here.
func TestBlankListEntryIsRefused(t *testing.T) {
	svc, _, _ := newComposer(t)
	_, err := svc.Save(launchcomposer.Input{
		Name: "blank", Provider: "claude", Skills: []string{"real", "   "},
	})
	if err == nil {
		t.Fatal("Save accepted a blank skill entry")
	}
	if !strings.Contains(err.Error(), "skills[1]") {
		t.Errorf("the refusal does not name which entry: %v", err)
	}
}
