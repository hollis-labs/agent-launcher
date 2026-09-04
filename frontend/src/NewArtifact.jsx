import { useEffect, useState } from "react";
import { Manager as ManagerAPI } from "./bridge.js";

// The "new artifact" entry point: CW-20260903-0010. A self-contained panel,
// deliberately its own file rather than folded into Manager.jsx so creation
// state remains independent from the tree/editor state that renders it.
//
// KIND_META here is a small, deliberately duplicated subset of
// Manager.jsx's own KIND_META (profile/role-prose/template/prompt/skill/
// binding only — this form never offers "hook", which is out of this
// task's scope). Importing Manager.jsx's copy would create a circular
// module dependency (Manager.jsx renders this component), so the small
// label/color table is repeated here instead. "part" is a creation intent,
// not a bundle kind; NewPart returns ordinary profile content.
const KIND_META = {
  profile: { label: "Profile", color: "#45c7b8" },
  part: { label: "Part", color: "#45c7b8" },
  "role-prose": { label: "Role prose", color: "#c78ee0" },
  template: { label: "Template", color: "#e0b04b" },
  prompt: { label: "Prompt", color: "#e0708a" },
  skill: { label: "Skill", color: "#7fb0e0" },
  binding: { label: "Binding", color: "#8fce7a" },
};

// CREATION_ORDER is every creation intent this form offers. "part" sits next
// to "profile", but support for it is derived from profile support rather
// than adding a synthetic value to bundle.Kind or NewArtifactKinds().
const CREATION_ORDER = ["profile", "part", "role-prose", "template", "prompt", "skill", "binding"];

// ID_PATTERN mirrors internal/skeleton's idPattern exactly, for immediate
// feedback. The server is the actual authority — this is UX only, and a
// mismatch here would just mean a slower round trip to the same answer, not
// a security or correctness gap.
const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;

// FIELD_SHAPE says which optional fields each kind's scaffold actually uses
// (internal/skeleton/scaffolds.go): profile uses both name and description;
// role prose uses name only (as its heading); skill uses description only
// (its frontmatter "name" is always the id, matching every real skill); a
// template ignores both. Hiding fields a scaffold ignores keeps the form
// from implying a knob that does nothing. prompt uses name only, the same
// way role prose does — promptScaffold's heading is "# " + displayName —
// and no description, for the same reason: a prompt has no frontmatter to
// put one in.
const FIELD_SHAPE = {
  profile: { name: true, description: true },
  part: { name: false, description: false },
  "role-prose": { name: true, description: false },
  template: { name: false, description: false },
  prompt: { name: true, description: false },
  skill: { name: false, description: true },
  binding: { name: false, description: false },
};

export default function NewArtifact({ onCreated, onCancel }) {
  const [supported, setSupported] = useState(null); // null while loading; else bundle.Kind[]
  const [kind, setKind] = useState(null);
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState(null);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    ManagerAPI.NewArtifactKinds()
      .then((kinds) => {
        setSupported(kinds ?? []);
        if (kinds && kinds.length > 0) setKind(kinds[0]);
      })
      .catch((e) => setError(String(e?.message ?? e)));
  }, []);

  const isSupported = (k) => Array.isArray(supported) && supported.includes(k === "part" ? "profile" : k);
  const idValid = ID_PATTERN.test(id);
  const canCreate = kind != null && isSupported(kind) && idValid && !creating;
  const shape = kind ? (FIELD_SHAPE[kind] ?? {}) : {};

  const create = () => {
    if (!canCreate) return;
    setCreating(true);
    setError(null);
    const request = kind === "part"
      ? ManagerAPI.NewPart(id.trim())
      : ManagerAPI.NewArtifact(kind, id.trim(), name.trim(), description.trim());
    request
      .then((content) => {
        onCreated?.(content);
      })
      .catch((e) => setError(String(e?.message ?? e)))
      .finally(() => setCreating(false));
  };

  return (
    <div className="new-artifact-panel">
      <div className="new-artifact-header">
        <span>New artifact</span>
        <button onClick={onCancel} title="Cancel">
          ✕
        </button>
      </div>

      <div className="new-artifact-kinds">
        {CREATION_ORDER.map((k) => {
          const meta = KIND_META[k];
          const enabled = supported == null ? false : isSupported(k);
          const title = enabled
            ? meta.label
            : supported == null
              ? "Loading…"
              : "Creation is not available for this artifact type";
          return (
            <button
              key={k}
              className={`new-artifact-kind${kind === k ? " active" : ""}`}
              style={{ borderColor: enabled ? meta.color : undefined, color: enabled ? meta.color : undefined }}
              disabled={!enabled}
              title={title}
              onClick={() => setKind(k)}
            >
              {meta.label}
            </button>
          );
        })}
      </div>

      <div className="new-artifact-fields">
        <label>
          ID (filename)
          <input
            autoFocus
            value={id}
            spellCheck={false}
            placeholder="e.g. specialist"
            onChange={(e) => setId(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && create()}
          />
        </label>
        {id !== "" && !idValid && (
          <p className="warn new-artifact-hint">
            Letters, digits, "-" and "_" only, starting with a letter or digit.
          </p>
        )}

        {shape.name && (
          <label>
            Name
            <input
              value={name}
              spellCheck={false}
              placeholder="(defaults to the ID, title-cased)"
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && create()}
            />
          </label>
        )}

        {shape.description && (
          <label>
            Description
            <input
              value={description}
              spellCheck={false}
              onChange={(e) => setDescription(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && create()}
            />
          </label>
        )}
      </div>

      {error && <p className="err new-artifact-hint">{error}</p>}

      <div className="new-artifact-actions">
        <button onClick={create} disabled={!canCreate}>
          {creating ? "Creating…" : "Create"}
        </button>
        <button onClick={onCancel}>Cancel</button>
      </div>
    </div>
  );
}
