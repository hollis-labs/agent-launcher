package shell

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

// Config is everything the shell needs from its host.
type Config struct {
	// Assets is the built frontend, rooted at the directory holding
	// index.html. Required.
	Assets fs.FS

	// PrefsPath overrides where preferences are stored. Empty means
	// DefaultPrefsPath. Tests set it; the app does not.
	PrefsPath string

	// Logger receives shell diagnostics. Nil means slog.Default.
	Logger *slog.Logger
}

// Shell is the running window shell.
type Shell struct {
	app     *application.App
	log     *slog.Logger
	prefs   *Store
	palette *application.WebviewWindow
	manager *application.WebviewWindow
	tray    *application.SystemTray
	hotkey  *hotkeyBinder

	// geometryDirty coalesces manager resize/move events. Capacity 1: a burst
	// of resize events during a drag becomes one pending save.
	geometryDirty chan struct{}

	// managerShown gates geometry persistence. A window that has never been
	// shown has no geometry worth remembering: it still emits resize and move
	// events while it is being created and laid out, and persisting one of
	// those writes a frame the user never chose over the frame they did.
	managerShown atomic.Bool

	startedOnce sync.Once
	started     chan struct{}
}

// New builds the application, both windows, the tray and the hotkey wiring.
// It does not start the run loop; call Run.
func New(cfg Config) (*Shell, error) {
	if cfg.Assets == nil {
		return nil, fmt.Errorf("shell: Config.Assets is required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	path := cfg.PrefsPath
	if path == "" {
		p, err := DefaultPrefsPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	prefs, err := NewStore(path)
	if err != nil {
		return nil, err
	}

	s := &Shell{
		log:           log,
		prefs:         prefs,
		geometryDirty: make(chan struct{}, 1),
		started:       make(chan struct{}),
	}

	s.app = application.New(application.Options{
		Name:        "Tachyon",
		Description: "Edit the agent-setup bundle; launch sessions through Cairn.",
		Mac: application.MacOptions{
			// Accessory: no Dock icon. Tachyon lives in the menu bar, and both
			// windows are summoned rather than launched into.
			ActivationPolicy: application.ActivationPolicyAccessory,
			// Both windows only hide when closed, so the app must survive the
			// last one closing or the tray icon would take the process with it.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Assets: application.AssetOptions{
			Handler:        http.FileServer(http.FS(cfg.Assets)),
			DisableLogging: true,
		},
		Services: []application.Service{
			application.NewService(&Service{shell: s}),
		},
		LogLevel: slog.LevelWarn,
	})

	s.palette = s.app.Window.NewWithOptions(paletteOptions())
	s.manager = s.app.Window.NewWithOptions(managerOptions(prefs.Get().Manager))

	s.wireWindows()
	s.wireTray()
	s.wireHotkey()

	return s, nil
}

// paletteOptions is the palette posture. Every field here is load-bearing;
// see the package documentation and target architecture §4.
func paletteOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:            "palette",
		Title:           "Tachyon",
		Width:           680,
		Height:          420,
		Frameless:       true,
		DisableResize:   true,
		AlwaysOnTop:     true,
		Hidden:          true, // summoned, never launched into
		HideOnEscape:    true,
		HideOnFocusLost: true, // a palette that lingers is not a palette
		InitialPosition: application.WindowCentered,
		URL:             "/#/palette",
		Mac: application.MacWindow{
			// An NSPanel, not an NSWindow. NonActivating is the point:
			// summoning the palette leaves the user's current application
			// active, which is what separates a Spotlight-style palette from
			// an app window that happens to float.
			WindowClass: application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{
				FloatingPanel: true,
				NonActivating: true,
				// False on purpose: the palette's whole job is typing, so it
				// must take key status as soon as it is shown.
				BecomesKeyOnlyIfNeeded: false,
			},
			// This pair, not the window level, is what puts the palette over a
			// fullscreen application without switching Spaces.
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,

			// Deliberately NSPopUpMenuWindowLevel (101) rather than the
			// floating level (3) that §4 names.
			//
			// systemTrayPositionWindow (systemtray_darwin.m:323) does an
			// unconditional [nsWindow setLevel:NSPopUpMenuWindowLevel] every
			// time the tray positions an attached window. Since §4 also
			// requires the palette to be tray-attached, the level becomes 101
			// on the first tray toggle no matter what is declared here — and
			// stays there. Declaring 3 would therefore not produce 3; it would
			// produce a window whose level changes under the user the first
			// time they click the tray icon.
			//
			// Declaring 101 makes the level the same on every summon path.
			// Nothing §4 wants is lost: 101 is above the floating level, and
			// the over-fullscreen behaviour comes from CollectionBehavior
			// above, which the tray does not touch. Wails' own notch-window
			// preset (notch_window.go:86) pins the same level for the same
			// kind of surface.
			WindowLevel: application.MacWindowLevelPopUpMenu,
			Backdrop:    application.MacBackdropTranslucent,
			TitleBar: application.MacTitleBar{
				AppearsTransparent: true,
				Hide:               true,
			},
		},
	}
}

// managerOptions is the manager posture: an ordinary window, restored to the
// geometry it was last left at.
func managerOptions(g WindowGeometry) application.WebviewWindowOptions {
	opts := application.WebviewWindowOptions{
		Name:   "manager",
		Title:  "Tachyon",
		Width:  g.Width,
		Height: g.Height,
		Hidden: true,
		// Escape dismisses. Blur does NOT — an editor that vanishes when you
		// click away is unusable, and that is the whole reason this is a
		// second window rather than a mode of the first one.
		HideOnEscape:    true,
		HideOnFocusLost: false,
		InitialPosition: application.WindowCentered,
		URL:             "/#/manager",
		Mac: application.MacWindow{
			// No CollectionBehavior and no WindowLevel: the manager is an
			// ordinary managed window at the normal level, on its own Space.
			TitleBar: application.MacTitleBarHiddenInset,
		},
	}
	if g.Placed {
		// WindowXY, not the zero value: WindowCentered IS 0, so a literal 0
		// here silently centres the window and throws the saved position away.
		opts.InitialPosition = application.WindowXY
		opts.X, opts.Y = g.X, g.Y
	}
	return opts
}

func (s *Shell) wireWindows() {
	// Closing either window hides it; the process stays resident behind the
	// tray icon. Without this the palette would be destroyed the first time
	// Cmd+W reached it and the hotkey would summon nothing.
	for _, w := range []*application.WebviewWindow{s.palette, s.manager} {
		win := w
		win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			win.Hide()
			e.Cancel()
		})
	}

	// Persist the manager's geometry. The events fire continuously during a
	// drag, so they only mark the geometry dirty; a single goroutine debounces
	// and writes.
	mark := func(*application.WindowEvent) {
		select {
		case s.geometryDirty <- struct{}{}:
		default:
		}
	}
	s.manager.OnWindowEvent(events.Common.WindowDidResize, mark)
	s.manager.OnWindowEvent(events.Common.WindowDidMove, mark)
	go s.persistManagerGeometry()

	// A resize that happens seconds before quit would otherwise be lost with
	// the pending debounce.
	s.app.OnShutdown(func() { s.saveManagerGeometry() })
}

