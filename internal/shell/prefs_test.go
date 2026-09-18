package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/tachyon/internal/state"
)

// TestDefaultPrefsPathIsUnderConfigDir pins where the hotkey preference
// lives: state.ConfigDir()/shell.json, which on a default machine is
// ~/.config/tachyon/shell.json.
//
// This test used to pin the OPPOSITE path -- <os.UserConfigDir()>/Tachyon/
// shell.json, computed by hand -- because a silent relocation of the hotkey
// preference is a real regression: an existing user's rebound accelerator
// would stop being found. The relocation happened deliberately when the
// roots moved to XDG, and the concern that test encoded did not go away with
// it; it moved to state.Adopt, which carries the old file to the new
// location on startup. That is what makes changing this assertion safe, so
// the two are named in each other's comments:
// TestAdoptMovesLegacyFilesOnce is the other half.
func TestDefaultPrefsPathIsUnderConfigDir(t *testing.T) {
	t.Setenv(state.DirEnv, "")

	dir, err := state.ConfigDir()
	if err != nil {
		t.Skipf("cannot resolve the config dir on this machine: %v", err)
	}
	want := filepath.Join(dir, "shell.json")

	got, err := DefaultPrefsPath()
	if err != nil {
		t.Fatalf("DefaultPrefsPath: %v", err)
	}
	if got != want {
		t.Fatalf("DefaultPrefsPath() = %q; want %q", got, want)
	}
}

func TestNewStoreDefaultsWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "shell.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got := s.Get()
	if got.Hotkey != DefaultHotkey {
		t.Errorf("Hotkey = %q, want %q", got.Hotkey, DefaultHotkey)
	}
	if got.Manager.Width != defaultManagerWidth || got.Manager.Height != defaultManagerHeight {
		t.Errorf("Manager = %+v, want the default geometry", got.Manager)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("NewStore wrote a file for an absent path; it should only read")
	}
}

// The whole point of the hotkey being persisted is that it survives a restart.
// A restart is a fresh Store over the same path, which is exactly this.
func TestHotkeySurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	first, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := first.SetHotkey("Ctrl+Option+J"); err != nil {
		t.Fatalf("SetHotkey: %v", err)
	}

	second, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := second.Get().Hotkey; got != "Ctrl+Option+J" {
		t.Errorf("after reload Hotkey = %q, want %q", got, "Ctrl+Option+J")
	}
}

func TestManagerGeometrySurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	first, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	want := WindowGeometry{Width: 1234, Height: 567, X: 40, Y: 60, Placed: true}
	if err := first.SetManagerGeometry(want); err != nil {
		t.Fatalf("SetManagerGeometry: %v", err)
	}

	second, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := second.Get().Manager; got != want {
		t.Errorf("after reload Manager = %+v, want %+v", got, want)
	}
}

// Sampling a window mid-teardown yields 0x0. Persisting that would leave the
// manager unopenable on the next launch, so it is dropped rather than written.
func TestImplausibleGeometryIsNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	good := WindowGeometry{Width: 900, Height: 600, Placed: true}
	if err := s.SetManagerGeometry(good); err != nil {
		t.Fatalf("SetManagerGeometry: %v", err)
	}
	for _, bad := range []WindowGeometry{
		{Width: 0, Height: 0, Placed: true},
		{Width: 12, Height: 8, Placed: true},
		{Width: 99999, Height: 99999, Placed: true},
	} {
		if err := s.SetManagerGeometry(bad); err != nil {
			t.Fatalf("SetManagerGeometry(%+v): %v", bad, err)
		}
		if got := s.Get().Manager; got != good {
			t.Fatalf("geometry %+v was persisted over %+v", bad, good)
		}
	}
}

func TestCorruptFileIsAnErrorNotSilentDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path); err == nil {
		t.Error("NewStore accepted an unparsable file; it would overwrite the user's hotkey")
	}
}

func TestTruncatedFileNormalizesToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	if err := os.WriteFile(path, []byte(`{"manager":{"width":3,"height":3}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got := s.Get()
	if got.Hotkey != DefaultHotkey {
		t.Errorf("Hotkey = %q, want the default", got.Hotkey)
	}
	if got.Manager.Width != defaultManagerWidth {
		t.Errorf("Manager.Width = %d, want the default", got.Manager.Width)
	}
}

func TestWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shell.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.SetHotkey("Ctrl+Option+J"); err != nil {
		t.Fatalf("SetHotkey: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "shell.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only shell.json (temp file left behind)", names)
	}
}

// Wails' set/get asymmetry means the geometry read back after a restore
// differs from what was written by a point or two. Persisting that made the
// manager shrink on every launch (measured 1100 -> 1099 -> 1097 -> 1096).
func TestReadbackNoiseDoesNotShrinkTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	start := WindowGeometry{Width: 1100, Height: 720, X: 200, Y: 130, Placed: true}
	if err := s.SetManagerGeometry(start); err != nil {
		t.Fatalf("SetManagerGeometry: %v", err)
	}

	// Simulate ten launches, each reading back one point smaller than the
	// value it restored.
	g := start
	for i := 0; i < 10; i++ {
		g.Width--
		g.Height--
		if err := s.SetManagerGeometry(g); err != nil {
			t.Fatalf("SetManagerGeometry: %v", err)
		}
		g = s.Get().Manager
	}
	if got := s.Get().Manager; got != start {
		t.Errorf("after ten noisy launches Manager = %+v, want %+v", got, start)
	}
}

func TestRealResizeIsPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.SetManagerGeometry(WindowGeometry{Width: 1100, Height: 720, Placed: true}); err != nil {
		t.Fatal(err)
	}
	resized := WindowGeometry{Width: 900, Height: 600, X: 30, Y: 40, Placed: true}
	if err := s.SetManagerGeometry(resized); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Manager; got != resized {
		t.Errorf("Manager = %+v, want %+v — a real resize must be persisted", got, resized)
	}
}

// Hand-editing shell.json is the documented way back in when the bound
// accelerator turns out to be dead, so it is exactly where a typo lands. An
// accelerator that bind() would reject must not reach bind(): the result there
// is an app with no hotkey at all and one line in a log nobody reads.
func TestUnbindableHotkeyFallsBackToDefault(t *testing.T) {
	for _, bad := range []string{
		`"K"`,      // a bare key — Register accepts it and binds K system-wide
		`"Space"`,  // ditto, named key
		`"Ctrl+"`,  // no key
		`"Meta+K"`, // not a Wails modifier name
		`""`,       // empty
	} {
		path := filepath.Join(t.TempDir(), "shell.json")
		body := `{"hotkey":` + bad + `,"manager":{"width":900,"height":600,"placed":true}}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := NewStore(path)
		if err != nil {
			t.Fatalf("NewStore(%s): %v", bad, err)
		}
		if got := s.Get().Hotkey; got != DefaultHotkey {
			t.Errorf("hotkey %s loaded as %q, want the default %q", bad, got, DefaultHotkey)
		}
	}
}

// The corollary: a valid hand-edited accelerator is honoured exactly, or
// hand-editing would not be a recovery path at all.
func TestValidHandEditedHotkeyIsHonoured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.json")
	if err := os.WriteFile(path, []byte(`{"hotkey":"Cmd+Shift+P"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := s.Get().Hotkey; got != "Cmd+Shift+P" {
		t.Errorf("hotkey = %q, want %q", got, "Cmd+Shift+P")
	}
}

// Whatever normalize falls back to has to be bindable, or the fallback is not
// a fallback.
func TestNormalizeFallbackIsBindable(t *testing.T) {
	p := Prefs{Hotkey: "K"}
	p.normalize()
	if err := ValidateAccelerator(p.Hotkey); err != nil {
		t.Fatalf("normalize produced an unbindable hotkey %q: %v", p.Hotkey, err)
	}
}
