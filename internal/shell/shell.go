package shell

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"

	"github.com/hollis-labs/tachyon/internal/apply"
	"github.com/hollis-labs/tachyon/internal/binding"
	"github.com/hollis-labs/tachyon/internal/bindingcomposer"
	"github.com/hollis-labs/tachyon/internal/boot"
	"github.com/hollis-labs/tachyon/internal/bundle"
	"github.com/hollis-labs/tachyon/internal/launch"
	"github.com/hollis-labs/tachyon/internal/manager"
	"github.com/hollis-labs/tachyon/internal/preview"
	"github.com/hollis-labs/tachyon/internal/project"
	"github.com/hollis-labs/tachyon/internal/state"
)

// Config is everything the shell needs from its host.
type Config struct {
	// Assets is the built frontend, rooted at the directory holding
	// index.html. Required.
	Assets fs.FS

	// PrefsPath overrides where preferences are stored. Empty means
	// DefaultPrefsPath. Tests set it; the app does not.
	PrefsPath string

	// BundleRootStorePath overrides where the manager's active-bundle-root
	// setting is persisted (see [bundle.RootStore]). Empty means
	// [bundle.DefaultRootStore]. Tests set it so a test run never touches
	// ~/Library/Application Support or, through the default it stores,
	// implies anything about ~/dev/projects/agent-setup.
	BundleRootStorePath string

	// ProjectStorePath overrides projects.json. Tests can keep all Tachyon
	// state in scratch space; the app leaves this empty to use state.Root().
	ProjectStorePath string

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

	rootStore, err := bundleRootStore(cfg.BundleRootStorePath)
	if err != nil {
		return nil, err
	}
	mgr := manager.New(rootStore)
	// The same rootStore the manager reads, so the palette's bindings list
	// and the manager's tree always agree on which bundle is active.
	bindings := binding.NewService(rootStore)
	// Same rootStore again: a launch resolves the binding the palette just
	// showed, from the same bundle everything else above is reading.
	launcher := launch.NewService(rootStore)
	composer := bindingcomposer.NewService(rootStore, launcher)
	compositionPreview := preview.NewService(rootStore, preview.Options{})
	// Same rootStore a fourth time: Apply (CW-20260904-0023) stages
	// whichever bundle everything else above is reading. apply.Options{}
	// (the zero value) is real behavior — the real AGENTS_HOME
	// (apply.ResolveAgentsHome) and a real `make install-system` runner
	// (apply.ExecRunner) — never overridden here; only this package's own
	// tests override either.
	stager := apply.NewService(rootStore, apply.Options{})
	var projectStore project.Store
	if cfg.ProjectStorePath != "" {
		projectStore.Path = cfg.ProjectStorePath
	} else {
		projectStore, err = project.DefaultStore()
		if err != nil {
			return nil, err
		}
	}
	projects := project.NewService(projectStore, rootStore)

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
			application.NewService(mgr),
			application.NewService(bindings),
			application.NewService(launcher),
			application.NewService(composer),
			application.NewService(compositionPreview),
			application.NewService(stager),
			application.NewService(projects),
		},
		LogLevel: slog.LevelWarn,
	})

	s.palette = s.app.Window.NewWithOptions(paletteOptions())
	s.manager = s.app.Window.NewWithOptions(managerOptions(prefs.Get().Manager))

	s.wireWindows()
	s.wireTray()
	s.wireHotkey()
	s.wireBootSweep()

	return s, nil
}

// bundleRootStore resolves the settings file the manager persists its active
// bundle root to. An explicit override (tests) is used verbatim; otherwise it
// is [bundle.DefaultRootStore] — a different file from the shell's own
// preferences, since the bundle root is a fact about which bundle is being
// edited, not about the shell's windows.
func bundleRootStore(override string) (bundle.RootStore, error) {
	if override != "" {
		return bundle.RootStore{Path: override}, nil
	}
	return bundle.DefaultRootStore()
}

// paletteOptions is the palette posture. Every field here is load-bearing;
// see the package documentation and target architecture §4.
func paletteOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:  "palette",
		Title: "Tachyon",
		Width: 680,
		// The normal surface gives its middle to the binding list. Composition
		// now opens in a bounded overlay whose body scrolls independently while
		// its Launch action stays fixed at the bottom; 640 leaves that deliberate
		// editing moment useful without making the always-visible list compete
		// with the form. DisableResize keeps this the one geometry decision.
		Height:          640,
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
	// No SetLabel here (CW-20260904-0004, issue 2). shell.go was unchanged
	// since 3e1136c, so the missing icon was not a code regression to find by
	// diffing — investigated instead: every one of Wails v3's own systray
	// examples (systray-basic, systray-custom, systray-menu, systray-clock,
	// v3.0.0-beta.16, checked in the module cache under
	// github.com/wailsapp/wails/v3@v3.0.0-beta.16/examples/) calls
	// SetTemplateIcon on darwin and never SetLabel alongside it; SetTooltip is
	// the label-shaped call they use instead, and macOS's own systray impl
	// (systemtray_darwin.go) even documents SetTooltip as a deliberate no-op
	// there ("Tooltips not supported on macOS"). The label/icon pairing this
	// code carried had no working precedent anywhere in Wails' own tree, so it
	// is the first thing that changed here — restoring the pairing every
	// upstream example actually uses, rather than tracing an OS-level
	// title/image conflict through Cocoa that no example exhibits. The prior
	// "environmental/Bartender" explanation for the missing icon
	// (CW-20260903-0006) was measured against a separately ad-hoc-signed .app
	// and was later retracted once Chrispian confirmed the icon rendered
	// under `go run .`; nothing here reuses that conclusion, and this change
	// has NOT been visually confirmed in a menu bar by this agent — it
	// cannot see one. It needs a human look under `go run .`.
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

