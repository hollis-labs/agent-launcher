import { useCallback, useEffect, useRef, useState } from "react";
import { Manager as ManagerAPI, Shell } from "./bridge.js";
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
      </header>
      <main className={tab === "bundle" ? "no-pad" : undefined}>
        {tab === "settings" ? <Settings /> : <Bundle />}
      </main>
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

function Bundle() {
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
  }, [selectedNode, loaded, openError, loadingContent, saving, draftText, loadTree]);

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
        {tree && (
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
