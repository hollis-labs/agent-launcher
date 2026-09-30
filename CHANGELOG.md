# Changelog

All notable changes to this project are documented here. The format is based
on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). The project does
not follow a versioning scheme yet because nothing has been released.

## [Unreleased]

### Added

- A macOS menu-bar app (Wails v3, Go and React) with two windows. The
  hotkey-summoned **palette** launches an agent. The **manager** edits the
  agent bundle.
- Launching through Cairn. The app runs `cairn boot --json` once per launch,
  decodes the report, prepares a stable boot directory per project, profile
  and launch profile, and opens iTerm2 running Claude Code or Codex.
- Launch profiles: Cairn parts stored in `~/.config/tachyon/launch/` that say
  how an agent runs, including its harness. A default one is created on first
  run.
- Bundle editing in the manager: a file tree and byte-for-byte text editor,
  creating new artifacts, a preview of the skills a launch would carry, a
  project list, and a guarded sweep of old boot directories.
- A configurable global hotkey, a tray menu (Open Manager, Settings, Quit) and
  a settings window.
- `scripts/build-macos.sh`, which packages and signs the app with a persistent
  local identity so macOS privacy grants survive rebuilds.
- `scripts/check-macos-tray.sh`, an acceptance check for the tray icon.
- `cmd/tachyon`, a debug CLI that resolves a composition through the same
  packages without spawning anything.
- MIT license.

### Changed

- Renamed from Tachyon to Agent Launcher. The Go module path, the packaged app
  and the config directories still use the old name.

### History

- This repository restarted from a single fresh commit. The earlier history,
  from before the fresh start, is preserved at the tag
  [`tachyon-v1-final`](https://github.com/hollis-labs/agent-launcher/tree/tachyon-v1-final).
