import { useCallback, useEffect, useRef, useState } from "react";
import { Apply as ApplyAPI, Manager as ManagerAPI, Shell } from "./bridge.js";
import { base64ToText, textToBase64 } from "./bytes.js";
// CW-20260903-0010's "new artifact" entry point. Its own file, its own
// state; see NewArtifact.jsx's header comment for why it is not folded in
// here.
import NewArtifact from "./NewArtifact.jsx";

// The bundle tree and the text editor: CW-20260903-0009. Every artifact is a
// text buffer — bytes in, bytes out. This window never parses, reformats or
// validates content; see internal/bundle and internal/manager's package docs
// for why (D5, D8).
export default function Manager({ route }) {
  const [tab, setTab] = useState(route === "settings" ? "settings" : "bundle");
  // bundleGeneration forces <Bundle> to unmount and remount from scratch
  // after a root change: its key changes, React discards the old instance
  // (whatever node was open, any unsaved draft) and mounts a fresh one,
  // whose own effect reloads the tree from the new root the same way it
  // always loads on mount -- see BundleRootBar's onRootChanged below. This
  // is the "no restart" half of CW-20260904-0019's acceptance criteria:
  // it works whether the user is looking at the Bundle tab when they
  // change the root (the visible instance is replaced immediately) or the
  // Settings tab (the next visit to Bundle mounts fresh regardless, since
  // the tab switch itself already unmounts/remounts -- see the ternary
  // below -- but bumping the key here too keeps this component correct on
  // its own, not dependent on that unrelated behavior staying true).
  const [bundleGeneration, setBundleGeneration] = useState(0);
  // bundleDirtyRef is shared between <Bundle> (which writes the currently
  // mounted instance's dirty state into it, and clears it on unmount — see
  // Bundle's own effect below) and <BundleRootBar> (which reads it before
  // calling SetRoot). A root change discards whatever <Bundle> currently
  // has open exactly the same way clicking a different tree node already
  // does; it must ask first, the same way, rather than silently losing an
  // unsaved draft the instant "Change…"/"Reset to default" is clicked —
  // both are reachable from every tab, with no navigation required.
  const bundleDirtyRef = useRef(false);

  // applyStatusBump is CW-20260904-0023's "genuinely re-evaluated whenever
  // it matters, not computed once and cached": Bundle() bumps this after
  // every successful Save (the only place a template/skill/prompt file's
  // content can change from inside the manager), and ApplyBar's own
  // status-fetch effect depends on it, so a save is reflected in the Apply
  // button's enablement without the user having to do anything else. A
  // root change already bumps bundleGeneration, which ApplyBar also
  // depends on directly -- see its own effect below.
  const [applyStatusBump, setApplyStatusBump] = useState(0);
  const bumpApplyStatus = useCallback(() => setApplyStatusBump((n) => n + 1), []);

  useEffect(() => {
    const onHash = () =>
      setTab(window.location.hash === "#/manager/settings" ? "settings" : "bundle");
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  return (
    <div className="manager-shell">
      <header>
        <h1>⌁ Tachyon</h1>
        <button onClick={() => (window.location.hash = "#/manager")}>Bundle</button>
        <button onClick={() => (window.location.hash = "#/manager/settings")}>Settings</button>
        {/* Persistently visible in every tab, not behind a menu --
            CW-20260904-0019's own reasoning, extended here to Apply
            (CW-20260904-0023): editing a template/skill/prompt can happen
            from deep in the tree/editor pane, and Apply's own enablement
            must be visible regardless of which tab is showing, the same
            way BundleRootBar already is. */}
        <div className="header-status-group">
          <ApplyBar refreshKey={`${bundleGeneration}:${applyStatusBump}`} />
          <BundleRootBar
            dirtyRef={bundleDirtyRef}
            onRootChanged={() => setBundleGeneration((g) => g + 1)}
          />
        </div>
      </header>
      <main className={tab === "bundle" ? "no-pad" : undefined}>
        {tab === "settings" ? (
          <Settings />
        ) : (
          <Bundle key={bundleGeneration} dirtyRef={bundleDirtyRef} onSaved={bumpApplyStatus} />
        )}
      </main>
    </div>
  );
}

// BundleRootBar is CW-20260904-0019's surface for "which bundle am I
// editing, and how do I change it": T05 (CW-20260903-0009) built
// Manager.Service.Root() and bound it to the frontend, but nothing ever
// called it -- the same shape of gap CW-20260904-0004 found in
// Shell.OpenManager(). This closes it, and adds the one thing that was
// never built at all: a way to change it, via the real native macOS
// folder picker Shell.PickBundleRoot() opens (Wails'
// application.App.Dialog.OpenFile, CanChooseDirectories(true) -- not a
// hand-rolled text field; see internal/shell.Service.PickBundleRoot's own
// doc for why that's a genuine, already-available capability here and not
// an aspiration).
//
// This component does not touch the tree or the bindings list itself --
// after a successful SetRoot it only calls onRootChanged, which
// Manager()'s bundleGeneration bump turns into a fresh <Bundle> mount
// (and Bundle's own mount-time effect re-fetches Tree()). The palette's
// bindings list lives in a completely separate window/webview
// (Palette.jsx) that this component has no handle on at all; that side of
// "no restart" is Palette.jsx's own re-fetch-on-focus, not anything this
// bar calls.
function BundleRootBar({ onRootChanged, dirtyRef }) {
  // null while the first Root() call is in flight.
  const [root, setRoot] = useState(null);
  const [status, setStatus] = useState(null); // {kind: 'ok'|'err', text}
  const [busy, setBusy] = useState(false);
  // Guards against an older request's response landing after a newer one
  // -- the same class of ordering bug Bundle()'s currentRequestRef and
  // treeRequestIdRef exist to prevent, applied here to "change the root"
  // instead of "open a node" or "reload the tree".
  const requestIdRef = useRef(0);

  const loadRoot = useCallback(() => {
    ManagerAPI.Root()
      .then((r) => setRoot(r))
      .catch((e) => setStatus({ kind: "err", text: String(e?.message ?? e) }));
  }, []);

  useEffect(() => {
    loadRoot();
  }, [loadRoot]);

  // applyRoot is shared by both "Change…" (a picked directory) and "Reset
  // to default" (Manager.DefaultRoot()'s own answer): both end the same
  // way, a SetRoot call followed by telling Manager() to refresh. Both also
  // discard whatever <Bundle> currently has open (the fresh-mount-on-root-
  // change in Manager()), so both are gated on the same confirm Bundle's
  // own openNode already uses for the identical "an unsaved draft is about
  // to disappear" moment. Checked here, in the one place both callers
  // funnel through, rather than duplicated in choose()/resetToDefault(): a
  // cancel must stop the root change before SetRoot is ever called, not
  // after — no partial state change either way.
  const applyRoot = (dir) => {
    if (
      dirtyRef?.current &&
      !window.confirm("Discard unsaved changes and switch the active bundle?")
    ) {
      return;
    }
    const requestId = ++requestIdRef.current;
    setBusy(true);
    setStatus(null);
    ManagerAPI.SetRoot(dir)
      .then(() => {
        if (requestIdRef.current !== requestId) return; // superseded by a newer change
        setRoot(dir);
        setStatus({ kind: "ok", text: "Bundle root changed." });
        onRootChanged?.();
      })
      .catch((e) => {
        if (requestIdRef.current !== requestId) return;
        setStatus({ kind: "err", text: String(e?.message ?? e) });
      })
      .finally(() => {
        if (requestIdRef.current === requestId) setBusy(false);
      });
  };

  const choose = () => {
    setStatus(null);
    Shell.PickBundleRoot()
      .then((dir) => {
        if (!dir) return; // the user dismissed the dialog without picking one
        applyRoot(dir);
      })
      .catch((e) => setStatus({ kind: "err", text: String(e?.message ?? e) }));
  };

  const resetToDefault = () => {
    setStatus(null);
    ManagerAPI.DefaultRoot()
      .then((def) => applyRoot(def))
      .catch((e) => setStatus({ kind: "err", text: String(e?.message ?? e) }));
  };

  return (
    <div className="bundle-root-bar">
      <span className="bundle-root-bar-label" title={root ?? ""}>
        Bundle: <code>{root ?? "Loading…"}</code>
      </span>
      <button onClick={choose} disabled={busy} title="Pick a different bundle folder">
        {busy ? "Working…" : "Change…"}
      </button>
      <button onClick={resetToDefault} disabled={busy} title="Use the default bundle">
        Reset to default
      </button>
      {status && (
        <span className={status.kind === "ok" ? "ok" : "err"}>{status.text}</span>
      )}
    </div>
  );
}

// NAME_LIST_CUTOFF is how many paths the confirmation dialog names outright
// before falling back to "N more" — CW-20260904-0023's own "by name if the
// list is short" call, judged here: past this many, a wall of filenames
// stops being more informative than a count and starts being noise.
const NAME_LIST_CUTOFF = 8;

// namesOrCount renders a list of paths as a comma-joined string, or --
// past NAME_LIST_CUTOFF -- the first few names followed by a count of the
// rest. Returns "" for an empty list so a caller can test truthiness
// directly.
function namesOrCount(names) {
  if (!names || names.length === 0) return "";
  if (names.length <= NAME_LIST_CUTOFF) return names.join(", ");
  const shown = names.slice(0, NAME_LIST_CUTOFF).join(", ");
  return `${shown}, and ${names.length - NAME_LIST_CUTOFF} more`;
}

// ApplyBar is CW-20260904-0023's whole surface for the manager's Apply
// action: enablement (lit only when the bundle and AGENTS_HOME genuinely
// differ — internal/apply.Compare, a real content-based tree comparison,
// not a dirty flag), a status line naming what differs, and the button
// that opens the confirmation dialog Apply actually runs behind.
//
// Persistently visible in the header (see Manager()) rather than only on
// the Bundle tab, for the identical reason BundleRootBar already is: a
// save that lights this up can happen from deep in the tree/editor pane,
// and the person needs to see it regardless of which tab they're on.
//
// refreshKey is a string Manager() changes whenever Apply's own status
// might have: after a bundle-root change (bundleGeneration) and after
// every successful Save (applyStatusBump) — see Manager()'s own comment.
// This component also re-checks on window focus, the same way Bundle()'s
// tree does, for the same reason: regaining focus is when a person is
// most likely returning from having edited the bundle another way (git,
// another editor).
function ApplyBar({ refreshKey }) {
  const [status, setStatus] = useState(null); // internal/apply.Summary, or null while loading
  const [loadError, setLoadError] = useState(null);
  const [applying, setApplying] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [result, setResult] = useState(null); // {kind: 'ok'|'err', text}
  const requestIdRef = useRef(0);

  const loadStatus = useCallback(() => {
    const requestId = ++requestIdRef.current;
    ApplyAPI.Status()
      .then((s) => {
        if (requestIdRef.current !== requestId) return; // superseded by a newer check
        setStatus(s);
        setLoadError(null);
      })
      .catch((e) => {
        if (requestIdRef.current !== requestId) return;
        setLoadError(String(e?.message ?? e));
      });
  }, []);

  useEffect(() => {
    loadStatus();
    window.addEventListener("focus", loadStatus);
    return () => window.removeEventListener("focus", loadStatus);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- refreshKey is
    // a deliberate extra trigger, not a value read inside the effect.
  }, [loadStatus, refreshKey]);

  const canApply = !!status?.differs && !applying;

  // Opening the confirmation dialog is the ONLY thing this button does —
  // see confirmApply below for the only place Apply() is actually called,
  // and cancelApply for why Cancel has zero side effects.
  const openConfirm = () => {
    if (!canApply) return;
    setResult(null);
    setConfirming(true);
  };

  // cancelApply is the real abort CW-20260904-0023 calls for: it only ever
  // closes the dialog. Nothing has run yet at the point this can be
  // called — ApplyAPI.Apply() is called from nowhere but confirmApply,
  // below — so there is no partial state to unwind and nothing to undo.
  const cancelApply = () => setConfirming(false);

  // confirmApply is the one and only call site of ApplyAPI.Apply() in this
  // whole file (and, per this task's own grep check, in the whole app) —
  // reachable only from a person clicking "Apply" inside the confirmation
  // dialog this function itself closes first.
  const confirmApply = () => {
    setConfirming(false);
    setApplying(true);
    setResult(null);
    ApplyAPI.Apply()
      .then((res) => {
        setResult({
          kind: "ok",
          text: `Applied to ${res.agentsHome}.`,
        });
        loadStatus();
      })
      .catch((e) => setResult({ kind: "err", text: String(e?.message ?? e) }))
      .finally(() => setApplying(false));
  };

  return (
    <div className="apply-bar">
      <button
        onClick={openConfirm}
        disabled={!canApply}
        title={status?.differs ? `Stage ${status.description} into ${status.agentsHome}` : "Nothing to stage"}
      >
        {applying ? "Applying…" : "Apply"}
      </button>
      {status && (
        <span className={`muted apply-bar-status${status.differs ? " apply-bar-lit" : ""}`}>
          {status.differs ? status.description : "Up to date"}
        </span>
      )}
      {loadError && <span className="err">{loadError}</span>}
      {result && <span className={result.kind === "ok" ? "ok" : "err"}>{result.text}</span>}
      {confirming && (
        <ApplyConfirmDialog status={status} onConfirm={confirmApply} onCancel={cancelApply} />
      )}
    </div>
  );
}

// ApplyConfirmDialog is CW-20260904-0023's actual deliverable: the
// confirmation `make install-system` runs behind. Chrispian's own framing
// (task comment 2698, as corrected in this task's own instructions) is
// that this makes staging SAFER than typing the command by hand — the CLI
// runs the mirror with no confirmation, no summary of what's about to be
// deleted, and no way to back out. This dialog is what a person typing
// `make install-system` themselves never gets:
//
//   - the three source directories and the destination, named plainly;
//   - what will be copied or updated, reusing the exact diff the
//     enablement check already computed (never a second, possibly
//     disagreeing calculation);
//   - that staged files absent from the bundle are DELETED — the
//     --delete this mirror runs with, which the CLI never announces —
//     with a count, and by name when the list is short;
//   - that this does not touch ~/.claude, and does not run
//     `cairn install` — the two things this task deliberately does NOT
//     do, said outright so neither is a silent assumption;
//   - a Cancel that is a real abort: see ApplyBar's own cancelApply,
//     which is the only thing this dialog's Cancel button calls.
function ApplyConfirmDialog({ status, onConfirm, onCancel }) {
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onCancel();
      }
    };
    // Capture phase: this dialog sits inside the manager window, which
    // itself hides on Escape (HideOnEscape) -- without capturing first,
    // that native/runtime handling could fire on the same keypress this
    // dialog means to just close. Escape here means "cancel the dialog",
    // never "hide the whole manager mid-confirmation".
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [onCancel]);

  const kinds = status?.kinds ?? [];
  const copied = kinds.flatMap((k) => [...k.added, ...k.changed].map((n) => `${k.kind}/${n}`));
  const deleted = kinds.flatMap((k) => k.removed.map((n) => `${k.kind}/${n}`));

  return (
    <div className="apply-confirm-overlay" onClick={onCancel}>
      <div className="apply-confirm" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <h2>Apply staged changes?</h2>

        <p>
          Runs <code>make install-system</code> in the bundle, mirroring{" "}
          <code>{status?.bundleRoot}/{"{templates,skills,prompts}"}</code> onto{" "}
          <code>{status?.agentsHome}/{"{templates,skills,prompts}"}</code>.
        </p>

        {copied.length > 0 && (
          <p className="apply-confirm-list">
            <strong>Copied or updated ({copied.length}):</strong> {namesOrCount(copied)}
          </p>
        )}

        {deleted.length > 0 ? (
          <p className="warn apply-confirm-list">
            <strong>Deleted ({deleted.length}):</strong> {namesOrCount(deleted)}
            <br />
            Staged at the destination, but no longer in the bundle — this mirror runs
            with <code>--delete</code>, so these are removed. Any hand-edit made
            directly to the staged copy, rather than the bundle, is lost.
          </p>
        ) : (
          <p className="muted">Nothing will be deleted — every staged file is still in the bundle.</p>
        )}

        <p className="muted">
          Does not touch <code>~/.claude</code>. Does not run <code>cairn install</code>.
        </p>

        <div className="apply-confirm-actions">
          <button className="apply-confirm-run" onClick={onConfirm} autoFocus>
            Apply
          </button>
          <button onClick={onCancel}>Cancel</button>
        </div>
      </div>
    </div>
  );
}

