import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { Launch, LaunchProfile, Manager, Project, Shell } from "./bridge.js";
import { acceptTopSuggestion } from "./autocomplete.js";
import EffectiveSkills from "./EffectiveSkills.jsx";
import {
  compositionDraftReducer,
  compositionInput,
  createCompositionDraft,
} from "./compositionDraft.js";

// The palette launches an agent. Three things make one, and they come from
// three places:
//
//   agent profile    the bundle          WHAT this is    the list below
//   launch profile   ~/.config/tachyon   HOW it runs     the top selector
//   project          Tachyon's own list  WHERE it works  the top selector
//
// All three are visible at once so the common launch is Enter on a row, and
// the compose modal is for one-off additions on top rather than for the
// basics. That split is the 2026-09-10 ruling (Tesseract
// cairn_is_a_template_engine_not_an_authority): agent-setup owns content
// and declares no runtime, cairn materializes a directory, the launcher owns
// everything about a launch.
//
// It used to list BINDINGS — a saved (profile, parts, skills, scope) tuple
// living in the bundle, in a format cairn could not read. agent-setup retired
// all 34 and cairn dropped bindings and --save-as, so what replaced them is
// not a smaller binding: it is the same facts, each owned by whoever knows
// it. See internal/launchprofile.
//
// # There is no provider control, and that is the point
//
// A provider used to be a free-text field rendering --provider. No profile in
// agent-setup declares a provider now and cairn refuses to render without
// one, so the provider is declared in the launch profile and folded in by
// cairn's own cascade. A control here would be a second source for a value a
// file already carries, and the two could disagree.
//
// The consequence is worth stating: a launch with no launch profile selected
// has no provider, and cairn refuses it. That is the intended shape rather
// than a gap — internal/launchprofile seeds a default so a first run has one.
//
// # THE CORRECTNESS PROPERTY: skills (and prompts) are additive only, never
// inherited
//
// `skills` in the composition state is a plain array of strings the user
// typed, starting empty with every new draft and growing ONLY through
// ADD_SKILLS/REMOVE_SKILL, both dispatched only by direct user actions (the
// skills draft input's Enter, and a chip's own remove button). Nothing in
// this file seeds skills from a profile, a launch profile, or any other data
// this window reads — and there would be nothing to seed it from even if
// something tried: launchprofile.Profile carries only
// { name, path, provider, description }, with no skills field at all (see
// internal/launch's own package doc and
// TestLaunchProfileCarriesNoSkillsFieldToSeedFrom). Cairn's own --skill flag
// is additive only (nothing in cairn removes a member of a collection keyed
// by its own id), so a control that looked pre-checked with a profile's
// existing skills would let a person "uncheck" one and silently get it
// anyway — a wrong result that looks right. The label on the skills field
// below says "Add skills for this launch" for exactly this reason: it is
// never a picture of what the target already has.
//
// `prompts` is built the identical way, through ADD_PROMPTS/REMOVE_PROMPT
// only, for the identical reason: cairn's own --prompt flag documents itself
// as "Additive only, for the reason --skill is". The prompts field's label
// says "Add prompts for this launch" to match.
//
// # Three empty states, not one (CW-20260904-0002 / T23)
//
// LaunchProfile.List() resolves to a ListResult-shaped object — see
// internal/launchprofile.Service.List's own doc — not a bare array:
// { profiles, state, path, detail }, state one of "ok" | "missing" |
// "unreadable". Before T23, an empty store, a wrong root, and a store that
// could not be read all rendered the same unconditional sentence — the exact
// falsely-reassuring copy that had Chrispian asking whether he was *supposed*
// to have bindings when the real answer was "this build can't read them."
//
// One of the three is now genuinely the common state: a fresh machine has
// written no launch profile. So "missing" invites rather than alarms, and
// the two that are real problems name the path.
export default function Palette() {
  // null = still loading. Once settled, either the resolved ListResult
  // ({ profiles, state, path, detail }) or a synthetic { state: "error" }
  // for the one case that isn't one of the three documented states.
  const [launchProfiles, setLaunchProfiles] = useState(null);
  const [targets, setTargets] = useState(null);
  const [targetsError, setTargetsError] = useState("");
  const [directLaunchError, setDirectLaunchError] = useState("");
  const [directLaunching, setDirectLaunching] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);

  // The two axes that are not the target. They live outside the compose
  // draft on purpose: a draft is one launch, and these persist across
  // launches the way a person's working context does. A new draft opens on
  // whatever is selected here.
  const [launchProfile, setLaunchProfile] = useState("");
  const [projectPath, setProjectPath] = useState("");

  const inputRef = useRef(null);
  const baseProfileRef = useRef(null);
  const partDraftRef = useRef(null);

  // Suggestion sources for the compose modal's free-text controls. They are
  // hints only: every control there remains an ordinary text input, so a
  // value in neither source still flows to cairn unchanged (D8). Loading is
  // deliberately independent of the two lists above — a bundle that cannot
  // be read must not take the launch-profile picker down with it, or the
  // reverse.
  const [catalogTree, setCatalogTree] = useState(null);
  const [projects, setProjects] = useState([]);
  const [suggestionErrors, setSuggestionErrors] = useState({});

  // The modal, its immutable target, and all launch-only fields form one
  // lifecycle. Native dismissal marks the open draft as retained; only an
  // explicit discard or successful composition launch consumes it.
  const [composition, dispatchComposition] = useReducer(
    compositionDraftReducer,
    undefined,
    createCompositionDraft,
  );
  // Set synchronously before crossing the Wails bridge. This closes the
  // small render gap where two Enter events could start the same draft, and
  // lets Discard invalidate an outstanding result before React rerenders.
  const activeCompositionLaunchRef = useRef(null);
  const {
    skills,
    skillDraft,
    prompts,
    promptDraft,
    launchProfile: composeLaunchProfile,
    scope,
    parts,
    partDraft,
    sets,
    setSlotDraft,
    setValueDraft,
    open: composeOpen,
    retained: composeRetained,
    target: composeTarget,
    targetDraft: baseProfile,
    launchError,
  } = composition;
  const typedTarget = composition.mode === "typed";
  const launching = directLaunching || composition.launching;

  const updateCompositionField = (field, value) => {
    dispatchComposition({ type: "UPDATE_FIELD", field, value });
  };
  const setBaseProfile = (value) => dispatchComposition({ type: "SET_TARGET_DRAFT", value });
  const setSkillDraft = (value) => updateCompositionField("skillDraft", value);
  const setPromptDraft = (value) => updateCompositionField("promptDraft", value);
  const setComposeLaunchProfile = (value) => updateCompositionField("launchProfile", value);
  const setScope = (value) => updateCompositionField("scope", value);
  const setPartDraft = (value) => updateCompositionField("partDraft", value);
  const setSetSlotDraft = (value) => updateCompositionField("setSlotDraft", value);
  const setSetValueDraft = (value) => updateCompositionField("setValueDraft", value);

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

      // The two lists settle independently, because they come from two
      // stores that fail for unrelated reasons: the bundle can be missing
      // while the launch store is fine, and the reverse. Neither failure is
      // a reason to take the other list down.
      Launch.Targets()
        .then((list) => {
          if (cancelled || myRequestId !== requestId) return;
          setTargets(list ?? []);
          setTargetsError("");
        })
        .catch((err) => {
          if (cancelled || myRequestId !== requestId) return;
          setTargets([]);
          setTargetsError(String(err?.message ?? err));
        });

      LaunchProfile.List()
        .then((result) => {
          if (cancelled || myRequestId !== requestId) return;
          setLaunchProfiles(result ?? { profiles: [], state: "ok", path: "" });
        })
        .catch((err) => {
          if (cancelled || myRequestId !== requestId) return;
          setLaunchProfiles({ state: "error", detail: String(err?.message ?? err) });
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

  const profiles = launchProfiles?.state === "ok" ? launchProfiles.profiles ?? [] : [];
  const targetList = targets ?? [];

  // Keep the selected launch profile pointing at something real. On first
  // load nothing is selected, so prefer one literally named "default" (the
  // seed) and otherwise take the first — a palette that opened with no
  // launch profile selected would refuse every launch for want of a
  // provider, which is a true diagnostic and a useless first impression.
  useEffect(() => {
    if (profiles.length === 0) return;
    if (profiles.some((p) => p.name === launchProfile)) return;
    setLaunchProfile((profiles.find((p) => p.name === "default") ?? profiles[0]).name);
  }, [profiles, launchProfile]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return targetList;
    // Description too, not just the id: the roles are told apart by what
    // they do far more readably than by their names.
    return targetList.filter((t) =>
      [t.id, t.name, t.description].some((field) => (field ?? "").toLowerCase().includes(q)),
    );
  }, [targetList, query]);

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

  // The palette puts the caret in the search box on every summon, so a
  // person can type immediately without clicking first.
  //
  // This window is created at app start and never unmounted: the hotkey and
  // the tray Show()/Hide() it. So React mounts ONCE, while the window is
  // hidden, and the search box's autoFocus fires there — into a window
  // nobody is looking at. Show() + Focus() fires a real "focus" DOM event on
  // this window's top-level browsing context every summon, the same event the
  // two load effects already use to re-read their lists, so that is where
  // this belongs too.
  //
  // It is a convenience and NOT what makes the keyboard work: the arrows and
  // Enter are bound to the window (see paletteKeyDown), precisely so they do
  // not depend on any one element holding focus.
  //
  // It defers to an open compose modal. A retained draft survives dismissal
  // (see the blur listener below), so a summon that lands back in an open
  // modal must not yank focus out of whatever field the person was typing
  // in and back to the search box.
  const composeOpenRef = useRef(false);
  composeOpenRef.current = composeOpen;
  useEffect(() => {
    const focusSearch = () => {
      if (composeOpenRef.current) return;
      inputRef.current?.focus();
      inputRef.current?.select();
    };
    focusSearch();
    window.addEventListener("focus", focusSearch);
    return () => window.removeEventListener("focus", focusSearch);
  }, []);

  // Escape, click-away, the global hotkey, and native focus loss all hide
  // this Wails window rather than unmounting it. A window blur therefore
  // marks an open modal as retained but deliberately leaves every draft
  // field and its target intact for the next summon.
  useEffect(() => {
    const retainOpenDraft = () => dispatchComposition({ type: "HIDE" });
    window.addEventListener("blur", retainOpenDraft);
    return () => window.removeEventListener("blur", retainOpenDraft);
  }, []);

  // attemptLaunch sends the target captured by the modal plus its full
  // compose draft through Launch.Composition. A one-time draft first uses
  // Enter (or the footer action) to capture its chosen base profile; that
  // target is immutable until the person explicitly discards the draft.
  // Every field's Enter handler either commits pending
  // text into the draft (skills/parts/sets — see each control below) or,
  // when there is nothing pending to commit, falls through to this
  // function, so "press Enter" always either builds the composition
  // further or launches it, never both at once and never something a
  // person did not ask for.
  function attemptLaunch() {
    if (launching || activeCompositionLaunchRef.current !== null) return;
    if (typedTarget && !composeTarget) {
      if (!baseProfile.trim()) return;
      dispatchComposition({ type: "CAPTURE_TARGET" });
      requestAnimationFrame(() => partDraftRef.current?.focus());
      return;
    }
    const input = compositionInput(composition);
    if (!input) return;
    const draftId = composition.draftId;
    activeCompositionLaunchRef.current = draftId;
    dispatchComposition({ type: "LAUNCH_START", draftId });
    // Fire-and-forget from the palette's own point of view too:
    // LaunchComposition resolves once iTerm2 has been asked to open, not
    // once a session is running inside it — internal/launch.Service holds
    // no handle on what it started (D7), so there is nothing further to
    // await here. On success, pick/compose/launch/vanish; on failure, stay
    // open and show why, rather than dismissing on a launch that didn't
    // happen, and leave the draft exactly as it was so the person can fix
    // whatever cairn's stderr says and try again without retyping it.
    Launch.Composition(input)
      .then(() => {
        // A person may explicitly discard while a launch is outstanding.
        // Its late completion must never clear a replacement draft or hide
        // the palette out from under it.
        if (activeCompositionLaunchRef.current !== draftId) return undefined;
        activeCompositionLaunchRef.current = null;
        dispatchComposition({ type: "LAUNCH_SUCCESS", draftId });
        return Shell.HidePalette();
      })
      .catch((err) => {
        if (activeCompositionLaunchRef.current !== draftId) return;
        activeCompositionLaunchRef.current = null;
        dispatchComposition({
          type: "LAUNCH_FAILURE",
          draftId,
          error: String(err?.message ?? err),
        });
      });
  }

  // Enter, or a double-click, is the fastest path through the palette: the
  // three axes are already chosen — the highlighted row, and the two
  // selectors above the list — so there is nothing left to fill in.
  //
  // It goes through Launch.Composition like everything else, carrying only
  // those three and nothing from the modal draft, so a direct launch can
  // never pick up a stale part, skill, prompt or set someone left open.
  function launchTarget(id) {
    if (launching || !id) return;
    setDirectLaunchError("");
    setDirectLaunching(true);
    Launch.Composition({
      target: id,
      launchProfile,
      scope: projectPath,
      skills: [],
      prompts: [],
      sets: [],
      parts: [],
    })
      .then(() => Shell.HidePalette())
      .catch((err) => setDirectLaunchError(String(err?.message ?? err)))
      .finally(() => setDirectLaunching(false));
  }

  // A new draft opens on whatever the two selectors say, so the modal is
  // for ADDITIONS rather than for re-entering the basics. Skills and
  // prompts are never seeded — see this file's header comment, and
  // compositionDraft.js's newDraft.
  const draftDefaults = () => ({ launchProfile, scope: projectPath });

  function openComposition(index, id) {
    setActiveIndex(index);
    setDirectLaunchError("");
    dispatchComposition({ type: "OPEN_TARGET", target: id, defaults: draftDefaults() });
    requestAnimationFrame(() => partDraftRef.current?.focus());
  }

  // A composition whose target is typed rather than picked. The state
  // machine refuses to replace an already-open draft; switching therefore
  // requires Discard & close.
  function openBlankComposition() {
    setDirectLaunchError("");
    dispatchComposition({ type: "OPEN_BLANK", defaults: draftDefaults() });
    requestAnimationFrame(() => baseProfileRef.current?.focus());
  }

  function discardComposition() {
    activeCompositionLaunchRef.current = null;
    dispatchComposition({ type: "DISCARD" });
    requestAnimationFrame(() => inputRef.current?.focus());
  }

  function clearComposition() {
    dispatchComposition({ type: "CLEAR" });
    requestAnimationFrame(() => (typedTarget && !composeTarget ? baseProfileRef : partDraftRef).current?.focus());
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
    dispatchComposition({ type: "ADD_SKILLS", values: additions });
  }
  function removeSkill(name) {
    dispatchComposition({ type: "REMOVE_SKILL", value: name });
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
    dispatchComposition({ type: "ADD_PROMPTS", values: additions });
  }
  function removePrompt(name) {
    dispatchComposition({ type: "REMOVE_PROMPT", value: name });
  }

  function addPart(raw) {
    const value = raw.trim();
    if (value === "" || parts.includes(value)) return;
    dispatchComposition({ type: "ADD_PART", value });
  }
  function removePart(value) {
    dispatchComposition({ type: "REMOVE_PART", value });
  }
  function movePart(index, offset) {
    dispatchComposition({ type: "MOVE_PART", index, offset });
  }

  function addSet(slot, value) {
    const s = slot.trim();
    const v = value.trim();
    if (s === "" || v === "") return;
    dispatchComposition({ type: "ADD_SET", value: { slot: s, value: v } });
  }
  function removeSet(index) {
    dispatchComposition({ type: "REMOVE_SET", index });
  }

  // paletteKeyDown is the palette's keyboard, and it is bound to the WINDOW
  // rather than to the search input.
  //
  // It used to be the input's own onKeyDown, and that made the primary action
  // depend on one element holding focus. On macOS, clicking a <button> does
  // not focus it — the platform convention — so a single click anywhere in
  // this window moves focus off the input and onto BODY, and from that moment
  // Enter reached nothing at all. Measured, not theorised: the palette's own
  // diagnostic logged activeElement=BODY straight after a click, with arrow
  // and Enter handlers never firing again.
  //
  // Summoning-focus alone does not fix that. It restores focus once per
  // summon, and the very next click takes it away again.
  //
  // The compose modal owns its own keys — every field there has an Enter
  // handler that either commits pending text or launches — so this defers
  // entirely while it is open rather than racing it.
  function paletteKeyDown(e) {
    if (composeOpenRef.current) return;
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
      launchTarget(activeTarget?.id);
    }
  }

  // Bound with no dependency array on purpose: the handler closes over
  // filtered/activeIndex/launchProfile/projectPath, and a listener registered
  // once at mount would keep launching whatever the FIRST render happened to
  // have highlighted. Re-subscribing each render costs one add/remove pair
  // and keeps the closure honest.
  useEffect(() => {
    window.addEventListener("keydown", paletteKeyDown);
    return () => window.removeEventListener("keydown", paletteKeyDown);
  });

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
        placeholder="Search agents…"
        autoFocus
        spellCheck={false}
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setDirectLaunchError("");
        }}
      />

      {/* The two axes that are not the target, side by side above the list.
          Both are restricted selects rather than free text, and for
          different reasons: a launch profile is a NAME the Go side resolves
          against its own store (so nothing typed here can point cairn's
          --with at an arbitrary file), and a project is a saved path Tachyon
          already holds. Free text for either belongs in the compose modal,
          which has it. */}
      <div className="palette-axes">
        <label className="palette-axis">
          <span className="muted">How</span>
          <select
            value={launchProfile}
            disabled={launching || profiles.length === 0}
            onChange={(e) => {
              setLaunchProfile(e.target.value);
              setDirectLaunchError("");
            }}
          >
            {profiles.length === 0 ? <option value="">no launch profiles</option> : null}
            {profiles.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}{p.provider ? ` · ${p.provider}` : ""}
              </option>
            ))}
          </select>
        </label>

        <label className="palette-axis">
          <span className="muted">Where</span>
          <select
            value={projectPath}
            disabled={launching}
            onChange={(e) => {
              setProjectPath(e.target.value);
              setDirectLaunchError("");
            }}
          >
            {/* No project is a real choice, not a missing one: cairn accepts
                a boot with no --scope, which is right for a profile like
                conductor that holds no scope of its own. */}
            <option value="">no project</option>
            {projectSuggestions.map((project) => (
              <option key={project.name} value={project.path}>{project.name}</option>
            ))}
          </select>
        </label>
      </div>

      <div className="palette-modebar">
        <span className="muted">Enter or double-click to launch · Compose to add more</span>
        <button type="button" onClick={openBlankComposition} disabled={launching}>
          Type a target…
        </button>
      </div>

      {directLaunchError && !composeOpen ? (
        <div className="err" style={{ padding: "8px 14px" }}>
          Couldn't launch: {directLaunchError}
        </div>
      ) : null}

      <div className="palette-scroll">
        {renderBody({
          targets,
          targetsError,
          launchProfiles,
          filtered,
          activeIndex,
          onSelect: (index) => {
            setActiveIndex(index);
            setDirectLaunchError("");
          },
          onHover: setActiveIndex,
          onCompose: openComposition,
          onLaunch: launchTarget,
          launching,
        })}
      </div>

      {composeOpen ? (
        <ComposeSection
          typedTarget={typedTarget}
          targetName={composeTarget}
          retained={composeRetained}
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
          previewInput={compositionInput(composition)}
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
          launchProfile={composeLaunchProfile}
          setLaunchProfile={setComposeLaunchProfile}
          launchProfileOptions={profiles}
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
          clearComposition={clearComposition}
          discardComposition={discardComposition}
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

// ComposeSection is the compose form: a typed target when the draft has no
// picked one, then one control per cairn flag, in the order cairn resolves
// them — launch profile -> ordered parts -> additive skills/prompts -> sets
// -> scope. Parts can be reordered because each becomes an ordered --with.
// Skills and prompts stay additive-only; their chip order is insertion
// order, never an inherited selection.
//
// There is deliberately no template control — template choice is
// authoring-time only (D4) — and deliberately no PROVIDER control: it is
// declared in the launch profile and folded in by cairn's cascade, so a
// field here would be a second source for one value. See this file's header.
function ComposeSection(props) {
  const {
    typedTarget,
    targetName,
    retained,
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
    previewInput,
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
    launchProfile,
    setLaunchProfile,
    launchProfileOptions,
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
    clearComposition,
    discardComposition,
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
    <div className="compose-modal-backdrop">
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
              {typedTarget ? "One-time composition" : "Compose additions for"}
            </div>
            <div className="compose-modal-target">
              {typedTarget ? targetName || "Choose an agent profile" : targetName}
            </div>
            {retained ? (
              <div className="compose-draft-retained" role="status">
                Draft retained while the palette was hidden
              </div>
            ) : null}
          </div>
          <button type="button" className="compose-modal-discard" onClick={discardComposition}>
            Discard &amp; close
          </button>
        </header>

        <div className="compose-modal-body">
          {launchError ? <div className="err">Couldn't launch: {launchError}</div> : null}

          <div className="compose-section">

      {typedTarget && !targetName ? (
        <div className="compose-field compose-base-profile">
          <label htmlFor="compose-profile-input">
            Agent profile to capture <span className="muted">(suggestions only — free text works)</span>
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
        <div className="effective-skills-pair">
          <div className="effective-skills-picker">
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
          <EffectiveSkills input={previewInput} />
        </div>
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

      {/* The launch profile: the file that says HOW this runs. It is a
          restricted select, unlike every other control here, and the reason
          is not taste — the Go side resolves a NAME against its own store,
          which validates it before joining, so nothing sent from here can
          point cairn's --with at an arbitrary file.

          An empty one means no provider, which cairn refuses. That is the
          intended shape rather than a gap; the option is offered so the
          refusal is reachable deliberately rather than only by accident. */}
      <div className="compose-field">
        <label htmlFor="compose-launch-profile">
          Launch profile <span className="muted">(the provider and posture — --with)</span>
        </label>
        <select
          id="compose-launch-profile"
          value={launchProfile}
          onChange={(e) => setLaunchProfile(e.target.value)}
        >
          <option value="">none — cairn will refuse this launch</option>
          {(launchProfileOptions ?? []).map((p) => (
            <option key={p.name} value={p.name}>
              {p.name}{p.provider ? ` · ${p.provider}` : ""}
              {p.description ? ` — ${p.description}` : ""}
            </option>
          ))}
        </select>
      </div>

      {/* Scope maps directly to --scope, and is the ONLY source for one:
          cairn refuses `scope:` as frontmatter, so a launch profile cannot
          carry a scope and there is nothing here to override. Project names
          never cross the launch boundary: selecting one copies its literal
          saved path into this ordinary free-text input. */}
      <div className="compose-field">
        <label htmlFor="compose-scope-input">
          Project scope<span className="muted"> (literal path — --scope)</span>
        </label>
        <input
          id="compose-scope-input"
          list="compose-project-suggestions"
          placeholder="project path (optional — no scope is a real choice)"
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
          <span className="muted">Launch only — save a launch profile from the manager.</span>
          <div className="compose-modal-actions">
            <button type="button" className="secondary" onClick={clearComposition} disabled={launching}>
              Clear
            </button>
            <button type="button" onClick={attemptLaunch} disabled={launching || (!targetName && !baseProfile.trim())}>
              {launching ? "Launching…" : typedTarget && !targetName ? "Use this profile" : "Launch composition"}
            </button>
          </div>
        </footer>
      </section>
    </div>
  );
}

// renderBody picks one of the palette's states.
//
// It reports on TWO stores, which is what the shape below is for. The agent
// list comes from the bundle and the launch-profile list from Tachyon's own
// directory; either can be empty or broken while the other is fine, and the
// palette can launch nothing without both. A single "nothing here" would
// leave a person guessing which one to go and fix.
//
// launchProfiles is null while loading; once settled it is the real
// ListResult ({ profiles, state: "ok"|"missing"|"unreadable", path, detail })
// or the synthetic { state: "error", detail } for a rejected call. Each
// non-"ok" state gets its own honest copy — none collapse into "none yet",
// which is reserved for the one state where that is actually true, and
// which is now the ordinary first-run state rather than a fault.
function renderBody({
  targets,
  targetsError,
  launchProfiles,
  filtered,
  activeIndex,
  onSelect,
  onHover,
  onCompose,
  onLaunch,
  launching,
}) {
  if (targets === null || launchProfiles === null) {
    return <div className="placeholder muted">Loading…</div>;
  }

  // The bundle first: without an agent profile there is nothing to launch,
  // whatever the launch store says.
  if (targetsError) {
    return (
      <div className="placeholder">
        <div>
          <div className="err">Couldn't read the bundle</div>
          <div className="muted" style={{ marginTop: 8 }}>{targetsError}</div>
        </div>
      </div>
    );
  }

  const profileState = launchProfiles.state;
  if (profileState === "error" || profileState === "unreadable") {
    return (
      <div className="placeholder">
        <div>
          <div className="err">Couldn't read your launch profiles</div>
          <div className="muted" style={{ marginTop: 8 }}>
            {launchProfiles.detail}
            {launchProfiles.path ? <> · <code>{launchProfiles.path}</code></> : null}
          </div>
        </div>
      </div>
    );
  }

  // "missing" is first run, not a fault, and the copy has to say so: the
  // directory is created the moment one is written, and Tachyon seeds a
  // default at startup, so seeing this means neither has happened yet.
  if (profileState === "missing" || (launchProfiles.profiles ?? []).length === 0) {
    return (
      <div className="placeholder">
        <div>
          <div>No launch profiles yet</div>
          <div className="muted" style={{ marginTop: 8 }}>
            A launch profile says how an agent runs — which harness, which
            settings. Nothing can launch without one, because no profile in
            the bundle names a provider. Write one at{" "}
            <code>{launchProfiles.path}</code>, or open the manager.
          </div>
        </div>
      </div>
    );
  }

  if (filtered.length === 0) {
    return (
      <div className="placeholder">
        <div className="muted">
          {(targets ?? []).length === 0
            ? "This bundle has no bootable agent profiles"
            : "No agents match your search"}
        </div>
      </div>
    );
  }

  return (
    <ul className="palette-list" role="listbox">
      {filtered.map((t, i) => (
        <li key={t.id} className={"palette-row" + (i === activeIndex ? " active" : "")}>
          <button
            type="button"
            role="option"
            aria-selected={i === activeIndex}
            className="palette-row-main"
            onMouseEnter={() => onHover(i)}
            onClick={() => onSelect(i)}
            onDoubleClick={() => onLaunch(t.id)}
            disabled={launching}
          >
            <span className="palette-row-name">{t.id}</span>
            <span className="palette-row-profile">{t.name}</span>
            <span className="palette-row-scope">{t.description}</span>
          </button>
          <button
            type="button"
            className="palette-row-compose"
            onClick={() => onCompose(i, t.id)}
            disabled={launching}
          >
            Compose
          </button>
        </li>
      ))}
    </ul>
  );
}
