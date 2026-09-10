import assert from "node:assert/strict";
import test from "node:test";

import {
  compositionDraftReducer,
  compositionInput,
  createCompositionDraft,
} from "./compositionDraft.js";

const reduce = (state, ...actions) => actions.reduce(compositionDraftReducer, state);

function populatedDraft(target = "engineer") {
  return reduce(
    createCompositionDraft(),
    { type: "OPEN_TARGET", target },
    { type: "ADD_PART", value: "reviewer" },
    { type: "UPDATE_FIELD", field: "partDraft", value: "pending-part" },
    { type: "ADD_SKILLS", values: ["commit", "push"] },
    { type: "UPDATE_FIELD", field: "skillDraft", value: "pending-skill" },
    { type: "ADD_PROMPTS", values: ["report"] },
    { type: "UPDATE_FIELD", field: "promptDraft", value: "pending-prompt" },
    { type: "ADD_SET", value: { slot: "tone", value: "terse" } },
    { type: "UPDATE_FIELD", field: "setSlotDraft", value: "model" },
    { type: "UPDATE_FIELD", field: "setValueDraft", value: "fast" },
    { type: "UPDATE_FIELD", field: "launchProfile", value: " codex " },
    { type: "UPDATE_FIELD", field: "scope", value: " /work/tachyon " },
  );
}

test("passive hide retains every field, modal mode, and captured target", () => {
  const before = populatedDraft();
  const hidden = compositionDraftReducer(before, { type: "HIDE" });

  assert.deepEqual(hidden, { ...before, retained: true });
  assert.equal(hidden.open, true);
  assert.equal(hidden.target, "engineer");
  assert.deepEqual(compositionInput(hidden), {
    target: "engineer",
    launchProfile: "codex",
    skills: ["commit", "push"],
    prompts: ["report"],
    scope: "/work/tachyon",
    sets: [{ slot: "tone", value: "terse" }],
    parts: ["reviewer"],
  });
});

test("an open draft refuses target switching from palette selection", () => {
  const draft = populatedDraft("engineer");

  assert.strictEqual(
    compositionDraftReducer(draft, { type: "OPEN_TARGET", target: "architect" }),
    draft,
  );
  assert.strictEqual(compositionDraftReducer(draft, { type: "OPEN_BLANK" }), draft);
  assert.equal(compositionInput(draft).target, "engineer");
});

test("a typed target becomes immutable only after explicit capture", () => {
  const choosing = reduce(
    createCompositionDraft(),
    { type: "OPEN_BLANK" },
    { type: "SET_TARGET_DRAFT", value: " engineer " },
  );
  assert.equal(choosing.target, "");
  assert.equal(compositionInput(choosing), null);

  const captured = compositionDraftReducer(choosing, { type: "CAPTURE_TARGET" });
  assert.equal(captured.target, "engineer");
  assert.equal(captured.targetDraft, "engineer");
  assert.strictEqual(
    compositionDraftReducer(captured, { type: "SET_TARGET_DRAFT", value: "reviewer" }),
    captured,
  );
  assert.equal(compositionInput(captured).target, "engineer");

  const retained = reduce(
    captured,
    { type: "UPDATE_FIELD", field: "scope", value: "/work/one-time" },
    { type: "HIDE" },
  );
  assert.equal(retained.open, true);
  assert.equal(retained.retained, true);
  assert.equal(retained.mode, "typed");
  assert.equal(retained.target, "engineer");
  assert.equal(retained.targetDraft, "engineer");
  assert.equal(compositionInput(retained).scope, "/work/one-time");
});

test("a draft opens on the launch profile and project it was given", () => {
  const opened = compositionDraftReducer(createCompositionDraft(), {
    type: "OPEN_TARGET",
    target: "engineer",
    defaults: { launchProfile: "default", scope: "/work/tachyon" },
  });
  assert.equal(opened.launchProfile, "default");
  assert.equal(opened.scope, "/work/tachyon");

  // Skills and prompts are NEVER seeded, whatever defaults say. cairn's
  // --skill and --prompt are additive only, so a pre-filled control would
  // let someone remove an entry and silently get it anyway.
  assert.deepEqual(opened.skills, []);
  assert.deepEqual(opened.prompts, []);

  const bare = compositionDraftReducer(createCompositionDraft(), {
    type: "OPEN_TARGET",
    target: "engineer",
  });
  assert.equal(bare.launchProfile, "");
  // An empty launch profile means no provider, which cairn refuses. That is
  // the intended shape rather than a gap -- the palette supplies a default
  // because internal/launchprofile seeds one.
  assert.equal(compositionInput(bare).launchProfile, "");
});

