package shell

import (
	"fmt"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// modifierNames mirrors the modifier table in Wails' pkg/application/keys.go.
// It is duplicated rather than imported because parseAccelerator and
// modifierMap are unexported; the copy is used only to reject accelerators
// Register would silently accept (see ValidateAccelerator).
var modifierNames = map[string]struct{}{
	"cmdorctrl": {}, "cmd": {}, "command": {},
	"ctrl":        {},
	"optionoralt": {}, "alt": {}, "option": {},
	"shift": {},
	"super": {},
}

// reservedAccelerators are combinations macOS itself claims by default. Binding
// one is not an error — the user may have turned the system shortcut off — but
// it is worth saying out loud, because the OS will not.
//
// This list is advisory and deliberately short. There is no API that answers
// "who owns this combination", so a long list would only look authoritative.
var reservedAccelerators = map[string]string{
	"CMD+SPACE":        "macOS Spotlight",
	"CMD+OPTION+SPACE": "macOS Finder search",
	"CMD+TAB":          "macOS application switcher",
	"CTRL+SPACE":       "macOS input-source switcher",
	"CMD+SHIFT+SPACE":  "commonly taken by 1Password Quick Access",
}

// ValidateAccelerator rejects accelerator strings that Register would accept
// but that no user wants bound.
//
// The load-bearing case is a bare key. Measured against Wails v3.0.0-beta.16:
// Register("K") returns a nil error and binds the unmodified K system-wide,
// which swallows that key in every application. parseAccelerator treats a
// single component as a key with no modifiers and reports no error, so nothing
// downstream catches it. A rebinding UI has to.
func ValidateAccelerator(accelerator string) error {
	s := strings.TrimSpace(accelerator)
	if s == "" {
		return fmt.Errorf("hotkey is empty")
	}
	components := strings.Split(s, "+")
	if len(components) < 2 {
		return fmt.Errorf(
			"hotkey %q has no modifier; a bare key would be captured in every application",
			accelerator)
	}
	for _, c := range components[:len(components)-1] {
		if _, ok := modifierNames[strings.ToLower(strings.TrimSpace(c))]; !ok {
			return fmt.Errorf("%q is not a modifier (use Cmd, Ctrl, Option, Shift or Super)", c)
		}
	}
	if strings.TrimSpace(components[len(components)-1]) == "" {
		return fmt.Errorf("hotkey %q has no key", accelerator)
	}
	return nil
}

// AdvisoryFor returns a non-empty string when an accelerator is known to be
// claimed by something else. It is guidance, not detection: registration
// succeeds against a conflict and reports nothing, so this is the only warning
// that can be given, and it is necessarily incomplete.
func AdvisoryFor(accelerator string) string {
	owner, ok := reservedAccelerators[canonicalish(accelerator)]
	if !ok {
		return ""
	}
	return fmt.Sprintf(
		"%s is normally %s. It may register successfully and never fire — press it to check.",
		accelerator, owner)
}

// canonicalish normalises an accelerator for advisory lookup: modifiers sorted
// in a fixed order, everything upper-cased. It is not Wails' canonical form
// (that is unexported) and is used only as a map key.
func canonicalish(accelerator string) string {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(accelerator)), "+")
	if len(parts) < 2 {
		return strings.ToUpper(strings.TrimSpace(accelerator))
	}
	order := []string{"CMD", "CMDORCTRL", "COMMAND", "CTRL", "OPTION", "OPTIONORALT", "ALT", "SHIFT", "SUPER"}
	rank := func(m string) int {
		for i, o := range order {
			if m == o {
				return i
			}
		}
		return len(order)
	}
	mods := parts[:len(parts)-1]
	for i := range mods {
		switch mods[i] {
		case "COMMAND", "CMDORCTRL":
			mods[i] = "CMD"
		case "ALT", "OPTIONORALT":
			mods[i] = "OPTION"
		}
	}
	for i := 1; i < len(mods); i++ {
		for j := i; j > 0 && rank(mods[j]) < rank(mods[j-1]); j-- {
			mods[j], mods[j-1] = mods[j-1], mods[j]
		}
	}
	return strings.Join(append(mods, parts[len(parts)-1]), "+")
}

// hotkeyBinder owns the single global shortcut that summons the palette.
//
// Every method must run OFF the main thread. GlobalShortcutManager.Register
// and Unregister both go through InvokeSyncWithError, which dispatches to the
// main queue and waits — calling either from the main thread deadlocks.
type hotkeyBinder struct {
	shortcuts *application.GlobalShortcutManager
	fire      func()

	mu    sync.Mutex
	bound string // the accelerator currently bound with the OS, "" if none
}

func newHotkeyBinder(shortcuts *application.GlobalShortcutManager, fire func()) *hotkeyBinder {
	return &hotkeyBinder{shortcuts: shortcuts, fire: fire}
}

// bound returns the accelerator currently registered, or "" if none is.
func (h *hotkeyBinder) current() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bound
}

// bind registers accelerator, releasing whatever was bound before.
//
// On failure the previous binding is restored, so a rejected rebind from the
// settings pane leaves the user with a working hotkey rather than none.
//
// A nil return means the OS accepted the registration. It does NOT mean the
// hotkey will fire: a cross-process conflict is invisible here. See the
// package documentation.
func (h *hotkeyBinder) bind(accelerator string) error {
	if err := ValidateAccelerator(accelerator); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	previous := h.bound
	if previous == accelerator {
		return nil
	}
	if previous != "" {
		if err := h.shortcuts.Unregister(previous); err != nil {
			return fmt.Errorf("releasing %q: %w", previous, err)
		}
		h.bound = ""
	}
	if err := h.shortcuts.Register(accelerator, h.fire); err != nil {
		if previous != "" {
			// Best effort: put the old binding back so the app is still usable.
			if reErr := h.shortcuts.Register(previous, h.fire); reErr == nil {
				h.bound = previous
			}
		}
		return err
	}
	h.bound = accelerator
	return nil
}
