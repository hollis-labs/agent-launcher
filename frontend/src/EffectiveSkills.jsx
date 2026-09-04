import { useEffect, useRef, useState } from "react";
import { CompositionPreview } from "./bridge.js";
import { createEffectiveSkillsController } from "./effectiveSkills.js";

const initialState = { status: "idle", skills: [], advisory: "", error: "" };

// EffectiveSkills is the one read-only presentation shared by both
// composition surfaces. It displays only Cairn's ordered answer; it never
// merges that answer with the additive picker or interprets contributors.
export default function EffectiveSkills({ input }) {
  const [state, setState] = useState(initialState);
  const latestInput = useRef(input);
  latestInput.current = input;
  const controller = useRef(null);
  if (controller.current === null) {
    controller.current = createEffectiveSkillsController({
      preview: CompositionPreview.Preview,
      onState: setState,
    });
  }

  const inputKey = JSON.stringify(input);
  useEffect(() => {
    controller.current.update(latestInput.current);
    return () => controller.current.cancel();
  }, [inputKey]);

  useEffect(() => {
    const pause = () => controller.current.cancel();
    const resume = () => controller.current.update(latestInput.current);
    window.addEventListener("blur", pause);
    window.addEventListener("focus", resume);
    return () => {
      window.removeEventListener("blur", pause);
      window.removeEventListener("focus", resume);
      controller.current.dispose();
    };
  }, []);

  return (
    <aside className="effective-skills" aria-live="polite">
      <div className="effective-skills-heading">
        Effective skills <span>read-only · resolved by Cairn</span>
      </div>
      {state.status === "idle" ? (
        <div className="effective-skills-empty muted">Choose a target to preview.</div>
      ) : state.status === "loading" ? (
        <div className="effective-skills-empty muted">Resolving…</div>
      ) : state.status === "error" ? (
        <div className="effective-skills-error warn">
          Preview unavailable: {state.error} Launch remains available.
        </div>
      ) : state.skills.length === 0 ? (
        <div className="effective-skills-empty muted">No effective skills.</div>
      ) : (
        <div className="effective-skills-values">
          {state.skills.map((skill, index) => <code key={`${skill}-${index}`}>{skill}</code>)}
        </div>
      )}
      {state.advisory ? (
        <div className="effective-skills-advisory warn">Cairn advisory: {state.advisory}</div>
      ) : null}
    </aside>
  );
}
