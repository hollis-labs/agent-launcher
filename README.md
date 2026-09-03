# Tachyon

Tachyon is the **user-facing surface of the agent system**. It does two things:
it edits the system's files, and it starts a session from them.

## The seam

Three responsibilities, three owners. The whole design rests on this.

| | Owner | What |
|---|---|---|
| **Content** | `agent-setup` (the bundle) | profiles, templates, role prose, skills, bindings, hooks |
| **Materialization** | Cairn | resolve a profile, assemble a directory, print its path, exit |
| **Authoring + launching** | **Tachyon** | edit the bundle; compose; build argv; spawn a terminal |

Cairn does not own content — it is a materializer pointed at a bundle, and
`agent-setup` is one bundle it can be pointed at. Tachyon is not a Cairn
front-end; it is an editor of the agent system that invokes Cairn to render it.

**Tether is not in this picture.** Tether launches session agents; Tachyon is
100% user-facing. A human presses a hotkey and a terminal opens. That is why
Tachyon has no session model, no daemon dependency and no headless path.

## What it is

A single Wails v3 + React desktop app: two window classes over one shared Go
core, behind one tray icon.

- **The palette** — hotkey-summoned, frameless, always-on-top, dismissed on
  Escape and on blur. Pick a binding or compose one; launch; vanish. This is the
  fast path, and it is what Tachyon is for.
- **The manager** — an ordinary window that does *not* dismiss on blur. A file
  tree over the bundle and a text editor. This is the CRUD.

The postures are mutually exclusive — an editor that vanishes when you click
away is unusable, and a palette that lingers is not a palette — so the split is
structural, not cosmetic. The palette can open the manager; the manager never
becomes the palette.

Nothing is a sidecar. The Go core is bound into the app directly, so there is
no subprocess on the interactive path at all — a subprocess per interaction
was the measured cause of browsing lag, and removing it is the point. The
launch path is a different thing and it does spawn: **Cairn**, once per
launch, and then **the terminal itself** (iTerm2 via AppleScript).

## What it does not do

Stated so nobody designs around a promise that is not there.

- **It never runs `cairn install`.** That rewrites the live `~/.claude` of the
  machine it runs on; it is human-executed, permanently. (`cairn install
  --check` is safe and may be surfaced read-only.)
- **It never runs git.** It writes files into a git repo; the user commits.
- **It holds no session state.** No list, no attach, no resume.
- **It never writes to a Cairn store.** The catalog is the bundle.
- **It does not validate content.** Shape and existence only.

## Status

Being rebuilt, and currently the cleared ground for that rebuild. The Swift
menubar app, the Go sidecar behind it, its bundled catalog corpus, the
frozen `list`/`describe`/`launch` contract and the `go-agent-launch` dependency
have all been deleted. Nothing here launches anything yet.

- Plan: `CW-20260518-0061`.
- Target architecture (decisions D1–D10):
  `~/dev/agent-os/workspaces/drafts/tachyon/session-20260902-0ce7995c/target-architecture.md`
