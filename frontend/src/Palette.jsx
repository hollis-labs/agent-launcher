import { useEffect, useMemo, useRef, useState } from "react";
import { Binding, Launch, Manager, Project, Shell } from "./bridge.js";
import { acceptTopSuggestion } from "./autocomplete.js";

// The palette lists the active bundle's bindings, filterable by name, and
// lets the user move a selection over them with the mouse or the arrow
// keys. The compose form has two explicit target modes: additions layered
// over the highlighted binding (CW-20260903-0017), or a one-time launch
// starting from a bare profile with no binding selected at all
// (CW-20260904-0029). Enter (from the search box, or from an empty compose
// field -- see attemptLaunch and each field's own onKeyDown) resolves that
// target plus whatever has been composed through
// internal/launch.Service.LaunchComposition -- runs `cairn boot` for it
// and spawns iTerm2 on the result (CW-20260903-0016) -- and dismisses the
// palette on success; a launch failure is shown inline instead, and the
// palette stays open so the user can try again. Escape dismisses the
// window at the native layer (paletteOptions' HideOnEscape, internal/shell)
// without this component doing anything at all -- nothing here calls
// Launch.* in response to Escape, so a compose-in-progress is safe to
// abandon that way.
//
// Binding authoring now exists in the manager (CW-20260904-0028), and only
// there. This window composes and launches; it never persists a composition
// as a binding of its own.
//
// # THE CORRECTNESS PROPERTY: skills (and prompts) are additive only, never
// inherited
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
// `prompts` (CW-20260904-0006) is built the identical way, through
// addPrompts/removePrompt only, for the identical reason: Cairn's own
// --prompt flag documents itself as "Additive only, for the reason --skill
// is", and internal/binding.Binding has no prompts field either -- see
// TestBindingCarriesNoPromptsFieldToSeedFrom. The prompts field's label
// says "Add prompts for this launch" to match.
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
  const baseProfileRef = useRef(null);
  const partDraftRef = useRef(null);

  // The palette reads suggestion values from the same active bundle tree
  // the manager shows and the same Tachyon Projects registry the manager
  // edits. They are hints only: every control below remains an ordinary
  // text input, so a value that is not in either source still flows to
  // Cairn unchanged (D8). Loading is deliberately independent from
  // Binding.List(): composing from a bare profile is most useful when the
  // bundle has no bindings at all, and a missing/unreadable bindings/
  // directory must not take this entry point down with it.
  const [catalogTree, setCatalogTree] = useState(null);
  const [projects, setProjects] = useState([]);
  const [suggestionErrors, setSuggestionErrors] = useState({});

  // --- compose-form draft state (CW-20260903-0017) -----------------------
  // One shared modal draft, layered on top of the binding captured when its
  // Compose button was clicked — not per-binding state — since these are
  // extra flags added at launch time, not a property of a row. See this
  // file's own header comment for why `skills` in particular must never be
  // seeded from anything but addSkill.
  const [skills, setSkills] = useState([]); // string[] — additive only
  const [skillDraft, setSkillDraft] = useState("");
  const [prompts, setPrompts] = useState([]); // string[] — additive only (CW-20260904-0006)
  const [promptDraft, setPromptDraft] = useState("");
  const [scope, setScope] = useState(""); // "" omits --scope entirely
  const [parts, setParts] = useState([]); // string[] — additional --with values
  const [partDraft, setPartDraft] = useState("");
  const [sets, setSets] = useState([]); // {slot, value}[] — one --set per entry
  const [setSlotDraft, setSetSlotDraft] = useState("");
  const [setValueDraft, setSetValueDraft] = useState("");

  // CW-20260904-0029 adds one explicit second target mode. false preserves
  // the existing highlighted-binding behavior; true means no binding is
  // selected and baseProfile is sent as CompositionInput.Target instead.
  const [bindingless, setBindingless] = useState(false);
  const [baseProfile, setBaseProfile] = useState("");
  const [composeOpen, setComposeOpen] = useState(false);
  const [composeBinding, setComposeBinding] = useState("");

  useEffect(() => {
    document.body.classList.add("palette");
  }, []);

  useEffect(() => {
    let cancelled = false;
    let requestId = 0;
    const loadSuggestions = () => {
      const myRequestId = ++requestId;

      // These calls intentionally settle independently. Project.List can
      // fail because binding associations are unreadable while Manager.Tree
      // still has perfectly good profile/skill/prompt suggestions (and vice
      // versa); neither failure is a reason to reject free-text composition.
      Manager.Tree()
        .then((tree) => {
          if (cancelled || myRequestId !== requestId) return;
          setCatalogTree(tree);
          setSuggestionErrors((cur) => ({ ...cur, catalog: "" }));
        })
        .catch((err) => {
          if (cancelled || myRequestId !== requestId) return;
          setCatalogTree(null);
          setSuggestionErrors((cur) => ({ ...cur, catalog: String(err?.message ?? err) }));
        });

      Project.List()
        .then((views) => {
          if (cancelled || myRequestId !== requestId) return;
          setProjects(views ?? []);
          setSuggestionErrors((cur) => ({ ...cur, projects: "" }));
        })
        .catch((err) => {
          if (cancelled || myRequestId !== requestId) return;
          setProjects([]);
          setSuggestionErrors((cur) => ({ ...cur, projects: String(err?.message ?? err) }));
        });
    };
    loadSuggestions();
    window.addEventListener("focus", loadSuggestions);
    return () => {
      cancelled = true;
      window.removeEventListener("focus", loadSuggestions);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    let requestId = 0;
    const load = () => {
      const myRequestId = ++requestId;
      Binding.List()
        .then((result) => {
          if (cancelled || myRequestId !== requestId) return;
          setListResult(result ?? { bindings: [], state: "ok", path: "" });
        })
        .catch((err) => {
          if (cancelled || myRequestId !== requestId) return;
          setListResult({ state: "error", detail: String(err?.message ?? err) });
        });
    };
    load();
    // The palette window's webview is never destroyed between summons --
    // TogglePalette only Hide()s/Show()s it (internal/shell/shell.go), so
    // without this a palette that had already loaded once would keep
    // showing whichever bundle was active the first time it loaded,
    // forever, even after the manager changed the active bundle root
    // (CW-20260904-0019). Re-reading on focus, the same trigger
    // Manager.jsx's Bundle() already uses for its own tree, is what makes
    // "no restart" true here too: Show() + Focus() (TogglePalette) fires a
    // real "focus" DOM event on this window's top-level browsing context
    // every time the palette is summoned, not just the first time.
    window.addEventListener("focus", load);
    return () => {
      cancelled = true;
      window.removeEventListener("focus", load);
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

  const catalog = useMemo(
    () => Object.fromEntries((catalogTree?.groups ?? []).map((group) => [group.kind, (group.nodes ?? []).map((node) => node.id)])),
    [catalogTree],
  );
  const profileSuggestions = catalog.profile ?? [];
  const skillSuggestions = catalog.skill ?? [];
  const promptSuggestions = catalog.prompt ?? [];
  const projectSuggestions = projects.map((view) => ({
    name: view.project.name,
    path: view.project.path,
  }));

  // resetComposeDraft clears every compose-form field back to its initial,
  // empty state. Called from attemptLaunch's success path (a spent
  // composition must not silently reapply), the window "blur" listener
  // just below (today's dismissal behavior), and the explicit start-from-
  // nothing action (T33 says that action begins a new draft).
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
    setLaunchError("");
    setSkills([]);
    setSkillDraft("");
    setPrompts([]);
    setPromptDraft("");
    setScope("");
    setParts([]);
    setPartDraft("");
    setSets([]);
    setSetSlotDraft("");
    setSetValueDraft("");
    setBaseProfile("");
    setBindingless(false);
    setComposeOpen(false);
    setComposeBinding("");
  }

  useEffect(() => {
    window.addEventListener("blur", resetComposeDraft);
    return () => window.removeEventListener("blur", resetComposeDraft);
  }, []);

  // attemptLaunch sends either the binding captured by the modal, the
  // highlighted binding used by search+Enter, or the explicitly entered
  // bare profile plus the full compose draft through Launch.Composition.
  // Every field's Enter handler either commits pending
  // text into the draft (skills/parts/sets — see each control below) or,
  // when there is nothing pending to commit, falls through to this
  // function, so "press Enter" always either builds the composition
  // further or launches it, never both at once and never something a
  // person did not ask for.
  function attemptLaunch() {
    if (launching) return;
    const target = bindingless ? baseProfile.trim() : composeOpen ? composeBinding : activeTarget?.name;
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
      target,
      skills,
      prompts,
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

  // A double-click is the fastest path through the palette and is kept
  // deliberately separate from composition: it launches the saved binding
  // by name through Launch.Binding and therefore cannot accidentally carry
  // a stale part, skill, prompt, scope, or set from the modal draft.
  function launchBinding(name) {
    if (launching || !name) return;
    setLaunchError("");
    setLaunching(true);
    Launch.Binding(name)
      .then(() => {
        resetComposeDraft();
        return Shell.HidePalette();
      })
      .catch((err) => setLaunchError(String(err?.message ?? err)))
      .finally(() => setLaunching(false));
  }

  function openBindingComposition(index, name) {
    resetComposeDraft();
    setActiveIndex(index);
    setComposeBinding(name);
    setComposeOpen(true);
    setLaunchError("");
    requestAnimationFrame(() => partDraftRef.current?.focus());
  }

  // This is a NEW draft, per T33: opening it clears any additions that were
  // being composed over a selected binding. resetComposeDraft still owns
  // today's dismissal behavior; T30 owns whether that function is called on
  // blur in the future, so this task does not choose draft persistence.
  function openBindinglessComposition() {
    resetComposeDraft();
    setBindingless(true);
    setComposeOpen(true);
    setLaunchError("");
    requestAnimationFrame(() => baseProfileRef.current?.focus());
  }

  function closeComposition() {
    resetComposeDraft();
    requestAnimationFrame(() => inputRef.current?.focus());
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

  // addPrompts is addSkills' exact mirror for --prompt (CW-20260904-0006):
  // same comma-split ergonomics, same additive-only guarantee, the ONLY
  // function in this file that ever grows `prompts`.
  function addPrompts(raw) {
    const additions = raw
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s !== "" && !prompts.includes(s));
    if (additions.length === 0) return;
    setPrompts((cur) => [...cur, ...additions]);
  }
  function removePrompt(name) {
    setPrompts((cur) => cur.filter((p) => p !== name));
  }

  function addPart(raw) {
    const value = raw.trim();
    if (value === "" || parts.includes(value)) return;
    setParts((cur) => [...cur, value]);
  }
  function removePart(value) {
    setParts((cur) => cur.filter((p) => p !== value));
  }
  function movePart(index, offset) {
    setParts((cur) => {
      const nextIndex = index + offset;
      if (nextIndex < 0 || nextIndex >= cur.length) return cur;
      const next = [...cur];
      [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
      return next;
    });
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

      <div className="palette-modebar">
        <span className="muted">Double-click a binding to launch it directly</span>
        <button type="button" onClick={openBindinglessComposition} disabled={launching}>
          New one-time composition
        </button>
      </div>

      {launchError && !composeOpen ? (
        <div className="err" style={{ padding: "8px 14px" }}>
          Couldn't launch: {launchError}
        </div>
      ) : null}

      <div className="palette-scroll">
        {renderBody(
          listResult,
          filtered,
          activeIndex,
          (index) => {
            setActiveIndex(index);
            setLaunchError("");
          },
          setActiveIndex,
          openBindingComposition,
          launchBinding,
          launching,
        )}
      </div>

      {composeOpen && (bindingless || composeBinding) ? (
        <ComposeSection
          bindingless={bindingless}
          targetName={bindingless ? baseProfile.trim() : composeBinding}
          baseProfile={baseProfile}
          setBaseProfile={setBaseProfile}
          baseProfileRef={baseProfileRef}
          partDraftRef={partDraftRef}
          profileSuggestions={profileSuggestions}
          skillSuggestions={skillSuggestions}
          promptSuggestions={promptSuggestions}
          projectSuggestions={projectSuggestions}
          suggestionErrors={suggestionErrors}
          launchError={launchError}
          launching={launching}
          skills={skills}
          skillDraft={skillDraft}
          setSkillDraft={setSkillDraft}
          addSkills={addSkills}
          removeSkill={removeSkill}
          prompts={prompts}
          promptDraft={promptDraft}
          setPromptDraft={setPromptDraft}
          addPrompts={addPrompts}
          removePrompt={removePrompt}
          scope={scope}
          setScope={setScope}
          parts={parts}
          partDraft={partDraft}
          setPartDraft={setPartDraft}
          addPart={addPart}
          removePart={removePart}
          movePart={movePart}
          sets={sets}
          setSlotDraft={setSlotDraft}
          setSetSlotDraft={setSetSlotDraft}
          setValueDraft={setValueDraft}
          setSetValueDraft={setSetValueDraft}
          addSet={addSet}
          removeSet={removeSet}
          onDraftEnter={onDraftEnter}
          acceptTopSuggestion={acceptTopSuggestion}
          attemptLaunch={attemptLaunch}
          closeComposition={closeComposition}
        />
      ) : null}

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

// ComposeSection is the compose form itself: a bare-profile target when T33
// mode is active, followed by one control per Cairn flag (T08,
// CW-20260903-0012; prompts added by CW-20260904-0006). It follows T32's
// profile -> ordered parts -> additive skills/prompts -> sets -> scope
// resolution stack. Parts can be reordered because each becomes an ordered
// --with flag. Skills and prompts stay additive-only; their chip order is
// insertion order, never an inherited selection. There is deliberately no
// template control — template choice is authoring-time only (D4) and
// contributes nothing to a composition.
function ComposeSection(props) {
  const {
    bindingless,
    targetName,
    baseProfile,
    setBaseProfile,
    baseProfileRef,
    partDraftRef,
    profileSuggestions,
    skillSuggestions,
    promptSuggestions,
    projectSuggestions,
    suggestionErrors,
    launchError,
    launching,
    skills,
    skillDraft,
    setSkillDraft,
    addSkills,
    removeSkill,
    prompts,
    promptDraft,
    setPromptDraft,
    addPrompts,
    removePrompt,
    scope,
    setScope,
    parts,
    partDraft,
    setPartDraft,
    addPart,
    removePart,
    movePart,
    sets,
    setSlotDraft,
    setSetSlotDraft,
    setValueDraft,
    setSetValueDraft,
    addSet,
    removeSet,
    onDraftEnter,
    acceptTopSuggestion,
    attemptLaunch,
    closeComposition,
  } = props;

  const modalRef = useRef(null);
  const trapModalTab = (event) => {
    if (
      event.key !== "Tab" || event.defaultPrevented || event.ctrlKey ||
      event.altKey || event.metaKey || event.isComposing ||
      event.nativeEvent?.isComposing
    ) return;
    const focusable = modalRef.current?.querySelectorAll(
      'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
    );
    if (!focusable?.length) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  return (
    <div
      className="compose-modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) closeComposition();
      }}
    >
      <section
        ref={modalRef}
        className="compose-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="compose-modal-title"
        onKeyDown={trapModalTab}
      >
        <header className="compose-modal-header">
          <div>
            <div className="compose-heading" id="compose-modal-title">
              {bindingless ? "One-time composition" : "Compose additions for"}
            </div>
            <div className="compose-modal-target">
              {bindingless ? targetName || "Choose a base profile" : targetName}
            </div>
          </div>
          <button type="button" className="compose-modal-close" onClick={closeComposition} aria-label="Close composition">
            ×
          </button>
        </header>

        <div className="compose-modal-body">
          {launchError ? <div className="err">Couldn't launch: {launchError}</div> : null}

          <div className="compose-section">

      {bindingless ? (
        <div className="compose-field compose-base-profile">
          <label htmlFor="compose-profile-input">
            Base profile <span className="muted">(suggestions only — free text works)</span>
          </label>
          <input
            id="compose-profile-input"
            ref={baseProfileRef}
            list="compose-profile-suggestions"
            placeholder="profile id"
            spellCheck={false}
            value={baseProfile}
            onChange={(e) => {
              setBaseProfile(e.target.value);
            }}
            onKeyDown={(e) => {
              if (acceptTopSuggestion(e, baseProfile, profileSuggestions, setBaseProfile)) return;
              if (e.key === "Enter") {
                e.preventDefault();
                attemptLaunch();
              }
            }}
          />
          <datalist id="compose-profile-suggestions">
            {profileSuggestions.map((profile) => <option key={profile} value={profile} />)}
          </datalist>
        </div>
      ) : null}

      {/* Additional --with parts. A Cairn profile can also be used as a
          composable part, so the active bundle's real profile IDs are the
          useful suggestions here. The input remains free text. */}
      <div className="compose-field">
        <label htmlFor="compose-part-input">Additional parts (--with)</label>
        <input
          id="compose-part-input"
          ref={partDraftRef}
          list="compose-part-suggestions"
          placeholder="a catalog id or path — Enter to add"
          spellCheck={false}
          value={partDraft}
          onChange={(e) => setPartDraft(e.target.value)}
          onKeyDown={(e) => {
            if (acceptTopSuggestion(e, partDraft, profileSuggestions, setPartDraft)) return;
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
        <datalist id="compose-part-suggestions">
          {profileSuggestions.map((part) => <option key={part} value={part} />)}
        </datalist>
        {parts.length > 0 ? (
          <div className="compose-chips">
            {parts.map((p, index) => (
              <span className="compose-chip" key={p}>
                {p}
                <button
                  type="button"
                  disabled={index === 0}
                  onClick={() => movePart(index, -1)}
                  title={`Move ${p} earlier`}
                >
                  ↑
                </button>
                <button
                  type="button"
                  disabled={index === parts.length - 1}
                  onClick={() => movePart(index, 1)}
                  title={`Move ${p} later`}
                >
                  ↓
                </button>
                <button type="button" onClick={() => removePart(p)} title={`Remove ${p}`}>
                  ×
                </button>
              </span>
            ))}
          </div>
        ) : null}
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
          list="compose-skill-suggestions"
          placeholder="skill id, comma-separated — Enter to add"
          spellCheck={false}
          value={skillDraft}
          onChange={(e) => setSkillDraft(e.target.value)}
          onKeyDown={(e) => {
            if (acceptTopSuggestion(e, skillDraft, skillSuggestions, setSkillDraft)) return;
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
        <datalist id="compose-skill-suggestions">
          {skillSuggestions.map((skill) => <option key={skill} value={skill} />)}
        </datalist>
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

      {/* Prompts: additive-only, the exact mirror of the skills field above
          (CW-20260904-0006) — mapped to --prompt exactly as skills maps to
          --skill. A prompt is a name Cairn resolves against prompts/, never
          content typed here: nothing in this form reads or shows a
          prompt's body. */}
      <div className="compose-field">
        <label htmlFor="compose-prompt-input">
          Add prompts for this launch <span className="muted">(planted as /boot:&lt;name&gt; — nothing here can remove one already declared)</span>
        </label>
        <input
          id="compose-prompt-input"
          list="compose-prompt-suggestions"
          placeholder="prompt name, comma-separated — Enter to add"
          spellCheck={false}
          value={promptDraft}
          onChange={(e) => setPromptDraft(e.target.value)}
          onKeyDown={(e) => {
            if (acceptTopSuggestion(e, promptDraft, promptSuggestions, setPromptDraft)) return;
            if (e.key === "Backspace" && promptDraft === "" && prompts.length > 0) {
              removePrompt(prompts[prompts.length - 1]);
              return;
            }
            onDraftEnter(e, () => {
              if (promptDraft.trim() === "") return false;
              addPrompts(promptDraft);
              setPromptDraft("");
              return true;
            });
          }}
        />
        <datalist id="compose-prompt-suggestions">
          {promptSuggestions.map((prompt) => <option key={prompt} value={prompt} />)}
        </datalist>
        {prompts.length > 0 ? (
          <div className="compose-chips">
            {prompts.map((p) => (
              <span className="compose-chip" key={p}>
                {p}
                <button type="button" onClick={() => removePrompt(p)} title={`Remove ${p}`}>
                  ×
                </button>
              </span>
            ))}
          </div>
        ) : (
          <div className="compose-empty muted">No prompts added — this launch gets only what the profile already declares.</div>
        )}
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

      {/* Scope maps directly to --scope. Project names never cross the
          launch boundary: selecting one copies its literal saved path into
          this ordinary free-text input, exactly like T32's composer. */}
      <div className="compose-field">
        <label htmlFor="compose-scope-input">
          Scope {bindingless ? "" : "override"}<span className="muted"> (literal path)</span>
        </label>
        <input
          id="compose-scope-input"
          list="compose-project-suggestions"
          placeholder={bindingless ? "project path (optional)" : "(leave empty to use the binding's own scope)"}
          spellCheck={false}
          value={scope}
          onChange={(e) => setScope(e.target.value)}
          onKeyDown={(e) => {
            const suggestions = projectSuggestions.map((project) => ({
              value: project.path,
              search: [project.name],
            }));
            if (acceptTopSuggestion(e, scope, suggestions, setScope)) return;
            if (e.key === "Enter") {
              e.preventDefault();
              attemptLaunch();
            }
          }}
        />
        <datalist id="compose-project-suggestions">
          {projectSuggestions.map((project) => (
            <option key={project.name} value={project.path}>{project.name}</option>
          ))}
        </datalist>
      </div>

      {suggestionErrors.catalog || suggestionErrors.projects ? (
        <div className="compose-suggestion-error muted">
          Some suggestions are unavailable. Free text still works.
          {suggestionErrors.catalog ? <span> Bundle: {suggestionErrors.catalog}</span> : null}
          {suggestionErrors.projects ? <span> Projects: {suggestionErrors.projects}</span> : null}
        </div>
      ) : null}

          </div>
        </div>

        <footer className="compose-modal-footer">
          <span className="muted">Launch only — this palette never writes a binding.</span>
          <button type="button" onClick={attemptLaunch} disabled={launching || !targetName}>
            {launching ? "Launching…" : "Launch composition"}
          </button>
        </footer>
      </section>
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
function renderBody(listResult, filtered, activeIndex, onSelect, onHover, onCompose, onLaunch, launching) {
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
        <li key={b.name} className={"palette-row" + (i === activeIndex ? " active" : "")}>
          <button
            type="button"
            role="option"
            aria-selected={i === activeIndex}
            className="palette-row-main"
            onMouseEnter={() => onHover(i)}
            onClick={() => onSelect(i)}
            onDoubleClick={() => onLaunch(b.name)}
            disabled={launching}
          >
            <span className="palette-row-name">{b.name}</span>
            <span className="palette-row-profile">{b.profile}</span>
            <span className="palette-row-scope">{b.scope}</span>
          </button>
          <button
            type="button"
            className="palette-row-compose"
            onClick={() => onCompose(i, b.name)}
            disabled={launching}
          >
            Compose
          </button>
        </li>
      ))}
    </ul>
  );
}
