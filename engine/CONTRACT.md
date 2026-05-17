# tachyon-engine — CLI contract

`tachyon-engine` is the Go sidecar of Tachyon and the sole public-seam
consumer of [`go-agent-launch`](https://github.com/hollis-labs/go-agent-launch)
(pinned at the published tag `v0.3.5`; no `replace` directive).
The Swift/AppKit app shells out to this binary and parses the JSON it
emits on **stdout**. Human-readable diagnostics go to **stderr** only.

This document is the frozen engine⇄app seam (S5.2, EP-20260516-0001).

## Interactive-only (D-T2)

Tachyon ships **interactive launches only**. Every runner the engine
advertises is an interactive runner: `claude-code`, `claude-pty`,
`claude-tui`, `codex-cli`, `codex-app-server`, `opencode`. There is
deliberately **no** `claude-stream` / streaming-stdio runner — Torque
owns autonomous/headless launches, and the non-interactive claude path
that could hang on an approval prompt is not reachable from Tachyon.

The launch `Mode` is **derived** from the resolved runner (every
interactive runner → `LaunchInteractive`), never hardcoded. The
go-agent-launch v0.3.x fail-fast gate (`ErrHeadlessClaudeNeedsPermission`)
still backs this: a background/ephemeral claude launch with no permission
posture is rejected at `Compile` — Tachyon surfaces that gate even though
it ships no such bag.

## Conventions

- JSON results are written to **stdout** (pretty-printed, 2-space indent).
- Diagnostics, warnings, and errors are written to **stderr**.
- Exit codes: `0` = success, `2` = `launch` var_error, `1` = fatal.
- All subcommands are **local-first** (D1): the launch corpus is embedded
  in the binary, so the engine works fully offline with no directory
  service and no on-disk catalog.
- `--catalog-root <path>` is accepted by every subcommand for
  forward-compatibility. The engine has no directory-service client
  today; an absent or unreachable root logs a fallback notice to stderr
  and the bundled corpus is used. Default: `~/.tether/catalog`.

## `tachyon-engine list [--facet KEY=VAL ...]`

Lists the launchable specs from the local-first corpus. `--facet` is
repeatable; recognized keys: `project`, `role`, `runner`. A spec must
match every supplied facet.

stdout:

```json
{
  "specs": [
    {
      "id": "tether-claude",
      "name": "Tether — Backend",
      "project": "tether",
      "role": "backend",
      "summary": "Tether — Backend",
      "facets": { "project": "tether", "role": "backend", "runner": "claude-code" }
    }
  ]
}
```

Exit: `0` on success, `1` on a fatal error.

## `tachyon-engine describe --spec <id>`

Returns the typed input surface of a spec (so the app can render a form)
plus the available runners. `--spec` is a **required flag** — not a
positional argument.

stdout:

```json
{
  "id": "tether-claude",
  "name": "Tether — Backend",
  "inputs": [
    { "name": "work_dir", "type": "string", "required": true,
      "default": "~/dev/hollis-labs/apps/tether",
      "description": "Agent working directory ..." }
  ],
  "runners": ["claude-code","claude-pty","claude-tui","codex-app-server","codex-cli","opencode"]
}
```

Each input row is `name / type / required / default / description`.
Exit: `0` on success, `1` on a fatal error (including unknown `--spec`).

## `tachyon-engine launch --spec <id> [--input KEY=VAL ...] [--on-error-choice retry|proceed_cached]`

Runs the go-agent-launch pipeline —
`agentlaunch.PlanFromLaunch → launcher.Compile → providerplant.PrepareAndPlant → sessionshim.ToSessionLaunch` —
and **emits** the runnable command. It does **not** exec the agent; the
Swift app spawns it in iTerm2.

The `LaunchPlan` is assembled by the supported `agentlaunch.PlanFromLaunch`
bridge — the engine no longer hand-builds a `LaunchPlan` struct literal.
The bridge carries `RuntimeBinding.Permission` onto
`LaunchPlan.Provider.Permission` automatically; the engine never
hand-threads permission.

- `--spec` — required, the spec id from `list`.
- `--input KEY=VAL` — repeatable; overrides a declared input collected via
  the `describe` form. An undeclared key is a fatal error.
- `--on-error-choice` — app-mediated decision on a var_error re-invocation:
  `retry` re-renders, `proceed_cached` proceeds with the degraded
  (empty-var) render. Omitted on the first call.

stdout on success:

```json
{
  "status": "ready",
  "binary": "claude",
  "args": ["--add-dir","/Users/.../tether"],
  "env": {},
  "workdir": "/var/folders/.../agentlaunch-bootdir-..."
}
```

`env` is a JSON **object** (map), not an array.

stdout on a var error:

```json
{
  "status": "var_error",
  "var": "work_dir",
  "message": "inputs.work_dir is unresolved; supply it via --input or proceed with cached values",
  "options": ["retry","proceed_cached","cancel"]
}
```

Exit codes:

- `0` — `status:ready`. The app opens iTerm2 running `binary args` with
  `env` in `workdir`.
- `2` — `status:var_error`. The app shows a retry / proceed-with-cached /
  cancel dialog, then re-invokes `launch` with `--on-error-choice`.
- `1` — fatal (unknown spec, undeclared `--input` key, compile/plant
  failure, ...).

## Architecture notes

- **No Tether-internal imports.** The engine depends only on
  `go-agent-launch` (published tag `v0.3.5`, no `replace` directive) and
  the Go stdlib.
- **Local-first corpus.** `internal/corpus/` embeds the S4.4 LaunchSpec,
  the launch-bag corpus, and one runtime-binding contract per runner
  token under `providers/`. `internal/launch.LoadCatalog` materializes it
  to a temp catalog root and walks it with the go-agent-launch
  file-backed loaders and file-backed registrar. This is the permanent
  offline path.
- **Runner bridge.** The S4.4 `runner` input is an open token. The
  `providers/` corpus subdir ships a runtime-binding contract per token;
  the engine ingests it with the go-agent-launch file-backed registrar
  (`providers/` → runtime-binding kind) and resolves a token through
  `agentlaunch.ResolveRuntimeBinding` — the same directory-registry seam
  an online directory service exposes. There is no hand-rolled runner
  table.