// KIND_META labels and color-codes each of the six artifact kinds. It exists
// so a row or an open tab can say what it is at a glance — necessary because
// every one of the eight role-prose files shares a basename with a profile
// (profiles/architect.md vs. templates/roles/architect.md), and being in a
// different tree group is not sufficient on its own: see task
// CW-20260903-0009's "conflation trap".
const KIND_META = {
  profile: { label: "Profile", plural: "Profiles", color: "#45c7b8" },
  "role-prose": { label: "Role prose", plural: "Role prose", color: "#c78ee0" },
  template: { label: "Template", plural: "Templates", color: "#e0b04b" },
  prompt: { label: "Prompt", plural: "Prompts", color: "#e0708a" },
  skill: { label: "Skill", plural: "Skills", color: "#7fb0e0" },
  hook: { label: "Hook", plural: "Hooks", color: "#e08a6c" },
  binding: { label: "Binding", plural: "Bindings", color: "#8fce7a" },
};

function kindMeta(kind) {
  return KIND_META[kind] ?? { label: kind, plural: kind, color: "#8a929b" };
}

function KindBadge({ kind }) {
  const meta = kindMeta(kind);
  return (
    <span className="kind-badge" style={{ color: meta.color, borderColor: meta.color }}>
      {meta.label}
    </span>
  );
}

