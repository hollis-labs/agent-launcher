import test from "node:test";
import assert from "node:assert/strict";
import { createEffectiveSkillsController, isCancellationError } from "./effectiveSkills.js";

function scheduler() {
  let nextID = 1;
  const jobs = new Map();
  return {
    schedule(fn) {
      const id = nextID++;
      jobs.set(id, fn);
      return id;
    },
    clear(id) { jobs.delete(id); },
    run() {
      const entries = [...jobs.entries()];
      jobs.clear();
      for (const [, fn] of entries) fn();
    },
    get size() { return jobs.size; },
  };
}

function deferred({ cancelSettles = false } = {}) {
  let resolve;
  let reject;
  let cancellations = 0;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  promise.cancel = () => {
    cancellations += 1;
    if (cancelSettles) {
      const error = new Error("binding call canceled");
      error.name = "CancelError";
      reject(error);
    }
    return Promise.resolve();
  };
  return { promise, resolve, reject, cancellations: () => cancellations };
}

const flush = () => new Promise((resolve) => setImmediate(resolve));

function controller(preview, states, clock) {
  return createEffectiveSkillsController({
    preview,
    onState: (state) => states.push(state),
    delay: 25,
    schedule: clock.schedule,
    clearSchedule: clock.clear,
  });
}

test("draft changes cancel the pending debounce and only request the newest input", async () => {
  const clock = scheduler();
  const states = [];
  const calls = [];
  const c = controller((input) => {
    calls.push(input);
    return Promise.resolve({ skills: [input.target], advisory: "" });
  }, states, clock);

  c.update({ target: "first", skills: [] });
  assert.equal(clock.size, 1);
  c.update({ target: "second", skills: ["new"] });
  assert.equal(clock.size, 1, "the old debounce was cleared before scheduling the new one");
  clock.run();
  await flush();

  assert.deepEqual(calls, [{ target: "second", skills: ["new"] }]);
  assert.deepEqual(states.at(-1).skills, ["second"]);
});

test("draft changes cancel the raw in-flight Wails request", () => {
  const clock = scheduler();
  const states = [];
  const first = deferred();
  const c = controller(() => first.promise, states, clock);

  c.update({ target: "first" });
  clock.run();
  c.update({ target: "second" });

  assert.equal(first.cancellations(), 1);
  assert.equal(clock.size, 1);
});

test("modal close and surface unmount each cancel pending work", () => {
  for (const lifecycle of ["cancel", "dispose"]) {
    const clock = scheduler();
    const states = [];
    const request = deferred();
    let calls = 0;
    const c = controller(() => { calls += 1; return request.promise; }, states, clock);

    c.update({ target: "engineer" });
    c[lifecycle]();
    clock.run();
    assert.equal(calls, 0, `${lifecycle} did not clear a pending debounce`);

    c.update({ target: "engineer" });
    clock.run();
    c[lifecycle]();
    assert.equal(request.cancellations(), 1, `${lifecycle} did not cancel the raw request`);
  }
});

test("an older completion cannot replace a newer preview", async () => {
  const clock = scheduler();
  const states = [];
  const first = deferred();
  const second = deferred();
  const requests = [first, second];
  const c = controller(() => requests.shift().promise, states, clock);

  c.update({ target: "first" });
  clock.run();
  c.update({ target: "second" });
  clock.run();

  second.resolve({ skills: ["new"], advisory: "new advisory" });
  await flush();
  first.resolve({ skills: ["stale"], advisory: "stale advisory" });
  await flush();

  assert.equal(states.at(-1).status, "ready");
  assert.deepEqual(states.at(-1).skills, ["new"]);
  assert.equal(states.at(-1).advisory, "new advisory");
  assert.ok(!states.some((state) => state.skills.includes("stale")));
});

test("cancellation errors are suppressed", async () => {
  const clock = scheduler();
  const states = [];
  const request = deferred();
  const c = controller(() => request.promise, states, clock);

  c.update({ target: "engineer" });
  clock.run();
  const cancellation = new Error("context canceled");
  cancellation.name = "CancelError";
  request.reject(cancellation);
  await flush();

  assert.equal(states.at(-1).status, "idle");
  assert.ok(!states.some((state) => state.status === "error"));
  assert.equal(isCancellationError(cancellation), true);
  assert.equal(isCancellationError(new Error("profile not found")), false);
});

test("preview errors and advisories remain presentation-only state", async () => {
  const clock = scheduler();
  const states = [];
  const requests = [
    Promise.reject(new Error("malformed preview")),
    Promise.resolve({ skills: ["commit"], advisory: "scope unresolved\n" }),
  ];
  const c = controller(() => requests.shift(), states, clock);

  c.update({ target: "engineer" });
  clock.run();
  await flush();
  assert.deepEqual(states.at(-1), {
    status: "error", skills: [], advisory: "", error: "malformed preview",
  });

  c.update({ target: "engineer", scope: "/missing" });
  clock.run();
  await flush();
  assert.deepEqual(states.at(-1), {
    status: "ready", skills: ["commit"], advisory: "scope unresolved\n", error: "",
  });
});