test("the launch profile is trimmed on the way out", () => {
  const untouched = compositionDraftReducer(createCompositionDraft(), {
    type: "OPEN_TARGET",
    target: "engineer",
  });
  const chosen = compositionDraftReducer(
    { ...untouched, launchError: "cairn refused the composition" },
    { type: "UPDATE_FIELD", field: "launchProfile", value: "  codex  " },
  );
  assert.equal(chosen.launchProfile, "  codex  ");
  assert.equal(chosen.launchError, "");
  assert.equal(compositionInput(chosen).launchProfile, "codex");
});

test("no provider ever reaches the launch input", () => {
  // --provider is gone: the provider is declared in the launch profile and
  // folded in by cairn's cascade. A field here would be a second source for
  // one value, and the two could disagree.
  const draft = populatedDraft();
  assert.equal("provider" in draft, false);
  assert.equal("provider" in compositionInput(draft), false);
});

test("Clear removes every override and pending input but preserves target and mode", () => {
  const before = compositionDraftReducer(populatedDraft(), { type: "HIDE" });
  const cleared = compositionDraftReducer(before, { type: "CLEAR" });

  assert.equal(cleared.open, true);
  assert.equal(cleared.mode, "picked");
  assert.equal(cleared.target, "engineer");
  assert.equal(cleared.draftId, before.draftId);
  assert.equal(cleared.retained, false);
  assert.deepEqual(compositionInput(cleared), {
    target: "engineer",
    launchProfile: "",
    skills: [],
    prompts: [],
    scope: "",
    sets: [],
    parts: [],
  });
  for (const field of [
    "skillDraft", "promptDraft", "launchProfile", "scope", "partDraft", "setSlotDraft", "setValueDraft",
  ]) assert.equal(cleared[field], "", `${field} should be cleared`);
});

test("Discard & close clears the whole draft and permits a different target", () => {
  const before = populatedDraft("engineer");
  const discarded = compositionDraftReducer(before, { type: "DISCARD" });

  assert.deepEqual(discarded, createCompositionDraft(before.draftId + 1));
  const next = compositionDraftReducer(discarded, { type: "OPEN_TARGET", target: "architect" });
  assert.equal(next.open, true);
  assert.equal(next.target, "architect");
});

test("failed launch retains the entire draft and records its error", () => {
  const before = populatedDraft();
  const started = compositionDraftReducer(before, { type: "LAUNCH_START", draftId: before.draftId });
  const failed = compositionDraftReducer(started, {
    type: "LAUNCH_FAILURE",
    draftId: before.draftId,
    error: "cairn refused the composition",
  });

  assert.deepEqual(failed, {
    ...before,
    launching: false,
    launchError: "cairn refused the composition",
  });
});

test("successful launch consumes and closes the draft", () => {
  const before = populatedDraft();
  const started = compositionDraftReducer(before, { type: "LAUNCH_START", draftId: before.draftId });
  const succeeded = compositionDraftReducer(started, {
    type: "LAUNCH_SUCCESS",
    draftId: before.draftId,
  });

  assert.deepEqual(succeeded, createCompositionDraft(before.draftId + 1));
  assert.equal(compositionInput(succeeded), null);
});

test("late launch outcomes cannot clear or contaminate a replacement draft", () => {
  const first = populatedDraft("engineer");
  const replacement = reduce(
    first,
    { type: "DISCARD" },
    { type: "OPEN_TARGET", target: "architect" },
    { type: "ADD_SKILLS", values: ["new-skill"] },
  );

  for (const stale of [
    { type: "LAUNCH_SUCCESS", draftId: first.draftId },
    { type: "LAUNCH_FAILURE", draftId: first.draftId, error: "old error" },
  ]) assert.strictEqual(compositionDraftReducer(replacement, stale), replacement);
  assert.equal(replacement.target, "architect");
  assert.equal(replacement.launchError, "");
});