// bootSweepTimeout bounds one whole [Shell.SweepBootDirectories] call —
// generous relative to boot's own per-lsof-invocation timeout, since a
// real boot root can hold several bindings' worth of .prev-* candidates,
// each probed once. It exists so a wedged lsof (a hung filesystem, an NFS
// mount, whatever) cannot leave the startup sweep's goroutine running
// forever; it does not exist to make the sweep run on any kind of
// schedule — see this method's own doc for why there is exactly one
// startup call and one manual one, and nothing periodic.
const bootSweepTimeout = 60 * time.Second

// wireBootSweep runs [Shell.SweepBootDirectories] exactly once, after the
// application has finished starting — same "wait for s.started, then run
// in a goroutine" shape as [Shell.wireHotkey], for the same reason: this
// must not block Run() or the window creation sequence, and nothing here
// needs the main thread. This is the ONLY place this package schedules a
// sweep. There is no ticker, no timer, no periodic re-run — the sweep
// specified in CW-20260903-0019 is deliberately "once at app start, plus
// whenever a person clicks the manual action in the manager
// (Service.SweepBootDirectories)" and nothing else; see that task's own
// hazard notes for why an automatic re-sweep is not a safe default even
// though it would be easy to add here.
func (s *Shell) wireBootSweep() {
	go func() {
		<-s.started

		ctx, cancel := context.WithTimeout(context.Background(), bootSweepTimeout)
		defer cancel()

		report, err := s.SweepBootDirectories(ctx)
		if err != nil {
			s.log.Error("boot directory sweep failed", "err", err)
			return
		}
		s.logSweepReport(report)
	}()
}

// SweepBootDirectories runs [boot.Sweep] against Tachyon's real boot root
// ([state.BootRoot]) and the real lsof binary, and returns what it found.
// This is the one place that resolves those two real values and builds
// the real [boot.LsofRunner] — both [wireBootSweep] (the automatic sweep
// at app start) and [Service.SweepBootDirectories] (the manual action the
// manager's Settings pane offers) call this method, so there is exactly
// one implementation of "what Sweep actually runs against in this app,"
// not two that could quietly drift apart.
//
// Resolving lsof follows the same pattern internal/launch.Service.Launch
// and cmd/tachyon/main.go already use for cairn: exec.LookPath first,
// against the app's real PATH. Unlike those two call sites, a failed
// lookup here is not itself a fatal error — an empty lsofPath still
// produces a working [boot.LsofRunner] (it runs the bare command name
// "lsof", deferring the failure to exec time), and [boot.Sweep] already
// turns "lsof could not be run at all" into a reported, visible
// Report.GuardOK == false rather than a crash. See [boot.Sweep]'s own doc
// for the full argument against ever treating an lsof failure as
// something to guess past.
func (s *Shell) SweepBootDirectories(ctx context.Context) (boot.Report, error) {
	root, err := state.BootRoot()
	if err != nil {
		return boot.Report{}, fmt.Errorf("shell: resolving boot root: %w", err)
	}

	lsofPath, err := exec.LookPath("lsof")
	if err != nil {
		s.log.Warn("lsof not found via LookPath; boot sweep will report the guard as unavailable", "err", err)
		lsofPath = ""
	}

	return boot.Sweep(ctx, root, boot.ExecLsofRunner(lsofPath))
}

// logSweepReport writes one [boot.Report] to s.log at a level matched to
// what it says: an unavailable guard is a warning a person should notice
// (nothing was swept, and not because there was nothing to sweep), a
// removed directory is worth a line per directory, and a skipped
// candidate with an ordinary "still held open" reason is routine detail,
// not a warning.
func (s *Shell) logSweepReport(report boot.Report) {
	s.log.Info("boot directory sweep complete",
		"guardOK", report.GuardOK, "swept", len(report.Swept), "skipped", len(report.Skipped))

	if !report.GuardOK {
		s.log.Warn("boot directory sweep: liveness guard unavailable this run — nothing was removed",
			"detail", report.GuardDetail)
	}
	for _, p := range report.Swept {
		s.log.Info("boot directory sweep: removed", "path", p)
	}
	for _, sk := range report.Skipped {
		s.log.Debug("boot directory sweep: skipped", "path", sk.Path, "reason", sk.Reason)
	}
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
