// Find and accept the first suggestion that the UI presents for a draft.
// Prefix matches win over substring matches; each group stays source-ordered.
// Suggestions may be strings or records shaped
// { value, search: string[] }; records let a Project match either its name
// or path while still inserting the literal path required by Cairn.
export function firstMatchingSuggestion(draft, suggestions) {
  const needle = draft.trim().toLowerCase();
  if (!needle) return null;
  const searchable = (suggestion) => {
    return typeof suggestion === "string"
      ? [suggestion]
      : [suggestion.value, ...(suggestion.search ?? [])];
  };
  const matches = (mode) => suggestions.find((suggestion) => {
    return searchable(suggestion).some((value) => String(value).toLowerCase()[mode](needle));
  });
  return matches("startsWith") ?? matches("includes") ?? null;
}

// Accept on the first unmodified Tab, then let a second Tab traverse normally.
// The caller's draft is changed only when the top match differs from it,
// which is also the only time this helper consumes the event. Modified Tab
// and an in-progress IME composition always retain their native behavior.
export function acceptTopSuggestion(event, draft, suggestions, setDraft) {
  if (
    event.key !== "Tab" || event.shiftKey || event.ctrlKey ||
    event.altKey || event.metaKey || event.isComposing ||
    event.nativeEvent?.isComposing
  ) return false;
  const match = firstMatchingSuggestion(draft, suggestions);
  if (!match) return false;
  const value = typeof match === "string" ? match : match.value;
  if (value === draft) return false;
  event.preventDefault();
  setDraft(value);
  return true;
}
