import { useMemo, useState } from "react";
import { LaunchComposer as ComposerAPI, LaunchProfile, Shell } from "./bridge.js";
import { acceptTopSuggestion } from "./autocomplete.js";
import EffectiveSkills from "./EffectiveSkills.jsx";

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
    <div className="launch-composer-field">
      <label>{label} <span>{hint}</span></label>
      <div className="launch-composer-add-row">
        <input
          value={draft}
          list={listID}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (acceptTopSuggestion(e, draft, suggestions, setDraft)) return;
            if (e.key === "Enter") { e.preventDefault(); add(); }
          }}
          placeholder={`Add ${label.toLowerCase().replace(/s$/, "")}`}
        />
        <datalist id={listID}>{suggestions.map((v) => <option key={v} value={v} />)}</datalist>
        <button type="button" onClick={add} disabled={!draft.trim()}>Add</button>
      </div>
      {values.length > 0 && (
        <ol className="launch-composer-values">
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
  return <div className="launch-composer-field">
    <label>One-off sets <span>launch only · never saved</span></label>
    <div className="launch-composer-set-row">
      <input value={slot} onChange={(e) => setSlot(e.target.value)} placeholder="slot" />
      <span>=</span>
      <input value={value} onChange={(e) => setValue(e.target.value)} placeholder="value" onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } }} />
      <button type="button" onClick={add} disabled={!slot.trim()}>Add</button>
    </div>
    {values.length > 0 && <ol className="launch-composer-values">{values.map((set, index) => <li key={`${set.slot}-${index}`}><code>{set.slot}={set.value}</code><button type="button" onClick={() => setValues(values.filter((_, i) => i !== index))}>Remove</button></li>)}</ol>}
  </div>;
}

