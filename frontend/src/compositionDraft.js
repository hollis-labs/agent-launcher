const emptyOverrides = Object.freeze({
  skills: [],
  skillDraft: "",
  prompts: [],
  promptDraft: "",
  provider: "",
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

function newDraft(state, mode, target = "") {
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
    case "OPEN_BINDING":
      return newDraft(state, "binding", action.target);
    case "OPEN_PROFILE":
      return newDraft(state, "profile");
    case "SET_TARGET_DRAFT":
      if (!state.open || state.mode !== "profile" || state.target) return state;
      return { ...state, targetDraft: action.value, launchError: "" };
    case "CAPTURE_PROFILE": {
      if (!state.open || state.mode !== "profile" || state.target) return state;
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

export function compositionInput(state) {
  if (!state.open || !state.target) return null;
  return {
    target: state.target,
    skills: state.skills,
    prompts: state.prompts,
    provider: state.provider.trim(),
    scope: state.scope.trim(),
    sets: state.sets,
    parts: state.parts,
  };
}