function refFor(node) {
  return `${node.kind} ${node.id}`;
}

function Bundle({ dirtyRef: sharedDirtyRef, onSaved } = {}) {
  const [tree, setTree] = useState(null);
  const [treeError, setTreeError] = useState(null);
  const [selectedRef, setSelectedRef] = useState(null);
  const [selectedNode, setSelectedNode] = useState(null);

  const [originalText, setOriginalText] = useState("");
  const [draftText, setDraftText] = useState("");
  const [openError, setOpenError] = useState(null);
  const [status, setStatus] = useState(null); // {kind: 'ok'|'err', text}
  const [saving, setSaving] = useState(false);
  const [loadingContent, setLoadingContent] = useState(false);
  // loaded is true only once the *currently selected* node's bytes have been
  // successfully decoded into originalText/draftText. It exists so Save can
  // never fire against stale or empty state: originalText/draftText are
  // reset to "" the instant a new node is opened (see openNode), so without
  // this flag "" == "" would read as "not dirty, safe to save" for a file
  // that actually failed to open (including the invalid-UTF-8 case below),
  // and Save would silently overwrite it with nothing.
  const [loaded, setLoaded] = useState(false);

  // creating toggles the "new artifact" panel (CW-20260903-0010). It is
  // deliberately independent of every piece of state above: opening it
  // never touches the currently-open editor, and closing it (Cancel, or a
  // successful create) never touches anything else either.
  const [creating, setCreating] = useState(false);

  const dirty = selectedNode != null && draftText !== originalText;
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  // Mirrored into the ref Manager() shares with BundleRootBar, so a root
  // change can ask before discarding this instance's draft the same way
  // openNode already does for a plain node switch. Cleared on unmount (tab
  // switch away, or the key-bump remount a root change itself causes) so a
  // stale "dirty" from an instance that's already gone can never block —
  // or wrongly warn about — a later action once there is nothing left it
  // would actually be discarding.
  if (sharedDirtyRef) sharedDirtyRef.current = dirty;
  useEffect(() => {
    if (!sharedDirtyRef) return;
    return () => {
      sharedDirtyRef.current = false;
    };
  }, [sharedDirtyRef]);
  const canSave = selectedNode != null && loaded && !openError && !loadingContent && !saving;

  // currentRequestRef names whichever node openNode most recently started a
  // request for, set synchronously the moment that request starts — before
  // any await/`.then()`. Open()'s completion handlers (and Save's, since a
  // save's write is correct regardless but its UI update is not) compare
  // against this before touching originalText/draftText/loaded/openError.
  //
  // Without this: click a slow file A, then a fast file B before A resolves.
  // B's response lands first and renders correctly. A's stale response then
  // lands and — with nothing to say it is stale — unconditionally overwrites
  // originalText/draftText with A's content while selectedNode is still B.
  // canSave reads true. Save writes B's file with A's bytes: a silent
  // cross-file byte-corruption path that looks, on screen, like an ordinary
  // successful save. A resolved promise is not evidence it is the most
  // recently REQUESTED one, only the most recently SETTLED one, and those
  // are different claims whenever two requests are in flight at once.
  const currentRequestRef = useRef(null);

  // treeRequestIdRef is the same shape of guard applied to loadTree(), which
  // is not tied to node identity: two Tree() calls can overlap (the focus
  // listener firing while a manual Refresh is still in flight, say), and
  // without an ordering guard the older response can land after the newer
  // one and silently roll the displayed tree back to stale data.
  const treeRequestIdRef = useRef(0);

  const loadTree = useCallback(() => {
    const requestId = ++treeRequestIdRef.current;
    ManagerAPI.Tree()
      .then((t) => {
        if (treeRequestIdRef.current !== requestId) return; // superseded by a newer refresh
        setTree(t);
        setTreeError(null);
      })
      .catch((e) => {
        if (treeRequestIdRef.current !== requestId) return;
        setTreeError(String(e?.message ?? e));
      });
  }, []);

  useEffect(() => {
    loadTree();
    // The tree is read fresh from disk on every call (bundle.Bundle caches
    // nothing) — regaining focus is when a user is most likely coming back
    // from having edited the bundle another way (git, another editor), so
    // that is when a stale tree would be most visible.
    window.addEventListener("focus", loadTree);
    return () => window.removeEventListener("focus", loadTree);
  }, [loadTree]);

  const openNode = (node) => {
    if (dirtyRef.current && !window.confirm(`Discard unsaved changes to ${node.relPath}?`)) {
      return;
    }
    const myRef = refFor(node);
    // Set synchronously, before the request goes out: this is what a later
    // `.then()` compares itself against to tell whether it is still the most
    // recently REQUESTED response, not merely the most recently SETTLED one.
    currentRequestRef.current = myRef;
    setSelectedRef(myRef);
    setSelectedNode(node);
    setOpenError(null);
    setStatus(null);
    setLoaded(false);
    // Reset immediately, not just on success: while this Open() is in
    // flight (or if it fails) selectedNode already names the new node, and
    // draftText/originalText must not still be the previous file's text —
    // otherwise canSave's dirty-adjacent bookkeeping would be checking the
    // wrong file's content against the wrong file's ref.
    setOriginalText("");
    setDraftText("");
    setLoadingContent(true);
    ManagerAPI.Open(node.kind, node.id)
      .then((content) => {
        if (currentRequestRef.current !== myRef) return; // a newer selection superseded this
        let text;
        try {
          text = base64ToText(content.bytes);
        } catch (decodeErr) {
          // TextDecoder with {fatal: true} threw: these bytes are not valid
          // UTF-8. Re-encoding whatever a <textarea> would show in that case
          // does not reproduce the original bytes (D5 forbids that
          // silently), so this file is refused as text rather than opened
          // and corrupted on the next save. See src/bytes.js.
          throw new Error(
            `${node.relPath} contains bytes that are not valid UTF-8 text, so Tachyon can't ` +
              "edit it here without risking corrupting it on save. Edit it with another tool.",
          );
        }
        setOriginalText(text);
        setDraftText(text);
        setLoaded(true);
      })
      .catch((e) => {
        if (currentRequestRef.current !== myRef) return;
        setOpenError(String(e?.message ?? e));
      })
      .finally(() => {
        if (currentRequestRef.current !== myRef) return;
        setLoadingContent(false);
      });
  };

  const save = useCallback(() => {
    // Re-checked here, not just at the button: onEditorKeyDown's Cmd+S
    // reaches this directly and does not go through the button's disabled
    // attribute.
    if (!selectedNode || !loaded || openError || loadingContent || saving) return;
    // Captured now, synchronously: selectedNode/draftText are what the user
    // meant to save at the moment they clicked. The write below always
    // targets this node's kind/id regardless of what happens to selection
    // afterward — the guard only protects the UI-update half of this
    // function, which must NOT paint a different, now-selected file with
    // this save's result.
    const node = selectedNode;
    const myRef = refFor(node);
    setSaving(true);
    setStatus(null);
    ManagerAPI.Save(node.kind, node.id, textToBase64(draftText))
      .then((content) => {
        // The write already landed on disk regardless of which branch
        // below runs (node.kind/node.id were fixed above, before any
        // await), so Apply's own enablement (CW-20260904-0023) must be
        // re-evaluated either way -- a template/skill/prompt save is
        // exactly the kind of change that can light up, or dark out, the
        // Apply button, and "the user moved on to a different node" is not
        // a reason to skip re-checking it.
        onSaved?.();
        if (currentRequestRef.current !== myRef) {
          // The user moved on to a different node while this save was in
          // flight. The write already landed correctly (node.kind/node.id
          // were fixed above, before any await), so nothing about the saved
          // file is wrong — only applying this response's text to whatever
          // is on screen NOW would be, since that is a different file's
          // editor. Still worth refreshing the tree: the artifact that was
          // actually written may have a changed header projection.
          loadTree();
          return;
        }
        const text = base64ToText(content.bytes);
        setOriginalText(text);
        setDraftText(text);
        setStatus({ kind: "ok", text: `Saved ${content.relPath}.` });
        // A save can only ever change the artifact just written, but the
        // header shown for it in the tree (id/name/description/extends) is a
        // display projection of the same bytes, so refresh the tree too.
        loadTree();
      })
      .catch((e) => {
        if (currentRequestRef.current !== myRef) return;
        setStatus({ kind: "err", text: String(e?.message ?? e) });
      })
      // Not guarded: `saving` is global "a save is in flight" state, not
      // per-node display state — it gates the Save button/Cmd+S regardless
      // of which node is now selected, so it must always clear when the
      // operation that set it actually finishes, or navigating away mid-save
      // would leave Save permanently disabled for every node after it.
      .finally(() => setSaving(false));
  }, [selectedNode, loaded, openError, loadingContent, saving, draftText, loadTree, onSaved]);

  const onEditorKeyDown = (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "s") {
      e.preventDefault();
      save();
    }
  };

  return (
    <div className="bundle-view">
      <aside className="tree-pane">
        <div className="tree-header">
          <span className="root-label" title={tree?.root ?? ""}>
            {tree ? tree.root : "Loading bundle…"}
          </span>
          <button onClick={loadTree} title="Re-read the bundle from disk">
            Refresh
          </button>
          <button onClick={() => setCreating(true)} title="Create a new artifact">
            + New
          </button>
        </div>
        {/* CW-20260903-0010's entry point. See NewArtifact.jsx; this is the
            panel's only mount point in the whole manager window. */}
        {creating && (
          <NewArtifact
            onCancel={() => setCreating(false)}
            onCreated={(content) => {
              setCreating(false);
              loadTree();
              openNode(content);
            }}
          />
        )}
        {treeError && <p className="err tree-msg">{treeError}</p>}
        {/* CW-20260904-0019: tree.state distinguishes "this directory was
            never a bundle" from "this bundle is genuinely empty" — two
            situations that used to render identically (an empty tree, six
            groups all saying "none"), which is exactly the failure mode
            CW-20260904-0002 already fixed once for the palette's bindings
            list and this task's own record says not to reintroduce here.
            "unrecognized" replaces the tree entirely with a clear reason,
            the same way Palette.jsx's "missing"/"unreadable" states
            replace its list rather than sitting on top of it. */}
        {tree && tree.state === "unrecognized" && (
          <div className="tree-msg">
            <div className="err">This doesn't look like a Cairn bundle</div>
            <div className="muted" style={{ marginTop: 8 }}>
              None of <code>profiles/</code>, <code>templates/</code>, <code>prompts/</code>,{" "}
              <code>skills/</code>, <code>hooks/</code> or <code>bindings/</code> exist under{" "}
              <code>{tree.root}</code>.
              Pick a different folder above, or reset to the default bundle.
            </div>
          </div>
        )}
        {tree && tree.state !== "unrecognized" && (
          <div className="tree-groups">
            {/* ?? [] is defense in depth, not the fix: the real guarantee is
                that internal/manager.Tree() never emits a null nodes array
                (see manager.go). Guarding here too means a future regression
                on the Go side degrades to an empty group instead of crashing
                this render and blanking the whole window. */}
            {(tree.groups ?? []).map((g) => (
              <TreeGroup key={g.kind} group={g} selectedRef={selectedRef} onOpen={openNode} />
            ))}
          </div>
        )}
      </aside>
      <section className="editor-pane">
        {!selectedNode && (
          <p className="note editor-empty">
            Select an artifact on the left to open it. Saving writes its bytes back
            unchanged — nothing here parses, reformats or validates content.
          </p>
        )}
        {selectedNode && (
          <>
            <div className="editor-header">
              <KindBadge kind={selectedNode.kind} />
              <code className="rel-path">{selectedNode.relPath}</code>
              {dirty && <span className="dirty-dot" title="Unsaved changes" />}
              <div className="editor-actions">
                <button onClick={save} disabled={!canSave}>
                  {saving ? "Saving…" : loadingContent ? "Loading…" : "Save"}
                </button>
              </div>
            </div>
            {openError && <p className="err tree-msg">{openError}</p>}
            {!openError && (
              <textarea
                className="editor"
                spellCheck={false}
                value={draftText}
                disabled={loadingContent || !loaded}
                onChange={(e) => setDraftText(e.target.value)}
                onKeyDown={onEditorKeyDown}
              />
            )}
            {status && <p className={status.kind === "ok" ? "ok status-line" : "err status-line"}>{status.text}</p>}
          </>
        )}
      </section>
    </div>
  );
}

