import { useEffect } from "react";
import { Shell } from "./bridge.js";

// The palette is deliberately empty. Bindings, filtering and the compose form
// are CW-20260903-0011 / -0017; this task ships the window posture only.
//
// No hotkey echo here on purpose: the accelerator is settings-page material,
// not palette material — this window is shown/hidden rather than reloaded, so
// a value fetched once on mount goes stale the moment the user rebinds
// elsewhere. "Currently bound" already lives correctly in Settings.
export default function Palette() {
  useEffect(() => {
    document.body.classList.add("palette");
  }, []);

  return (
    <div className="palette-shell">
      <input placeholder="Search bindings…" autoFocus spellCheck={false} />
      <div className="placeholder">
        <div>
          <div className="ok">⌁ Tachyon palette</div>
          <div style={{ marginTop: 10 }}>
            <kbd>Esc</kbd> dismisses · clicking away dismisses
          </div>
          <div style={{ marginTop: 16 }}>
            <button onClick={() => Shell.OpenManager()}>Open manager</button>
          </div>
        </div>
      </div>
    </div>
  );
}
