# Agent Launcher

A macOS menu-bar app that edits an agent-setup bundle and launches CLI coding
agents (Claude Code, Codex) from it, through Cairn, into a terminal.

> **Pre-release.** This project is unreleased, not deployed, and has no outside consumers. It's being built in the open: the code, the docs, and this README describe what exists today, not a pitch for what's planned. Interfaces and behavior change without notice, and there are no compatibility guarantees yet.

Agent Launcher is an early-stage convenience tool. It was built for one
person's workflow on one machine, and it is likely to change substantially or
be folded into another app. Expect rough edges.

## What it is

A single desktop app built with [Wails v3](https://v3.wails.io/) (Go core,
React frontend), living behind one tray icon. It has two windows:

- **The palette** — summoned by a global hotkey, frameless and always on top.
  Pick an agent profile, a launch profile and a project, press Enter, and a
  terminal opens with the agent running. It disappears on Escape or when it
  loses focus.
- **The manager** — an ordinary window with a file tree and text editor over
  the bundle: create and edit profiles, prompts, skills and other bundle
  files, author launch profiles, manage project folders, preview which skills
  a launch would carry, and clean up old boot directories.

A launch is made of three things:

| | What it answers | Where it lives |
|---|---|---|
| **Agent profile** | what the agent is | the bundle |
| **Launch profile** | how it runs, including which harness (Claude Code or Codex) | `~/.config/tachyon/launch/` |
| **Project** | which folder it works in | the app's project list |

The launcher hands those to `cairn boot`. Cairn builds a boot directory and
reports on it as JSON. The launcher then opens an iTerm2 session running the
harness from that directory. Claude Code launches always pass
`--settings <bootdir>/.claude/settings.json`. Codex launches run with
`CODEX_HOME` set to the boot directory and the project granted through
`--add-dir`.

### What it does not do

- It never runs `cairn install` or modifies your live `~/.claude`.
- It never runs git. It writes files into the bundle, and you commit them.
- It holds no session state: there is no session list, attach or resume.
- It does not validate bundle content beyond shape and existence.

## How it fits

```
agent-setup bundle  ->  Cairn  ->  boot directory  ->  Agent Launcher opens the agent
  (content: profiles,    (resolves a profile,          (iTerm2 session running
   prompts, skills,       assembles a directory,        Claude Code or Codex
   hooks)                 prints its path)              from that directory)
```

- **The bundle** holds the content: a directory of profiles, prompts, skills
  and hooks in the layout Cairn reads (the author's is called `agent-setup`).
  The manager edits it in place.
- **[Cairn](https://github.com/hollis-labs/cairn)** materializes a profile
  from the bundle into a boot directory.
- **Agent Launcher** is where a person edits that content and starts an agent
  from it.

## Requirements

- macOS 12 or newer. The tray, hotkey and terminal launch are macOS-only.
- Go 1.26.3 or newer.
- Node.js and npm, needed only to rebuild the frontend or package the app.
- Xcode command-line tools, needed for cgo and for code signing.
- [Cairn](https://github.com/hollis-labs/cairn) on your `PATH`.
- [iTerm2](https://iterm2.com/), used to open launched sessions.
- An agent bundle. The default is `~/dev/projects/agent-setup`, and you can
  choose another from the manager.
- The harness CLI you launch (`claude` or `codex`) on your `PATH`.

## Build and run

The built frontend (`frontend/dist`) is committed, so the Go build works
without npm:

```sh
go build ./...
go test ./...
```

For development, run the app straight from source:

```sh
npm --prefix frontend ci
npm --prefix frontend run build
go run .
```

A process started from a terminal inherits that terminal's macOS privacy
grants. That makes `go run .` a development run, not a test of the packaged
app.

### Packaging a macOS app

```sh
./scripts/build-macos.sh
```

This rebuilds the frontend, checks that every hashed asset in
`frontend/dist/index.html` exists and is tracked, builds the production
binary, and signs the result at `build/bin/Tachyon.app`. The bundle and
binary still carry the app's former name (see Naming below).

On its first run the script creates a self-signed, local-only code-signing
identity in `~/Library/Application Support/Tachyon/signing/`, stored in a
dedicated keychain that it adds to your user keychain search list. It does not
need an Apple developer account or admin access. The same identity is reused
on later builds, so macOS keeps recognizing the app and its Accessibility
permission survives a rebuild. The app is signed for this machine only. It is
not notarized and should not be distributed to other Macs.

Launch the packaged app through LaunchServices:

```sh
open build/bin/Tachyon.app
```

Grant it Accessibility once, in **System Settings → Privacy & Security →
Accessibility**, so the global hotkey works.

`./scripts/check-macos-tray.sh` is an optional acceptance check for the tray
icon. It needs Accessibility and Screen Recording access for the terminal
that runs it.

## Usage

1. Launch the app. A `⌁` mark appears in the menu bar. The app has no Dock
   icon.
2. Press the global hotkey, `Ctrl+Option+Space` by default, or left-click the
   tray mark to open the palette.
3. Choose a launch profile and a project, pick an agent profile, and press
   Enter. An iTerm2 window opens with the agent running.
4. Right-click the tray mark for **Open Manager**, **Settings…** and
   **Quit**. In the manager you choose the bundle and edit its files, and you
   manage launch profiles and projects.

A default launch profile is created on first run, so the first launch works.

### If the hotkey stops working

macOS does not report hotkey conflicts. If another app already owns the
combination, the key never arrives. Change the hotkey from the tray menu under
**Settings…**, or edit the `"hotkey"` key in `~/.config/tachyon/shell.json`
(Wails accelerator spelling, e.g. `"Ctrl+Option+Space"`) and restart the app.
If the value can't be bound, the app falls back to the default.

## Files it keeps

| Path | Contents |
|---|---|
| `~/.config/tachyon/` | your choices: bundle root, projects, shell preferences, launch profiles |
| `~/.local/state/tachyon/boot/` | boot directories, which the app regenerates, grouped as `<project>/<profile>/<launch profile>` |

Both honor `XDG_CONFIG_HOME` and `XDG_STATE_HOME`. When you relaunch a
composition, its old boot directory is moved aside rather than deleted,
because a running session may still be using it. The sweep in the manager
removes old directories that are no longer in use.

## Naming

This project was formerly called Tachyon. The Tachyon name now belongs to a
separate Hollis Labs control-plane app. The Go module path, the packaged
`Tachyon.app`, its bundle identifier and the `tachyon` config directories
still use the old name.

## License

MIT. See [LICENSE](LICENSE).
