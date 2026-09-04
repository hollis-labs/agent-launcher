import { useEffect, useMemo, useRef, useState } from "react";
import { Binding, Launch, Shell } from "./bridge.js";

// The palette lists the active bundle's bindings, filterable by name, and
// lets the user move a selection over them with the mouse or the arrow
// keys. Enter resolves the active row's binding through
// internal/launch.Service.Launch — runs `cairn boot` for it and spawns
// iTerm2 on the result (CW-20260903-0016) — and dismisses the palette on
// success; a launch failure is shown inline instead, and the palette stays
// open so the user can try again. See onKeyDown.
// The compose form (CW-20260903-0017) and saving a composition as a new
// binding (CW-20260903-0018) are further out still; this window only reads
// bindings, it does not build or edit compositions.
//
// # Three empty states, not one (CW-20260904-0002 / T23)
//
// Binding.List() resolves to a [ListResult]-shaped object — see
// internal/binding.Service.List's own doc — not a bare array:
// { bindings, state, path, detail }, state one of "ok" | "missing" |
// "unreadable". Before T23, an empty bundle, a wrong bundle root, and a
// bundle whose bindings could not be read all rendered the same
// unconditional "No bindings yet" — the exact falsely-reassuring copy that
// had Chrispian asking whether he was *supposed* to have bindings when the
// real answer was "this build can't read them." listResult below carries
// enough for renderBody to tell all three apart and say which one is
// actually true, naming the bundle path in the two states that are real
// problems rather than a genuinely empty bundle.
export default function Palette() {
  // null = still loading. Once settled, either the resolved ListResult
  // ({ bindings, state, path, detail }) or a synthetic { state: "error" }
  // for the one case that isn't one of the three documented states: the
  // active bundle root itself could not even be resolved (Binding.List()
  // rejects rather than resolving — see internal/binding.Service.List's
  // doc on when it returns a non-nil error instead of State).
  const [listResult, setListResult] = useState(null);
  const [launchError, setLaunchError] = useState("");
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef(null);

  useEffect(() => {
    document.body.classList.add("palette");
  }, []);

  useEffect(() => {
    let cancelled = false;
    Binding.List()
      .then((result) => {
        if (!cancelled) setListResult(result ?? { bindings: [], state: "ok", path: "" });
      })
      .catch((err) => {
        if (!cancelled) setListResult({ state: "error", detail: String(err?.message ?? err) });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const bindings = listResult?.state === "ok" ? listResult.bindings ?? [] : [];

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return bindings;
    return bindings.filter((b) => b.name.toLowerCase().includes(q));
  }, [bindings, query]);

  // The active row must stay in range as filtering shrinks or reorders the
  // list — an index left pointing past the end, or at a row that scrolled
  // out from under it, is worse than resetting to the top.
  useEffect(() => {
    setActiveIndex((i) => (filtered.length === 0 ? 0 : Math.min(i, filtered.length - 1)));
  }, [filtered.length]);

  function onKeyDown(e) {
    if (filtered.length === 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => (i + 1) % filtered.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => (i - 1 + filtered.length) % filtered.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const target = filtered[activeIndex];
      if (!target) return;
      setLaunchError("");
      // Fire-and-forget from the palette's own point of view too: Launch
      // resolves once iTerm2 has been asked to open, not once a session is
      // running inside it — internal/launch.Service holds no handle on
      // what it started (D7), so there is nothing further to await here.
      // On success, pick/launch/vanish (§1); on failure, stay open and
      // show why, rather than dismissing on a launch that didn't happen.
      Launch.Binding(target.name)
        .then(() => Shell.HidePalette())
        .catch((err) => setLaunchError(String(err?.message ?? err)));
    }
  }

  return (
    <div className="palette-shell">
      <input
        ref={inputRef}
        placeholder="Search bindings…"
        autoFocus
        spellCheck={false}
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setLaunchError("");
        }}
        onKeyDown={onKeyDown}
      />

      {launchError ? (
        <div className="err" style={{ padding: "8px 14px" }}>
          Couldn't launch: {launchError}
        </div>
      ) : null}

      {renderBody(listResult, filtered, activeIndex, setActiveIndex)}

      {/* Persistent across every state (loading/error/missing/unreadable/
          empty/populated) — CW-20260904-0004. Before this, "Open manager"
          only lived inside the empty/error placeholders (see git history at
          e71f153 and the commit that first rendered a populated <ul>), so a
          palette with bindings in it had no path to the manager at all.
          This footer is the one affordance now; the placeholders above no
          longer duplicate it. */}
      <div className="palette-footer">
        <span className="muted">
          <kbd>Esc</kbd> dismisses · clicking away dismisses
        </span>
        <button type="button" onClick={() => Shell.OpenManager()}>
          Open manager
        </button>
      </div>
    </div>
  );
}

// renderBody picks one of the palette's states. listResult is null while
// still loading; once settled it is either the real ListResult
// ({ bindings, state: "ok"|"missing"|"unreadable", path, detail }) or the
// synthetic { state: "error", detail } Palette sets when Binding.List()
// itself rejected. Each non-"ok" state gets its own honest copy, per this
// task's own acceptance table — none of them collapse into "No bindings
// yet", which is reserved for the one state where that is actually true.
function renderBody(listResult, filtered, activeIndex, setActiveIndex) {
  if (listResult === null) {
    return <div className="placeholder muted">Loading bindings…</div>;
  }

  if (listResult.state === "error") {
    return (
      <div className="placeholder">
        <div>
          <div className="err">Couldn't load bindings</div>
          <div className="muted" style={{ marginTop: 8 }}>{listResult.detail}</div>
        </div>
      </div>
    );
  }

  if (listResult.state === "missing") {
    return (
      <div className="placeholder">
        <div>
          <div className="err">No bindings/ directory found</div>
          <div className="muted" style={{ marginTop: 8 }}>
            Nothing exists at <code>{listResult.path}</code>. Check that this is the right
            bundle, or that it's been set up with Cairn.
          </div>
        </div>
      </div>
    );
  }

  if (listResult.state === "unreadable") {
    return (
      <div className="placeholder">
        <div>
          <div className="err">Couldn't read bindings</div>
          <div className="muted" style={{ marginTop: 8 }}>{listResult.detail}</div>
        </div>
      </div>
    );
  }

  // state === "ok" from here down: a real, resolved bindings/ directory —
  // possibly genuinely empty, which is the one case "No bindings yet" is
  // actually true.
  if (filtered.length === 0) {
    return (
      <div className="placeholder">
        <div className="muted">
          {(listResult.bindings ?? []).length === 0 ? "No bindings yet" : "No bindings match your search"}
        </div>
      </div>
    );
  }

  return (
    <ul className="palette-list" role="listbox">
      {filtered.map((b, i) => (
        <li key={b.name}>
          <button
            type="button"
            role="option"
            aria-selected={i === activeIndex}
            className={"palette-row" + (i === activeIndex ? " active" : "")}
            onMouseEnter={() => setActiveIndex(i)}
            onClick={() => setActiveIndex(i)}
          >
            <span className="palette-row-name">{b.name}</span>
            <span className="palette-row-profile">{b.profile}</span>
            <span className="palette-row-scope">{b.scope}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}
