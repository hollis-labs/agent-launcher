import { useEffect, useState } from "react";
import { Shell } from "./bridge.js";

// The palette is deliberately empty. Bindings, filtering and the compose form
// are CW-20260903-0011 / -0017; this task ships the window posture only.
export default function Palette() {
  const [hotkey, setHotkey] = useState("");

  useEffect(() => {
    document.body.classList.add("palette");
    Shell.Settings().then((s) => setHotkey(s.hotkey)).catch(() => {});
  }, []);

  return (
    <div className="palette-shell">
      <input placeholder="Search bindings…" autoFocus spellCheck={false} />
      <div className="placeholder">
        <div>
          <div className="ok">⌁ Tachyon palette</div>
          <div style={{ marginTop: 10 }}>
            {hotkey ? <>summoned by <kbd>{hotkey}</kbd></> : "…"}
            <br />
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
