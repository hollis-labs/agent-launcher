import assert from "node:assert/strict";
import test from "node:test";

import {
  compositionDraftReducer,
  compositionInput,
  createCompositionDraft,
} from "./compositionDraft.js";

const reduce = (state, ...actions) => actions.reduce(compositionDraftReducer, state);

function populatedBindingDraft(target = "binding-a") {
  return reduce(
    createCompositionDraft(),
    { type: "OPEN_BINDING", target },
    { type: "ADD_PART", value: "reviewer" },
    { type: "UPDATE_FIELD", field: "partDraft", value: "pending-part" },
    { type: "ADD_SKILLS", values: ["commit", "push"] },
    { type: "UPDATE_FIELD", field: "skillDraft", value: "pending-skill" },
    { type: "ADD_PROMPTS", values: ["report"] },
    { type: "UPDATE_FIELD", field: "promptDraft", value: "pending-prompt" },
    { type: "ADD_SET", value: { slot: "tone", value: "terse" } },
    { type: "UPDATE_FIELD", field: "setSlotDraft", value: "model" },
    { type: "UPDATE_FIELD", field: "setValueDraft", value: "fast" },
    { type: "UPDATE_FIELD", field: "provider", value: " codex " },
    { type: "UPDATE_FIELD", field: "scope", value: " /work/tachyon " },
  );
}

test("passive hide retains every field, modal mode, and captured binding", () => {
  const before = populatedBindingDraft();
  const hidden = compositionDraftReducer(before, { type: "HIDE" });

  assert.deepEqual(hidden, { ...before, retained: true });
  assert.equal(hidden.open, true);
  assert.equal(hidden.target, "binding-a");
  assert.deepEqual(compositionInput(hidden), {
    target: "binding-a",
    skills: ["commit", "push"],
    prompts: ["report"],
    provider: "codex",
    scope: "/work/tachyon",
    sets: [{ slot: "tone", value: "terse" }],
    parts: ["reviewer"],
  });
});

test("an open draft refuses target switching from palette selection", () => {
  const draft = populatedBindingDraft("binding-a");

  assert.strictEqual(
    compositionDraftReducer(draft, { type: "OPEN_BINDING", target: "binding-b" }),
    draft,
  );
  assert.strictEqual(compositionDraftReducer(draft, { type: "OPEN_PROFILE" }), draft);
  assert.equal(compositionInput(draft).target, "binding-a");
});

test("one-time profile becomes immutable only after explicit capture", () => {
  const choosing = reduce(
    createCompositionDraft(),
    { type: "OPEN_PROFILE" },
    { type: "SET_TARGET_DRAFT", value: " engineer " },
  );
  assert.equal(choosing.target, "");
  assert.equal(compositionInput(choosing), null);

  const captured = compositionDraftReducer(choosing, { type: "CAPTURE_PROFILE" });
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
  assert.equal(retained.mode, "profile");
  assert.equal(retained.target, "engineer");
  assert.equal(retained.targetDraft, "engineer");
  assert.equal(compositionInput(retained).scope, "/work/one-time");
});

test("provider is an untouched-by-default override that is trimmed on the way out", () => {
  const untouched = compositionDraftReducer(createCompositionDraft(), {
    type: "OPEN_BINDING",
    target: "binding-a",
  });
  assert.equal(untouched.provider, "");
  // The empty string is the ordinary case, not a missing value: it means
  // Tachyon sends no --provider flag at all and the resolved profile
  // cascade picks the harness.
  assert.equal(compositionInput(untouched).provider, "");

  const chosen = compositionDraftReducer(
    { ...untouched, launchError: "cairn refused the composition" },
    { type: "UPDATE_FIELD", field: "provider", value: "  codex  " },
  );
  assert.equal(chosen.provider, "  codex  ");
  assert.equal(chosen.launchError, "");
  assert.equal(compositionInput(chosen).provider, "codex");

  // Free text reaches Cairn unchanged; only Cairn decides what a provider is.
  const unknown = compositionDraftReducer(untouched, {
    type: "UPDATE_FIELD",
    field: "provider",
    value: "opencode",
  });
  assert.equal(compositionInput(unknown).provider, "opencode");
});

test("Clear removes every override and pending input but preserves target and mode", () => {
  const before = compositionDraftReducer(populatedBindingDraft(), { type: "HIDE" });
  const cleared = compositionDraftReducer(before, { type: "CLEAR" });

  assert.equal(cleared.open, true);
  assert.equal(cleared.mode, "binding");
  assert.equal(cleared.target, "binding-a");
  assert.equal(cleared.draftId, before.draftId);
  assert.equal(cleared.retained, false);
  assert.deepEqual(compositionInput(cleared), {
    target: "binding-a",
    skills: [],
    prompts: [],
    provider: "",
    scope: "",
    sets: [],
    parts: [],
  });
  for (const field of [
    "skillDraft", "promptDraft", "provider", "scope", "partDraft", "setSlotDraft", "setValueDraft",
  ]) assert.equal(cleared[field], "", `${field} should be cleared`);
});

test("Discard & close clears the whole draft and permits a different target", () => {
  const before = populatedBindingDraft("binding-a");
  const discarded = compositionDraftReducer(before, { type: "DISCARD" });

  assert.deepEqual(discarded, createCompositionDraft(before.draftId + 1));
  const next = compositionDraftReducer(discarded, { type: "OPEN_BINDING", target: "binding-b" });
  assert.equal(next.open, true);
  assert.equal(next.target, "binding-b");
});

test("failed launch retains the entire draft and records its error", () => {
  const before = populatedBindingDraft();
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
  const before = populatedBindingDraft();
  const started = compositionDraftReducer(before, { type: "LAUNCH_START", draftId: before.draftId });
  const succeeded = compositionDraftReducer(started, {
    type: "LAUNCH_SUCCESS",
    draftId: before.draftId,
  });

  assert.deepEqual(succeeded, createCompositionDraft(before.draftId + 1));
  assert.equal(compositionInput(succeeded), null);
});

test("late launch outcomes cannot clear or contaminate a replacement draft", () => {
  const first = populatedBindingDraft("binding-a");
  const replacement = reduce(
    first,
    { type: "DISCARD" },
    { type: "OPEN_BINDING", target: "binding-b" },
    { type: "ADD_SKILLS", values: ["new-skill"] },
  );

  for (const stale of [
    { type: "LAUNCH_SUCCESS", draftId: first.draftId },
    { type: "LAUNCH_FAILURE", draftId: first.draftId, error: "old error" },
  ]) assert.strictEqual(compositionDraftReducer(replacement, stale), replacement);
  assert.equal(replacement.target, "binding-b");
  assert.equal(replacement.launchError, "");
});
