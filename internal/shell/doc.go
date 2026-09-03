// Package shell is Tachyon's window shell: two window classes with opposed
// postures behind one tray icon, plus the global hotkey that summons the
// palette.
//
// The postures are structural, not cosmetic (target architecture §4). The
// palette is frameless, sits above ordinary windows, joins every Space, and
// dismisses on Escape and on blur — a palette that lingers is not a palette.
// The manager is an ordinary resizable window that remembers its size and
// never dismisses on blur — an editor that vanishes when you click away is
// unusable. They cannot be one window in two modes, so they are two windows
// in one process.
//
// "Above ordinary windows" is deliberately not "floating": the palette is at
// NSPopUpMenuWindowLevel (101), not the floating level (3) that §4 names, and
// paletteOptions explains why. What puts it over a fullscreen application is
// not the level at all but CollectionBehavior — CanJoinAllSpaces with
// FullScreenAuxiliary. Measured with four panels at levels 3 and 101, with
// and without those attributes, against a fullscreened application: only the
// two carrying the collection behaviour appeared on the fullscreen Space, and
// both of them did, at either level.
//
// The palette can open the manager. The manager never becomes the palette.
//
// # Why this is a package and not a library
//
// D2a: go-launcher-ui, the folio wails-launcher preset and go-wails-kit are
// cancelled permanently. Tachyon is the only launcher UI that will exist, so
// the shell lives inside it. If a second consumer ever appears, extracting
// this package is a normal refactor informed by two real users.
//
// # The hotkey, and what registration does not tell you
//
// Global-shortcut registration on macOS cannot report a cross-process
// conflict. Measured on macOS 15.7.7 against Wails v3.0.0-beta.16
// (CW-20260903-0006): two processes registering the same accelerator both get
// a nil error and both fire; and an accelerator already consumed upstream by
// an event tap registers with a nil error and then never fires. Wails maps
// Carbon's -9878 to "already registered (possibly by another application)",
// but that branch does not fire cross-process — it only catches an in-process
// duplicate, which the Go-side map has already rejected.
//
// So a nil error from Register is never evidence that the hotkey works. The
// only workable answer is the one this package implements: the accelerator is
// runtime-configurable, persisted, and rebindable from the manager's settings
// pane without a rebuild.
//
// # Getting back in when the hotkey is dead
//
// Read this as part of the mitigation, not as a footnote to it. Rebinding is
// the whole answer to a silent collision, so how a user REACHES the rebinding
// UI is load-bearing — and if the hotkey is dead, the palette cannot be
// summoned, which removes one of the two ways in.
//
// The two ways in:
//
//  1. The tray icon. Left-click summons the palette; right-click opens a menu
//     with "Settings…", which opens the manager on its settings pane. This is
//     the intended route and it does not depend on the hotkey.
//
//  2. Editing the preferences file by hand. It lives at
//     internal/state.Root()/shell.json — on macOS that is
//     ~/Library/Application Support/Tachyon/shell.json — and the accelerator
//     is the top-level "hotkey" key, in Wails' accelerator spelling, e.g.
//     "Ctrl+Option+Space". Change it and restart Tachyon. A value that cannot
//     be bound is replaced with DefaultHotkey at load rather than leaving the
//     app with no hotkey (see Prefs.normalize), so a typo here costs a restart
//     and not the app.
//
// Route 2 exists because route 1 has a real dependency: Tachyon runs under
// ActivationPolicyAccessory, so there is no Dock icon and no application menu
// to fall back on, and a status item that fails to render — which has been
// observed on at least one machine, with a notched display and a menu-bar
// manager installed — takes the tray route with it. That failure is
// environmental rather than Tachyon's, but the recovery path should not
// depend on it being absent.
package shell