function TreeGroup({ group, selectedRef, onOpen }) {
  // Same defense-in-depth as tree.groups above: bindings/ does not exist in
  // the live bundle today, so this is the group that is empty in practice,
  // right now, not a hypothetical.
  const nodes = group.nodes ?? [];
  return (
    <div className="tree-group">
      <div className="tree-group-label">
        {group.label} <span className="muted">({group.count})</span>
      </div>
      {nodes.length === 0 && <div className="tree-empty muted">none</div>}
      {nodes.map((n) => (
        <TreeRow key={refFor(n)} node={n} active={refFor(n) === selectedRef} onOpen={onOpen} />
      ))}
    </div>
  );
}

function TreeRow({ node, active, onOpen }) {
  const meta = kindMeta(node.kind);
  return (
    <button
      className={`tree-row${active ? " active" : ""}`}
      onClick={() => onOpen(node)}
      title={node.relPath}
    >
      <span className="tree-row-dot" style={{ background: meta.color }} />
      <span className="tree-row-text">
        <span className="tree-row-id">{node.id}</span>
        <span className="tree-row-path">{node.relPath}</span>
        {node.header?.present && node.header?.name && (
          <span className="tree-row-name">{node.header.name}</span>
        )}
        {node.note && <span className="tree-row-note warn">{node.note}</span>}
      </span>
    </button>
  );
}

