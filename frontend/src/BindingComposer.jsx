import { useMemo, useState } from "react";
import { BindingComposer as ComposerAPI, Shell } from "./bridge.js";

function AdditiveField({ label, hint, values, setValues, suggestions }) {
  const [draft, setDraft] = useState("");
  const listID = `composer-${label.toLowerCase()}`;
  const add = () => {
    const value = draft.trim();
    if (!value) return;
    setValues([...values, value]);
    setDraft("");
  };
  return (
    <div className="binding-composer-field">
      <label>{label} <span>{hint}</span></label>
      <div className="binding-composer-add-row">
        <input
          value={draft}
          list={listID}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } }}
          placeholder={`Add ${label.toLowerCase().replace(/s$/, "")}`}
        />
        <datalist id={listID}>{suggestions.map((v) => <option key={v} value={v} />)}</datalist>
        <button type="button" onClick={add} disabled={!draft.trim()}>Add</button>
      </div>
      {values.length > 0 && (
        <ol className="binding-composer-values">
          {values.map((value, index) => (
            <li key={`${value}-${index}`}>
              <code>{value}</code>
              {label === "Parts" && <>
                <button type="button" disabled={index === 0} onClick={() => {
                  const next = [...values]; [next[index - 1], next[index]] = [next[index], next[index - 1]]; setValues(next);
                }}>↑</button>
                <button type="button" disabled={index === values.length - 1} onClick={() => {
                  const next = [...values]; [next[index + 1], next[index]] = [next[index], next[index + 1]]; setValues(next);
                }}>↓</button>
              </>}
              <button type="button" aria-label={`Remove ${value}`} onClick={() => setValues(values.filter((_, i) => i !== index))}>Remove</button>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function SetsField({ values, setValues }) {
  const [slot, setSlot] = useState("");
  const [value, setValue] = useState("");
  const add = () => {
    if (!slot.trim()) return;
    setValues([...values, { slot: slot.trim(), value }]); setSlot(""); setValue("");
  };
  return <div className="binding-composer-field">
    <label>One-off sets <span>launch only · never saved</span></label>
    <div className="binding-composer-set-row">
      <input value={slot} onChange={(e) => setSlot(e.target.value)} placeholder="slot" />
      <span>=</span>
      <input value={value} onChange={(e) => setValue(e.target.value)} placeholder="value" onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } }} />
      <button type="button" onClick={add} disabled={!slot.trim()}>Add</button>
    </div>
    {values.length > 0 && <ol className="binding-composer-values">{values.map((set, index) => <li key={`${set.slot}-${index}`}><code>{set.slot}={set.value}</code><button type="button" onClick={() => setValues(values.filter((_, i) => i !== index))}>Remove</button></li>)}</ol>}
  </div>;
}

export default function BindingComposer({ tree, projects, projectError, onSaved }) {
  const groups = useMemo(() => Object.fromEntries((tree?.groups ?? []).map((g) => [g.kind, g.nodes.map((n) => n.id)])), [tree]);
  const profiles = groups.profile ?? [];
  const [name, setName] = useState("");
  const [profile, setProfile] = useState("");
  const [parts, setParts] = useState([]);
  const [skills, setSkills] = useState([]);
  const [prompts, setPrompts] = useState([]);
  const [sets, setSets] = useState([]);
  const [scope, setScope] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState(null);
  const input = { name: name.trim(), profile: profile.trim(), parts, skills, prompts, sets, scope: scope.trim() };

  const run = (action) => {
    if (!profile.trim() || (action !== "Launch" && !name.trim())) return;
    setBusy(true); setStatus(null);
    ComposerAPI[action](input).then((result) => {
      setStatus({ kind: "ok", text: action === "Launch" ? `Launched ${profile.trim()}.` : `${action === "Save" ? "Saved" : "Saved and launched"} ${result.relPath}.` });
      if (action !== "Launch") onSaved?.();
    }).catch((e) => {
      // Save + Launch writes before it boots. Refreshing is harmless when
      // save failed, and essential when the error reports a saved binding
      // followed by a failed launch.
      if (action === "SaveAndLaunch") onSaved?.();
      setStatus({ kind: "err", text: String(e?.message ?? e) });
    }).finally(() => setBusy(false));
  };

  const pickScope = () => Shell.PickProjectPath().then((path) => { if (path) setScope(path); }).catch((e) => setStatus({ kind: "err", text: String(e?.message ?? e) }));

  return <form className="binding-composer" onSubmit={(e) => { e.preventDefault(); run("SaveAndLaunch"); }}>
    <div className="binding-composer-heading"><h2>Compose binding</h2><p>Creates a new binding. Existing bindings remain text-only.</p></div>
    <div className="binding-composer-stack">
      <div className="binding-composer-field">
        <label>Binding name <span>required to save</span></label>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="eng-project" autoFocus />
      </div>
      <div className="binding-composer-field">
        <label>Base profile <span>profile</span></label>
        <input value={profile} list="composer-profiles" onChange={(e) => setProfile(e.target.value)} placeholder="engineer" />
        <datalist id="composer-profiles">{profiles.map((v) => <option key={v} value={v} />)}</datalist>
      </div>
      <AdditiveField label="Parts" hint="ordered · --with" values={parts} setValues={setParts} suggestions={profiles} />
      <AdditiveField label="Skills" hint="additive only" values={skills} setValues={setSkills} suggestions={groups.skill ?? []} />
      <AdditiveField label="Prompts" hint="additive only" values={prompts} setValues={setPrompts} suggestions={groups.prompt ?? []} />
      <SetsField values={sets} setValues={setSets} />
      <div className="binding-composer-field">
        <label>Scope <span>literal path</span></label>
        <div className="binding-composer-scope-row">
          <input value={scope} list="composer-projects" onChange={(e) => setScope(e.target.value)} placeholder="Paste or type a path" />
          <datalist id="composer-projects">{(projects ?? []).map((view) => <option key={view.project.name} value={view.project.path}>{view.project.name}</option>)}</datalist>
          <button type="button" onClick={pickScope}>Choose folder…</button>
        </div>
        {(projects ?? []).length > 0 && <div className="binding-composer-projects"><span>Projects:</span>{projects.map((view) => <button type="button" key={view.project.name} title={view.project.path} onClick={() => setScope(view.project.path)}>{view.project.name}</button>)}</div>}
        {projectError && <span className="err">Projects unavailable: {projectError}</span>}
      </div>
    </div>
    <div className="binding-composer-actions">
      {status && <span className={status.kind}>{status.text}</span>}
      <button type="button" disabled={busy || !name.trim() || !profile.trim()} onClick={() => run("Save")}>{busy ? "Working…" : "Save"}</button>
      <button type="button" disabled={busy || !profile.trim()} onClick={() => run("Launch")}>Launch</button>
      <button className="primary" type="submit" disabled={busy || !name.trim() || !profile.trim()}>Save + Launch</button>
    </div>
  </form>;
}
