import { useEffect, useState } from "react";
import { Shell } from "./bridge.js";

// The bundle tree and the text editor are CW-20260903-0009. What lands here is
// the window plus the settings surface the hotkey needs (see Settings below):
// registration cannot report a cross-process conflict, so a user whose default
// silently collides needs a way out that is not a rebuild.
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
      <main>{tab === "settings" ? <Settings /> : <Bundle />}</main>
    </div>
  );
}

function Bundle() {
  return (
    <p className="note">
      The bundle tree and the editor land in CW-20260903-0009.
      <br />
      This window exists to prove its posture: it is resizable, it remembers its
      size across restarts, and it does <strong>not</strong> dismiss when it
      loses focus.
    </p>
  );
}

function Settings() {
  const [settings, setSettings] = useState(null);
  const [draft, setDraft] = useState("");
  const [status, setStatus] = useState(null);

  const load = () =>
    Shell.Settings().then((s) => {
      setSettings(s);
      setDraft(s.hotkey);
    });

  useEffect(() => {
    load().catch((e) => setStatus({ kind: "err", text: String(e) }));
  }, []);

  const save = async () => {
    setStatus(null);
    try {
      const res = await Shell.SetHotkey(draft);
      setSettings(res.settings);
      setDraft(res.settings.hotkey);
      setStatus(
        res.advisory
          ? { kind: "warn", text: res.advisory }
          : { kind: "ok", text: `Bound to ${res.settings.hotkey} and saved.` },
      );
    } catch (e) {
      setStatus({ kind: "err", text: String(e.message ?? e) });
    }
  };

  if (!settings) return <p className="note">Loading…</p>;

  return (
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
    </div>
  );
}