function Settings() {
  const [settings, setSettings] = useState(null);
  const [draft, setDraft] = useState("");
  const [status, setStatus] = useState(null);
  // CW-20260903-0019's manual boot-directory sweep. Independent of the
  // hotkey state above -- its own request-id guard (sweeping is a slower,
  // real filesystem-and-lsof operation, so a double-click must not let an
  // older response clobber a newer one) and its own status display.
  const [sweep, setSweep] = useState(null);
  const [sweeping, setSweeping] = useState(false);
  const sweepRequestIdRef = useRef(0);
  // Same class of guard as Bundle()'s currentRequestRef/treeRequestIdRef:
  // clicking "Bind and save" twice in quick succession with a changed draft
  // in between (or Enter, then a click before the first call returns) starts
  // two SetHotkey calls, and without an ordering check the OLDER response
  // could land after the newer one and roll the shown hotkey back to a
  // stale value even though the newer bind is what actually took effect.
  const requestIdRef = useRef(0);

  const load = () =>
    Shell.Settings().then((s) => {
      setSettings(s);
      setDraft(s.hotkey);
    });

  useEffect(() => {
    load().catch((e) => setStatus({ kind: "err", text: String(e) }));
  }, []);

  const save = async () => {
    const requestId = ++requestIdRef.current;
    setStatus(null);
    try {
      const res = await Shell.SetHotkey(draft);
      if (requestIdRef.current !== requestId) return; // superseded by a newer save
      setSettings(res.settings);
      setDraft(res.settings.hotkey);
      setStatus(
        res.advisory
          ? { kind: "warn", text: res.advisory }
          : { kind: "ok", text: `Bound to ${res.settings.hotkey} and saved.` },
      );
    } catch (e) {
      if (requestIdRef.current !== requestId) return;
      setStatus({ kind: "err", text: String(e.message ?? e) });
    }
  };

  // runSweep is CW-20260903-0019's manual action: it calls the exact same
  // Sweep the app already ran once, automatically, right after this
  // window last started -- see internal/shell.Shell.wireBootSweep and
  // bridge.js's own comment on Shell.SweepBootDirectories. There is no
  // periodic re-check anywhere; this button, and the one automatic run at
  // startup, are the only two times it ever runs.
  const runSweep = async () => {
    const requestId = ++sweepRequestIdRef.current;
    setSweeping(true);
    setSweep(null);
    try {
      const report = await Shell.SweepBootDirectories();
      if (sweepRequestIdRef.current !== requestId) return; // superseded by a newer sweep
      setSweep({ kind: report.guardOK ? "ok" : "warn", report });
    } catch (e) {
      if (sweepRequestIdRef.current !== requestId) return;
      setSweep({ kind: "err", error: String(e.message ?? e) });
    } finally {
      if (sweepRequestIdRef.current === requestId) setSweeping(false);
    }
  };

  if (!settings) return <p className="note">Loading…</p>;

  return (
    <div className="settings-pane">
      <div className="settings">
        <div>
          <label htmlFor="hk">Global hotkey</label>
          <input
            id="hk"
            value={draft}
            spellCheck={false}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && save()}
          />
        </div>
        <div>
          <button onClick={save}>Bind and save</button>{" "}
          <button onClick={() => setDraft(settings.defaultHotkey)}>Reset to default</button>
        </div>
        {status && <p className={status.kind}>{status.text}</p>}
        <p className="note">
          Currently bound: <kbd>{settings.hotkey}</kbd>
          {settings.registered ? "" : " — not registered with the OS"}
          <br />
          Written to <code>{settings.path}</code>
        </p>
        <p className="warn">
          macOS reports no error when another application already owns a
          combination. Registration succeeding is not evidence the hotkey works —
          press it and see. If nothing happens, pick a different one here.
        </p>
        <div>
          <label>Old boot directories</label>
          <p className="note">
            Relaunching a binding leaves its previous boot directory behind,
            renamed aside rather than deleted, in case a session was still
            using it. This checks each one and removes only the ones nothing
            still has open as a working directory — it already ran once,
            automatically, when Tachyon last started; run it again here any
            time.
          </p>
          <button onClick={runSweep} disabled={sweeping}>
            {sweeping ? "Checking…" : "Clean up now"}
          </button>
        </div>
        {sweep && (
          <p className={sweep.kind}>
            {sweep.error
              ? `Sweep failed: ${sweep.error}`
              : sweep.report.guardOK
                ? `Removed ${sweep.report.swept.length}, kept ${sweep.report.skipped.length}.`
                : `Could not verify it was safe to remove anything this time, so nothing was removed. ${sweep.report.guardDetail}`}
          </p>
        )}
      </div>
    </div>
  );
}