// LaunchProfileComposer writes a launch profile: the file that says HOW an
// agent runs. It replaced the binding composer, which wrote a format of
// Tachyon's own into the bundle that cairn could not read.
//
// Only the durable half of the form is saved — provider, skills, prompts.
// A target, a scope, a one-off part and a set are facts about ONE launch:
// a scope cannot be saved at all (cairn refuses `scope:` as frontmatter),
// and a composition worth reusing is a profile worth naming, which is what
// saving one here produces. Save's result names whatever it did not carry
// so the person is told rather than left to notice.
export default function LaunchProfileComposer({ tree, projects, projectError, onSaved }) {
  const groups = useMemo(() => Object.fromEntries((tree?.groups ?? []).map((g) => [g.kind, g.nodes.map((n) => n.id)])), [tree]);
  const profiles = groups.profile ?? [];
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("claude");
  const [profile, setProfile] = useState("");
  const [parts, setParts] = useState([]);
  const [skills, setSkills] = useState([]);
  const [prompts, setPrompts] = useState([]);
  const [sets, setSets] = useState([]);
  const [scope, setScope] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState(null);
  const input = {
    name: name.trim(), provider: provider.trim(), target: profile.trim(),
    parts, skills, prompts, sets, scope: scope.trim(),
  };
  // The preview shows what the LAUNCH resolves to, so it carries the launch
  // profile by name exactly as a launch would — a preview that skipped it
  // would resolve a different provider and show the wrong skills.
  const previewInput = {
    target: profile.trim(), launchProfile: name.trim(),
    parts, skills, prompts, sets, scope: scope.trim(),
  };

  const run = (action) => {
    if (action === "Launch" ? !profile.trim() : !name.trim() || !provider.trim()) return;
    setBusy(true); setStatus(null);
    ComposerAPI[action](input).then((result) => {
      if (action === "Launch") {
        setStatus({ kind: "ok", text: `Launched ${profile.trim()}.` });
        return;
      }
      const dropped = result?.dropped ?? [];
      const note = dropped.length > 0
        ? ` ${dropped.join(", ")} ${dropped.length === 1 ? "is" : "are"} per-launch and was not saved.`
        : "";
      setStatus({
        kind: "ok",
        text: `${action === "Save" ? "Saved" : "Saved and launched"} ${result.name}.${note}`,
      });
      onSaved?.();
    }).catch((e) => {
      // Save + Launch writes before it boots. Refreshing is harmless when
      // save failed, and essential when the error reports a saved profile
      // followed by a failed launch.
      if (action === "SaveAndLaunch") onSaved?.();
      setStatus({ kind: "err", text: String(e?.message ?? e) });
    }).finally(() => setBusy(false));
  };

  const pickScope = () => Shell.PickProjectPath().then((path) => { if (path) setScope(path); }).catch((e) => setStatus({ kind: "err", text: String(e?.message ?? e) }));

  return <form className="launch-composer" onSubmit={(e) => { e.preventDefault(); run("SaveAndLaunch"); }}>
    <div className="launch-composer-heading">
      <h2>New launch profile</h2>
      <p>
        A launch profile says how an agent runs — the harness and its
        settings — and is an ordinary Cairn part, so what is saved here is
        exactly what a launch consumes. Existing ones stay text-editable.
      </p>
    </div>
    <div className="launch-composer-stack">
      <div className="launch-composer-field">
        <label>Launch profile name <span>required to save</span></label>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="codex" autoFocus />
      </div>
      <div className="launch-composer-field">
        {/* The one key a launch profile must carry. No profile in the bundle
            declares a provider, and cairn refuses to render without one
            rather than writing one harness's files into another's directory,
            so this is not a default that can be left blank. Free text with a
            datalist rather than a dropdown: cairn distinguishes "a provider
            cairn cannot render yet" from "not a provider at all", and a
            restricted control would collapse the two. */}
        <label>Provider <span>required · the harness this materializes into</span></label>
        <input
          value={provider}
          list="composer-providers"
          onChange={(e) => setProvider(e.target.value)}
          onKeyDown={(e) => acceptTopSuggestion(e, provider, ["claude", "codex"], setProvider)}
          placeholder="claude"
        />
        <datalist id="composer-providers">
          <option value="claude" />
          <option value="codex" />
        </datalist>
      </div>
      <div className="launch-composer-field">
        <label>Agent profile to launch <span>not saved · this launch only</span></label>
        <input
          value={profile}
          list="composer-profiles"
          onChange={(e) => setProfile(e.target.value)}
          onKeyDown={(e) => acceptTopSuggestion(e, profile, profiles, setProfile)}
          placeholder="engineer"
        />
        <datalist id="composer-profiles">{profiles.map((v) => <option key={v} value={v} />)}</datalist>
      </div>
      <AdditiveField label="Parts" hint="ordered · --with · launch only, never saved" values={parts} setValues={setParts} suggestions={profiles} />
      <div className="effective-skills-pair">
        <AdditiveField label="Skills" hint="additive only · saved as spec.skills" values={skills} setValues={setSkills} suggestions={groups.skill ?? []} />
        <EffectiveSkills input={previewInput} />
      </div>
      <AdditiveField label="Prompts" hint="additive only · saved as spec.prompts" values={prompts} setValues={setPrompts} suggestions={groups.prompt ?? []} />
      <SetsField values={sets} setValues={setSets} />
      <div className="launch-composer-field">
        <label>Project scope <span>literal path · launch only, never saved</span></label>
        <div className="launch-composer-scope-row">
          <input
            value={scope}
            list="composer-projects"
            onChange={(e) => setScope(e.target.value)}
            onKeyDown={(e) => acceptTopSuggestion(
              e,
              scope,
              (projects ?? []).map((view) => ({ value: view.project.path, search: [view.project.name] })),
              setScope,
            )}
            placeholder="Paste or type a path"
          />
          <datalist id="composer-projects">{(projects ?? []).map((view) => <option key={view.project.name} value={view.project.path}>{view.project.name}</option>)}</datalist>
          <button type="button" onClick={pickScope}>Choose folder…</button>
        </div>
        {(projects ?? []).length > 0 && <div className="launch-composer-projects"><span>Projects:</span>{projects.map((view) => <button type="button" key={view.project.name} title={view.project.path} onClick={() => setScope(view.project.path)}>{view.project.name}</button>)}</div>}
        {projectError && <span className="err">Projects unavailable: {projectError}</span>}
      </div>
    </div>
    <div className="launch-composer-actions">
      {status && <span className={status.kind}>{status.text}</span>}
      <button type="button" disabled={busy || !name.trim() || !provider.trim()} onClick={() => run("Save")}>{busy ? "Working…" : "Save"}</button>
      <button type="button" disabled={busy || !profile.trim()} onClick={() => run("Launch")}>Launch</button>
      <button className="primary" type="submit" disabled={busy || !name.trim() || !provider.trim() || !profile.trim()}>Save + Launch</button>
    </div>
  </form>;
}
