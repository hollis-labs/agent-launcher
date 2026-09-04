import { useEffect, useMemo, useRef, useState } from "react";
import { Binding, Launch, Shell } from "./bridge.js";

// The palette lists the active bundle's bindings, filterable by name, and
// lets the user move a selection over them with the mouse or the arrow
// keys. Below the list is the compose form (CW-20260903-0017): skills to
// ADD, a scope override, one-off --set values and additional --with parts,
// all layered on top of whichever binding is currently highlighted. Enter
// (from the search box, or from an empty compose field -- see
// attemptLaunch and each field's own onKeyDown) resolves the highlighted
// binding plus whatever has been composed through
// internal/launch.Service.LaunchComposition -- runs `cairn boot` for it
// and spawns iTerm2 on the result (CW-20260903-0016) -- and dismisses the
// palette on success; a launch failure is shown inline instead, and the
// palette stays open so the user can try again. Escape dismisses the
// window at the native layer (paletteOptions' HideOnEscape, internal/shell)
// without this component doing anything at all -- nothing here calls
// Launch.* in response to Escape, so a compose-in-progress is safe to
// abandon that way.
//
// Saving a composition as a new binding (CW-20260903-0018) is further out
// still; this window composes and launches, it does not persist a
// composition as a binding of its own.
//
// # THE CORRECTNESS PROPERTY: skills are additive only, never inherited
//
// `skills` below is a plain array of strings the user typed, starting
// empty on every mount (useState([])) and growing ONLY through
// addSkill/removeSkill, both wired only to direct user actions (the
// skills draft input's Enter, and a chip's own remove button). Nothing in
// this file ever calls setSkills from a binding, a profile, or any other
// data this window reads -- and there would be nothing to seed it from
// even if something tried: the ListResult Binding.List() resolves to
// carries only { name, profile, scope } per binding (internal/binding.Binding
// has no skills field at all -- see internal/launch's own package doc and
// TestBindingCarriesNoSkillsFieldToSeedFrom). Cairn's own --skill flag is
// additive only (nothing in cairn removes a member of a collection keyed
// by its own id), so a control that looked pre-checked with a profile's
// existing skills would let a person "uncheck" one and silently get it
// anyway -- a wrong result that looks right. The label on the skills field
// below says "Add skills for this launch" for exactly this reason: it is
// never a picture of what the target already has.
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
  const [launching, setLaunching] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef(null);

  // --- compose-form draft state (CW-20260903-0017) -----------------------
  // One shared draft, layered on top of whichever row is highlighted below
  // — not per-binding state — since these are extra flags added to
  // whatever target is chosen at launch time, not a property of any one
  // row. See this file's own header comment for why `skills` in particular
  // must never be seeded from anything but addSkill.
  const [skills, setSkills] = useState([]); // string[] — additive only
  const [skillDraft, setSkillDraft] = useState("");
  const [scope, setScope] = useState(""); // "" omits --scope entirely
  const [parts, setParts] = useState([]); // string[] — additional --with values
  const [partDraft, setPartDraft] = useState("");
  const [sets, setSets] = useState([]); // {slot, value}[] — one --set per entry
  const [setSlotDraft, setSetSlotDraft] = useState("");
  const [setValueDraft, setSetValueDraft] = useState("");

  useEffect(() => {
    document.body.classList.add("palette");
  }, []);

  useEffect(() => {
    let cancelled = false;
    // Named rather than inlined so a later window-focus refetch (T28,
    // concurrent in a sibling worktree as of this writing — see
    // Manager.jsx's own `loadTree` for the pattern it follows there) is a
    // small, additive diff: call loadBindings from a second effect that
    // adds a "focus" listener, rather than restructuring this one.
    const loadBindings = () => {
      Binding.List()
        .then((result) => {
          if (!cancelled) setListResult(result ?? { bindings: [], state: "ok", path: "" });
        })
        .catch((err) => {
          if (!cancelled) setListResult({ state: "error", detail: String(err?.message ?? err) });
        });
    };
    loadBindings();
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

  const activeTarget = filtered[activeIndex] ?? null;

  // resetComposeDraft clears every compose-form field back to its initial,
  // empty state. Called from two places: attemptLaunch's success path
  // (below — a spent composition must not silently reapply to a later
  // launch), and the window "blur" listener just below this component's
  // other effects (an *abandoned* composition — Escape, or clicking away —
  // must not silently survive to reapply to a completely different binding
  // the next time the palette is summoned).
  //
  // Why "blur" specifically: this window is opened with HideOnFocusLost —
  // see internal/shell/shell.go's paletteOptions — so for THIS window,
  // losing focus and being hidden are the same event, by construction, not
  // a heuristic. Wails routes HideOnFocusLost through window.Hide()
  // (pkg/application/webview_window.go's setupHideOnFocusLost), the exact
  // same call Escape's own key binding makes; ordering a window out also
  // resigns its key/focus status as a side effect, so the DOM "blur" this
  // listens for should fire for both dismiss paths, not just the
  // focus-loss one. This is inferred from Wails' own source, not observed
  // in a running window — there is no way to watch a live macOS window
  // from here. If "blur" ever turns out not to fire for one of these
  // paths, the practical consequence is exactly the pre-existing bug this
  // is fixing (a stale draft can reach a later launch), not a new
  // regression — so this is a strict improvement even if imperfect.
  function resetComposeDraft() {
    setSkills([]);
    setSkillDraft("");
    setScope("");
    setParts([]);
    setPartDraft("");
    setSets([]);
    setSetSlotDraft("");
    setSetValueDraft("");
  }

  useEffect(() => {
    window.addEventListener("blur", resetComposeDraft);
    return () => window.removeEventListener("blur", resetComposeDraft);
  }, []);

  // attemptLaunch resolves the highlighted binding plus the full compose
  // draft through Launch.Composition. It is the one place this component
  // calls Launch.* — every field's Enter handler either commits pending
  // text into the draft (skills/parts/sets — see each control below) or,
  // when there is nothing pending to commit, falls through to this
  // function, so "press Enter" always either builds the composition
  // further or launches it, never both at once and never something a
  // person did not ask for.
  function attemptLaunch() {
    if (launching) return;
    const target = activeTarget;
    if (!target) return;
    setLaunchError("");
    setLaunching(true);
    // Fire-and-forget from the palette's own point of view too:
    // LaunchComposition resolves once iTerm2 has been asked to open, not
    // once a session is running inside it — internal/launch.Service holds
    // no handle on what it started (D7), so there is nothing further to
    // await here. On success, pick/compose/launch/vanish; on failure, stay
    // open and show why, rather than dismissing on a launch that didn't
    // happen, and leave the draft exactly as it was so the person can fix
    // whatever cairn's stderr says and try again without retyping it.
    Launch.Composition({
      target: target.name,
      skills,
      scope: scope.trim(),
      sets,
      parts,
    })
      .then(() => {
        // A spent composition must not silently reapply to whichever
        // binding happens to be highlighted the next time the palette is
        // summoned. (Shell.HidePalette below will also trigger the "blur"
        // listener's own resetComposeDraft call once the window actually
        // hides — this explicit call is not redundant with that, it's what
        // makes the fields visibly clear immediately, without waiting on
        // the hide round-trip.)
        resetComposeDraft();
        return Shell.HidePalette();
      })
      .catch((err) => setLaunchError(String(err?.message ?? err)))
      .finally(() => setLaunching(false));
  }

  // addSkills splits raw on commas (--skill's own "comma-separated and
  // repeatable, the two forms equivalent" — accepting either shape here is
  // just ergonomics; internal/compose.Build joins the whole slice back
  // into one --skill flag regardless), trims, drops anything empty or
  // already present, and appends the rest — the ONLY function in this file
  // that ever grows `skills`.
  function addSkills(raw) {
    const additions = raw
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s !== "" && !skills.includes(s));
    if (additions.length === 0) return;
    setSkills((cur) => [...cur, ...additions]);
  }
  function removeSkill(name) {
    setSkills((cur) => cur.filter((s) => s !== name));
  }

  function addPart(raw) {
    const value = raw.trim();
    if (value === "" || parts.includes(value)) return;
    setParts((cur) => [...cur, value]);
  }
  function removePart(value) {
    setParts((cur) => cur.filter((p) => p !== value));
  }

  function addSet(slot, value) {
    const s = slot.trim();
    const v = value.trim();
    if (s === "" || v === "") return;
    setSets((cur) => [...cur, { slot: s, value: v }]);
  }
  function removeSet(index) {
    setSets((cur) => cur.filter((_, i) => i !== index));
  }

  function onSearchKeyDown(e) {
    if (e.key === "ArrowDown") {
      if (filtered.length === 0) return;
      e.preventDefault();
      setActiveIndex((i) => (i + 1) % filtered.length);
    } else if (e.key === "ArrowUp") {
      if (filtered.length === 0) return;
      e.preventDefault();
      setActiveIndex((i) => (i - 1 + filtered.length) % filtered.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      attemptLaunch();
    }
  }

  // onDraftEnter is the shared shape for every compose field's own Enter
  // key: if there is pending text, commit() it and stop there (never
  // launch on the same keystroke that just added a chip); if the field is
  // empty, Enter falls through to attemptLaunch() — so "press Enter" from
  // inside any compose field either advances the composition or launches
  // it, and pressing it once more after committing always launches.
  function onDraftEnter(e, commit) {
    if (e.key !== "Enter") return;
    e.preventDefault();
    if (commit()) return;
    attemptLaunch();
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
        onKeyDown={onSearchKeyDown}
      />

      {launchError ? (
        <div className="err" style={{ padding: "8px 14px" }}>
          Couldn't launch: {launchError}
        </div>
      ) : null}

      <div className="palette-scroll">
        {renderBody(listResult, filtered, activeIndex, setActiveIndex)}

        {activeTarget ? (
          <ComposeSection
            targetName={activeTarget.name}
            skills={skills}
            skillDraft={skillDraft}
            setSkillDraft={setSkillDraft}
            addSkills={addSkills}
            removeSkill={removeSkill}
            scope={scope}
            setScope={setScope}
            parts={parts}
            partDraft={partDraft}
            setPartDraft={setPartDraft}
            addPart={addPart}
            removePart={removePart}
            sets={sets}
            setSlotDraft={setSlotDraft}
            setSetSlotDraft={setSetSlotDraft}
            setValueDraft={setValueDraft}
            setSetValueDraft={setSetValueDraft}
            addSet={addSet}
            removeSet={removeSet}
            onDraftEnter={onDraftEnter}
            attemptLaunch={attemptLaunch}
          />
        ) : null}
      </div>

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

// ComposeSection is the compose form itself: one control per Cairn flag
// (T08, CW-20260903-0012) beyond the target already chosen from the list
// above — skills to ADD, a scope override, repeatable --set slot=value
// pairs, and repeatable additional --with parts. There is deliberately no
// template control here — template choice is authoring-time only (D4) and
// contributes nothing to a composition. Ordering carries no meaning: chips
// render in insertion order purely because that's the natural order for a
// list, not because position means anything to Cairn (resolution is
// deterministic by key) — nothing here lets a person reorder one.
function ComposeSection(props) {
  const {
    targetName,
    skills,
    skillDraft,
    setSkillDraft,
    addSkills,
    removeSkill,
    scope,
    setScope,
    parts,
    partDraft,
    setPartDraft,
    addPart,
    removePart,
    sets,
    setSlotDraft,
    setSetSlotDraft,
    setValueDraft,
    setSetValueDraft,
    addSet,
    removeSet,
    onDraftEnter,
    attemptLaunch,
  } = props;

  return (
    <div className="compose-section">
      <div className="compose-heading">
        Compose additions for <span className="compose-target">{targetName}</span>
      </div>

      {/* Skills: additive-only. The label says "Add", never "Skills" alone
          — see this file's header comment for why that wording is load
          bearing, not decoration. */}
      <div className="compose-field">
        <label htmlFor="compose-skill-input">
          Add skills for this launch <span className="muted">(on top of the profile's own — nothing here can remove one)</span>
        </label>
        <input
          id="compose-skill-input"
          placeholder="skill id, comma-separated — Enter to add"
          spellCheck={false}
          value={skillDraft}
          onChange={(e) => setSkillDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Backspace" && skillDraft === "" && skills.length > 0) {
              removeSkill(skills[skills.length - 1]);
              return;
            }
            onDraftEnter(e, () => {
              if (skillDraft.trim() === "") return false;
              addSkills(skillDraft);
              setSkillDraft("");
              return true;
            });
          }}
        />
        {skills.length > 0 ? (
          <div className="compose-chips">
            {skills.map((s) => (
              <span className="compose-chip" key={s}>
                {s}
                <button type="button" onClick={() => removeSkill(s)} title={`Remove ${s}`}>
                  ×
                </button>
              </span>
            ))}
          </div>
        ) : (
          <div className="compose-empty muted">No skills added — this launch gets only what the profile already resolves to.</div>
        )}
      </div>

      {/* Scope override: a plain value, bound directly — nothing to
          "commit" separately, so its own Enter goes straight to
          attemptLaunch. */}
      <div className="compose-field">
        <label htmlFor="compose-scope-input">Scope override</label>
        <input
          id="compose-scope-input"
          placeholder="(leave empty to use the binding's own scope)"
          spellCheck={false}
          value={scope}
          onChange={(e) => setScope(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              attemptLaunch();
            }
          }}
        />
      </div>

      {/* Additional --with parts. */}
      <div className="compose-field">
        <label htmlFor="compose-part-input">Additional parts (--with)</label>
        <input
          id="compose-part-input"
          placeholder="a catalog id or path — Enter to add"
          spellCheck={false}
          value={partDraft}
          onChange={(e) => setPartDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Backspace" && partDraft === "" && parts.length > 0) {
              removePart(parts[parts.length - 1]);
              return;
            }
            onDraftEnter(e, () => {
              if (partDraft.trim() === "") return false;
              addPart(partDraft);
              setPartDraft("");
              return true;
            });
          }}
        />
        {parts.length > 0 ? (
          <div className="compose-chips">
            {parts.map((p) => (
              <span className="compose-chip" key={p}>
                {p}
                <button type="button" onClick={() => removePart(p)} title={`Remove ${p}`}>
                  ×
                </button>
              </span>
            ))}
          </div>
        ) : null}
      </div>

      {/* One-off --set slot=value overrides. */}
      <div className="compose-field">
        <label htmlFor="compose-set-slot-input">Set overrides (--set slot=value)</label>
        <div className="compose-row">
          <input
            id="compose-set-slot-input"
            placeholder="slot"
            spellCheck={false}
            value={setSlotDraft}
            onChange={(e) => setSetSlotDraft(e.target.value)}
            onKeyDown={(e) =>
              onDraftEnter(e, () => {
                if (setSlotDraft.trim() === "" || setValueDraft.trim() === "") return false;
                addSet(setSlotDraft, setValueDraft);
                setSetSlotDraft("");
                setSetValueDraft("");
                return true;
              })
            }
          />
          <span className="compose-row-eq">=</span>
          <input
            placeholder="value"
            spellCheck={false}
            value={setValueDraft}
            onChange={(e) => setSetValueDraft(e.target.value)}
            onKeyDown={(e) =>
              onDraftEnter(e, () => {
                if (setSlotDraft.trim() === "" || setValueDraft.trim() === "") return false;
                addSet(setSlotDraft, setValueDraft);
                setSetSlotDraft("");
                setSetValueDraft("");
                return true;
              })
            }
          />
        </div>
        {sets.length > 0 ? (
          <div className="compose-chips">
            {sets.map((s, i) => (
              <span className="compose-chip" key={`${s.slot}=${s.value}-${i}`}>
                {s.slot}={s.value}
                <button type="button" onClick={() => removeSet(i)} title={`Remove ${s.slot}`}>
                  ×
                </button>
              </span>
            ))}
          </div>
        ) : null}
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
