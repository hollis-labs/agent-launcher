package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// DefaultHotkey is the accelerator the shell falls back to.
//
// Not Cmd+Shift+Space: CW-20260518-0056 measured that combination silently
// colliding with 1Password's Quick Access, and CW-20260903-0006 re-measured it
// registering with a nil error and then never firing. A default that is quiet
// on a typical machine is worth more than a memorable one, and the user can
// rebind either way.
const DefaultHotkey = "Ctrl+Option+Space"

// Default manager geometry, used on first run and whenever the persisted
// values are missing or implausible.
const (
	defaultManagerWidth  = 1100
	defaultManagerHeight = 720

	// Guard rails for restored geometry. A window restored to 12x8 because a
	// resize event was sampled mid-teardown is unrecoverable by mouse.
	minManagerWidth  = 480
	minManagerHeight = 320
	maxManagerDim    = 20000

	// geometryEpsilon is the smallest change treated as a real move or resize.
	//
	// Wails' set and get are not symmetric on macOS: windowSetSize computes a
	// content size, applies it, and then setFrame:display:animate:YES, while
	// windowGetSize reads the live NSWindow frame. Reading back what was just
	// written therefore returns a value one or two points off. Persisting that
	// difference shrank the saved size on every launch — measured drifting
	// 1100 -> 1099 -> 1097 -> 1096 across three restarts.
	//
	// A person resizing a window moves it by more than this; the readback
	// asymmetry never does. The trade is deliberate and it is real: a resize
	// of four points or fewer is discarded.
	//
	// This is one of two guards, and they cover different things. This one
	// covers the asymmetry on a window that is up and being used. Shell's
	// managerShown covers the resize and move events a window emits while it
	// is being created, which are not a user's choice at all.
	geometryEpsilon = 4
)

// WindowGeometry is a persisted window size and position.
//
// X and Y are ABSOLUTE screen coordinates, because that is what the window
// creation path consumes: options.X/Y are applied through setPosition, which
// is absolute. Saving WebviewWindow.RelativePosition and restoring it as
// options.X/Y moves the window a little on every launch, which is how this
// was originally written and what the current shape fixes.
//
// The cost of absolute coordinates is that they do not survive a display
// going away; sane() drops an origin that has gone out of range rather than
// restoring the window onto a screen that no longer exists.
type WindowGeometry struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	X      int `json:"x"`
	Y      int `json:"y"`
	// Placed distinguishes "never positioned" from "positioned at 0,0".
	Placed bool `json:"placed"`
}

// near reports whether other is the same geometry as g up to readback noise.
func (g WindowGeometry) near(other WindowGeometry) bool {
	within := func(a, b int) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d <= geometryEpsilon
	}
	return within(g.Width, other.Width) && within(g.Height, other.Height) &&
		within(g.X, other.X) && within(g.Y, other.Y)
}

func (g WindowGeometry) sane() bool {
	if g.Width < minManagerWidth || g.Height < minManagerHeight ||
		g.Width > maxManagerDim || g.Height > maxManagerDim {
		return false
	}
	// Position is absolute screen coordinates, which do not survive a display
	// going away. A wildly out-of-range origin is dropped along with the size
	// rather than restored onto a screen that no longer exists.
	return g.X > -maxManagerDim && g.X < maxManagerDim &&
		g.Y > -maxManagerDim && g.Y < maxManagerDim
}

// Prefs is everything the shell persists across restarts.
type Prefs struct {
	Hotkey  string         `json:"hotkey"`
	Manager WindowGeometry `json:"manager"`
}

func defaultPrefs() Prefs {
	return Prefs{
		Hotkey: DefaultHotkey,
		Manager: WindowGeometry{
			Width:  defaultManagerWidth,
			Height: defaultManagerHeight,
		},
	}
}

// normalize replaces missing or implausible values with defaults. It is applied
// on load so that a hand-edited or truncated file degrades to a usable app
// rather than to an invisible window or an unregistrable hotkey.
//
// The hotkey is validated, not merely checked for emptiness. Hand-editing
// shell.json is the documented way back in when the bound accelerator turns
// out to be dead (see the package documentation), so it is exactly the file a
// user edits under pressure and exactly where a typo lands. An accelerator
// that would be rejected at bind time is replaced here, because the
// alternative is an app that starts with NO hotkey at all and one line in a
// log nobody is reading.
func (p *Prefs) normalize() {
	if ValidateAccelerator(p.Hotkey) != nil {
		p.Hotkey = DefaultHotkey
	}
	if !p.Manager.sane() {
		p.Manager.Width = defaultManagerWidth
		p.Manager.Height = defaultManagerHeight
		p.Manager.X, p.Manager.Y, p.Manager.Placed = 0, 0, false
	}
}

// Store reads and writes Prefs at a fixed path. Every method is safe for
// concurrent use: the geometry writer runs on its own goroutine and the
// settings service is called from the frontend.
type Store struct {
	path string

	mu    sync.Mutex
	prefs Prefs
}

// DefaultPrefsPath is where the shell keeps its preferences.
//
// os.UserConfigDir is ~/Library/Application Support on macOS, which is where a
// GUI app's own state belongs. Note this is deliberately NOT inside the
// agent-setup bundle: the bundle is content under git, and shell preferences
// are neither.
func DefaultPrefsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	return filepath.Join(dir, "Tachyon", "shell.json"), nil
}

// NewStore loads preferences from path, falling back to defaults when the file
// is absent. A file that exists but cannot be read or parsed is an error: it
// is likelier to be a bug or a permission problem than an empty state, and
// silently overwriting it would lose the user's hotkey.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path, prefs: defaultPrefs()}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var p Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	p.normalize()
	s.prefs = p
	return s, nil
}

// Path is the file the store reads and writes.
func (s *Store) Path() string { return s.path }

// Get returns a copy of the current preferences.
func (s *Store) Get() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefs
}

// SetHotkey records a new accelerator and writes the file.
func (s *Store) SetHotkey(accelerator string) error {
	return s.update(func(p *Prefs) { p.Hotkey = accelerator })
}

// SetManagerGeometry records the manager's size and position and writes the
// file.
//
// Two classes of write are dropped rather than persisted:
//
//   - Implausible geometry. Sampling a window mid-teardown otherwise bakes a
//     0x0 size into the file and leaves the manager unopenable next launch.
//   - Changes smaller than geometryEpsilon. Those are readback noise, not a
//     user resizing a window, and persisting them made the window shrink a
//     little on every launch.
func (s *Store) SetManagerGeometry(g WindowGeometry) error {
	if !g.sane() {
		return nil
	}
	s.mu.Lock()
	unchanged := s.prefs.Manager.Placed && s.prefs.Manager.near(g)
	s.mu.Unlock()
	if unchanged {
		return nil
	}
	return s.update(func(p *Prefs) { p.Manager = g })
}

func (s *Store) update(mutate func(*Prefs)) error {
	s.mu.Lock()
	next := s.prefs
	mutate(&next)
	next.normalize()
	s.prefs = next
	s.mu.Unlock()
	return s.write(next)
}

// write persists prefs atomically: a crash between truncate and write would
// otherwise leave an empty file, and NewStore treats an unparsable file as an
// error rather than as defaults.
func (s *Store) write(p Prefs) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(s.path), err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding preferences: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".shell-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("renaming into %s: %w", s.path, err)
	}
	return nil
}
