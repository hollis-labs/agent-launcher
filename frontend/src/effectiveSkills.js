export const PREVIEW_DEBOUNCE_MS = 180;

export function isCancellationError(error) {
  const name = String(error?.name ?? "");
  const message = String(error?.message ?? error ?? "");
  return name === "CancelError" || name === "CanceledError" ||
    /\b(cancelled|canceled|context canceled)\b/i.test(message);
}

export function createEffectiveSkillsController({
  preview,
  onState,
  delay = PREVIEW_DEBOUNCE_MS,
  schedule = setTimeout,
  clearSchedule = clearTimeout,
}) {
  let generation = 0;
  let timer = null;
  let request = null;

  const cancelOutstanding = () => {
    if (timer !== null) {
      clearSchedule(timer);
      timer = null;
    }
    const pending = request;
    request = null;
    if (typeof pending?.cancel === "function") {
      try {
        const cancellation = pending.cancel();
        cancellation?.catch?.(() => {});
      } catch {
        // Cancellation is cleanup. It must never surface as a preview error.
      }
    }
  };

  const cancel = () => {
    generation += 1;
    cancelOutstanding();
  };

  const update = (input) => {
    generation += 1;
    const myGeneration = generation;
    cancelOutstanding();

    if (!input?.target) {
      onState({ status: "idle", skills: [], advisory: "", error: "" });
      return;
    }

    onState({ status: "loading", skills: [], advisory: "", error: "" });
    timer = schedule(() => {
      timer = null;
      if (myGeneration !== generation) return;

      let rawRequest;
      try {
        rawRequest = preview(input);
      } catch (error) {
        if (myGeneration === generation && !isCancellationError(error)) {
          onState({ status: "error", skills: [], advisory: "", error: String(error?.message ?? error) });
        }
        return;
      }

      // Keep the original Wails object for cancel(); Promise.resolve is used
      // only to attach portable completion handlers for tests and browsers.
      request = rawRequest;
      Promise.resolve(rawRequest).then(
        (result) => {
          if (myGeneration !== generation || request !== rawRequest) return;
          request = null;
          onState({
            status: "ready",
            skills: Array.isArray(result?.skills) ? result.skills : [],
            advisory: String(result?.advisory ?? ""),
            error: "",
          });
        },
        (error) => {
          if (myGeneration !== generation || request !== rawRequest) return;
          request = null;
          if (isCancellationError(error)) {
            onState({ status: "idle", skills: [], advisory: "", error: "" });
            return;
          }
          onState({ status: "error", skills: [], advisory: "", error: String(error?.message ?? error) });
        },
      );
    }, delay);
  };

  const dispose = () => {
    cancel();
  };

  return { update, cancel, dispose };
}