func (s *Shell) wireTray() {
	menu := application.NewMenu()
	menu.Add("Open Manager").OnClick(func(*application.Context) { s.OpenManager() })
	menu.Add("Settings…").OnClick(func(*application.Context) { s.OpenManagerSettings() })
	menu.AddSeparator()
	menu.Add("Quit Tachyon").OnClick(func(*application.Context) { s.app.Quit() })

	s.tray = s.app.SystemTray.New()
	s.tray.SetLabel("⌁")
	s.tray.SetTooltip("Tachyon")
	s.tray.SetTemplateIcon(icons.SystrayMacTemplate)

	// One tray icon serves both windows. applySmartDefaults installs
	// ToggleWindow as the left-click handler because a window is attached, and
	// ShowMenu as the right-click handler because a menu is set — so the
	// palette is a left-click away and the manager is in the right-click menu.
	s.tray.AttachWindow(s.palette).WindowOffset(6)
	s.tray.SetMenu(menu)
}

func (s *Shell) wireHotkey() {
	s.hotkey = newHotkeyBinder(s.app.GlobalShortcut, s.TogglePalette)

	s.app.Event.OnApplicationEvent(events.Common.ApplicationStarted,
		func(*application.ApplicationEvent) {
			s.startedOnce.Do(func() { close(s.started) })
		})

	// The goroutine starts before app.Run(), as §4 requires, but it waits for
	// the application to be running before registering.
	//
	// That wait is the point. GlobalShortcutManager.Register queues anything
	// registered before start onto a pending list and returns nil without
	// touching the OS (global_shortcut_manager.go:151); flushPending then
	// binds it during startup and routes any OS rejection to the application
	// error handler, where the caller never sees it. Registering after start
	// puts the rejection in the return value, which is the only failure mode
	// that IS reportable — a cross-process conflict is not (see the package
	// documentation).
	go func() {
		<-s.started
		accelerator := s.prefs.Get().Hotkey
		if err := s.hotkey.bind(accelerator); err != nil {
			s.log.Error("global hotkey not registered",
				"accelerator", accelerator, "err", err)
			return
		}
		s.log.Info("global hotkey registered", "accelerator", accelerator,
			"note", "registration success does not prove the hotkey fires")
	}()
}

