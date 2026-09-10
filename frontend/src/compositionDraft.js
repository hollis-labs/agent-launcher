// The compose form's own state, minus the target.
//
// `provider` is deliberately absent. It used to be a free-text control that
// rendered --provider; that flag is gone, and the provider is declared in
// the launch profile and folded in by cairn's own cascade. A control here
// would be a second source for a value a file already carries.
//
// `launchProfile` replaced it, and is not the same thing wearing a new
// name: it names a file that says HOW this runs -- provider, sandbox
// posture, settings -- which the launcher owns and cairn consumes as an
// ordinary part.
const emptyOverrides = Object.freeze({
  skills: [],
  skillDraft: "",
  prompts: [],
  promptDraft: "",
  launchProfile: "",
  scope: "",
  parts: [],
  partDraft: "",
  sets: [],
  setSlotDraft: "",
  setValueDraft: "",
});

export function createCompositionDraft(draftId = 0) {
  return {
    draftId,
    open: false,
    retained: false,
    mode: null,
    target: "",
    targetDraft: "",
    launchError: "",
    launching: false,
    ...emptyOverrides,
  };
}

// defaultOverrides are the fields a fresh draft may start non-empty, and
// the list is deliberately short: skills and prompts are NEVER among them.
//
// cairn's --skill and --prompt are additive only, so a control that started
// pre-filled would let a person remove an entry and silently get it anyway.
// launchProfile and scope are different in kind -- they are single-valued
// selections, and starting them at the obvious choice is a convenience
// rather than a claim about what the target already resolves to.
function newDraft(state, mode, target = "", defaults = {}) {
  // An open modal owns its target. UI beneath the backdrop cannot normally
  // ask to open another one, but refusing here makes that safety property
  // independent of presentation details.
  if (state.open) return state;
  return {
    ...createCompositionDraft(state.draftId + 1),
    open: true,
    mode,
    target,
    targetDraft: target,
    launchProfile: defaults.launchProfile ?? "",
    scope: defaults.scope ?? "",
  };
}

function clearOverrides(state) {
  return {
    ...state,
    ...emptyOverrides,
    retained: false,
    launchError: "",
  };
}

function updateField(state, field, value) {
  if (!Object.prototype.hasOwnProperty.call(emptyOverrides, field)) return state;
  return { ...state, [field]: value, launchError: "" };
}

export function compositionDraftReducer(state, action) {
  switch (action.type) {
    case "OPEN_TARGET":
      return newDraft(state, "picked", action.target, action.defaults);
    case "OPEN_BLANK":
      return newDraft(state, "typed", "", action.defaults);
    case "SET_TARGET_DRAFT":
      if (!state.open || state.mode !== "typed" || state.target) return state;
      return { ...state, targetDraft: action.value, launchError: "" };
    case "CAPTURE_TARGET": {
      if (!state.open || state.mode !== "typed" || state.target) return state;
      const target = state.targetDraft.trim();
      if (!target) return state;
      return { ...state, target, targetDraft: target, launchError: "" };
    }
    case "UPDATE_FIELD":
      return updateField(state, action.field, action.value);
    case "ADD_SKILLS":
      return updateField(state, "skills", [...new Set([...state.skills, ...action.values])]);
    case "REMOVE_SKILL":
      return updateField(state, "skills", state.skills.filter((value) => value !== action.value));
    case "ADD_PROMPTS":
      return updateField(state, "prompts", [...new Set([...state.prompts, ...action.values])]);
    case "REMOVE_PROMPT":
      return updateField(state, "prompts", state.prompts.filter((value) => value !== action.value));
    case "ADD_PART":
      if (!action.value || state.parts.includes(action.value)) return state;
      return updateField(state, "parts", [...state.parts, action.value]);
    case "REMOVE_PART":
      return updateField(state, "parts", state.parts.filter((value) => value !== action.value));
    case "MOVE_PART": {
      const nextIndex = action.index + action.offset;
      if (action.index < 0 || nextIndex < 0 || nextIndex >= state.parts.length) return state;
      const parts = [...state.parts];
      [parts[action.index], parts[nextIndex]] = [parts[nextIndex], parts[action.index]];
      return updateField(state, "parts", parts);
    }
    case "ADD_SET":
      return updateField(state, "sets", [...state.sets, action.value]);
    case "REMOVE_SET":
      return updateField(state, "sets", state.sets.filter((_, index) => index !== action.index));
    case "HIDE":
      return state.open ? { ...state, retained: true } : state;
    case "CLEAR":
      return state.open && !state.launching ? clearOverrides(state) : state;
    case "DISCARD":
      return createCompositionDraft(state.draftId + 1);
    case "LAUNCH_START":
      if (!state.open || !state.target || action.draftId !== state.draftId) return state;
      return { ...state, launching: true, launchError: "" };
    case "LAUNCH_FAILURE":
      if (!state.open || action.draftId !== state.draftId) return state;
      return { ...state, launching: false, launchError: action.error };
    case "LAUNCH_SUCCESS":
      if (!state.open || action.draftId !== state.draftId) return state;
      return createCompositionDraft(state.draftId + 1);
    default:
      return state;
  }
}

// compositionInput is what crosses the Wails boundary: exactly
// internal/launch.CompositionInput's fields, and nothing synthesized.
//
// launchProfile is a NAME, never a path. The Go side resolves it against
// the launch store, which validates it before joining, so nothing sent from
// here can point cairn's --with at an arbitrary file.
export function compositionInput(state) {
  if (!state.open || !state.target) return null;
  return {
    target: state.target,
    launchProfile: state.launchProfile.trim(),
    skills: state.skills,
    prompts: state.prompts,
    scope: state.scope.trim(),
    sets: state.sets,
    parts: state.parts,
  };
}
