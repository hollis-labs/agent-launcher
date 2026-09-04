import assert from "node:assert/strict";
import test from "node:test";

import { acceptTopSuggestion, firstMatchingSuggestion } from "./autocomplete.js";

test("firstMatchingSuggestion prefers source-ordered prefixes before substrings", () => {
  assert.equal(firstMatchingSuggestion("eng", ["my-engineer", "Engineer", "engineer-alt"]), "Engineer");
  assert.equal(firstMatchingSuggestion("ENGINE", ["engineer-alt", "engineer"]), "engineer-alt");
  assert.equal(firstMatchingSuggestion("view", ["reviewer", "preview"]), "reviewer");
  assert.equal(firstMatchingSuggestion("", ["engineer"]), null);
});

test("project labels complete to their literal paths", () => {
  const projects = [
    { value: "/work/cairn", search: ["Cairn"] },
    { value: "/work/tachyon", search: ["Tachyon"] },
  ];
  assert.deepEqual(firstMatchingSuggestion("tach", projects), projects[1]);
});

test("Tab completes once, then allows normal traversal", () => {
  let value = "eng";
  let prevented = false;
  const event = { key: "Tab", preventDefault: () => { prevented = true; } };
  assert.equal(acceptTopSuggestion(event, value, ["engineer"], (next) => { value = next; }), true);
  assert.equal(value, "engineer");
  assert.equal(prevented, true);

  prevented = false;
  assert.equal(acceptTopSuggestion(event, value, ["engineer"], () => {}), false);
  assert.equal(prevented, false);
});

test("modified Tab and IME composition are never intercepted", () => {
  for (const event of [
    { key: "Tab", shiftKey: true },
    { key: "Tab", ctrlKey: true },
    { key: "Tab", altKey: true },
    { key: "Tab", metaKey: true },
    { key: "Tab", isComposing: true },
    { key: "Tab", nativeEvent: { isComposing: true } },
  ]) {
    let prevented = false;
    event.preventDefault = () => { prevented = true; };
    assert.equal(acceptTopSuggestion(event, "eng", ["engineer"], () => {}), false);
    assert.equal(prevented, false);
  }
});