// TogglePalette shows the palette if it is hidden, hides it if it is visible.
// Safe to call from any goroutine.
func (s *Shell) TogglePalette() {
	// One InvokeSync around the whole sequence, not one per call. Measured in
	// CW-20260903-0006: Show() followed by Focus() from a non-main goroutine
	// on a never-yet-shown window silently no-ops — no error, no panic,
	// IsVisible() simply stays false. Grouping them onto the main thread fixes
	// it. Nesting is safe: App.dispatchOnMainThread runs inline when it is
	// already on the main thread, so the inner InvokeSync calls do not
	// deadlock.
	application.InvokeSync(func() {
		if s.palette.IsVisible() {
			s.palette.Hide()
			return
		}
		s.palette.Show()
		s.palette.Center()
		s.palette.Focus()
	})
}

// OpenManager shows and focuses the manager. Safe to call from any goroutine.
//
// The palette can open the manager; nothing opens the palette as a manager.
func (s *Shell) OpenManager() {
	application.InvokeSync(func() {
		// An accessory app is not frontmost by default, so a plain Focus on a
		// window of it leaves the window behind whatever is in front.
		s.app.Show()
		s.manager.Show()
		s.manager.Focus()
	})
	s.managerShown.Store(true)
}

// OpenManagerSettings opens the manager on its settings pane.
func (s *Shell) OpenManagerSettings() {
	application.InvokeSync(func() {
		s.manager.SetURL("/#/manager/settings")
		s.app.Show()
		s.manager.Show()
		s.manager.Focus()
	})
	s.managerShown.Store(true)
}

// HidePalette hides the palette. Safe to call from any goroutine.
func (s *Shell) HidePalette() {
	application.InvokeSync(func() { s.palette.Hide() })
}

// Rebind changes the global hotkey and persists it.
//
// The OS binding is changed first: a preference that names an accelerator the
// app could not bind is worse than a rejected change, because the next launch
// would fail the same way with no one watching.
func (s *Shell) Rebind(accelerator string) error {
	if err := s.hotkey.bind(accelerator); err != nil {
		return err
	}
	return s.prefs.SetHotkey(accelerator)
}

// Run starts the application. It blocks until the app exits.
func (s *Shell) Run() error { return s.app.Run() }

// persistManagerGeometry debounces resize and move events into a single write.
func (s *Shell) persistManagerGeometry() {
	const settle = 500 * time.Millisecond
	for range s.geometryDirty {
		time.Sleep(settle)
		// Collapse anything that arrived while settling.
		select {
		case <-s.geometryDirty:
		default:
		}
		s.saveManagerGeometry()
	}
}

func (s *Shell) saveManagerGeometry() {
	if !s.managerShown.Load() {
		return
	}
	w, h := s.manager.Size()
	// Absolute, not relative: the window-creation path applies options.X/Y
	// through setPosition, which is absolute. Saving RelativePosition and
	// restoring it as options.X/Y silently moves the window on every launch.
	x, y := s.manager.Position()
	g := WindowGeometry{Width: w, Height: h, X: x, Y: y, Placed: true}
	if err := s.prefs.SetManagerGeometry(g); err != nil {
		s.log.Error("saving manager geometry", "err", err)
	}
}
