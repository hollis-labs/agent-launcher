# Tachyon

Tachyon is the standalone, user-facing launcher app for the Tether launch substrate.

It keeps the native macOS front-end from `tether-launcher`, but the launch path now runs through a dedicated Go sidecar, `tachyon-engine`, which consumes the public shared libraries instead of Tether internals.

## Layout

```text
tachyon/
  app/        Swift/AppKit menubar launcher
  engine/     Go sidecar; consumes go-agent-launch and related public libs
  build.sh    Builds the engine and bundles the macOS app
```

## Build

```bash
./build.sh
open dist/Tachyon.app
```

The built engine is bundled into `Tachyon.app/Contents/Resources/tachyon-engine`.

## Current launch sources

Tachyon is local-first:

- The engine ships a bundled catalog corpus, so `list` / `describe` / `launch`
  work fully offline with no directory service and no `~/.tether/catalog`.
- When `~/.tether/catalog` is present it is ingested through the same
  file-backed public registry surface exposed by `go-agent-launch`.
- When native `boot-spec` contracts are present, Tachyon lists and describes
  them through the same registry path.

## Notes

- The engine is a plain consumer of `go-agent-launch v0.3.5` (and its public
  shared libraries) — pinned off published tags, no `replace` directives.
- No Tether-internal Go packages are imported.
- **Interactive launch is the only supported mode** (decision D-T2). Launch
  `Mode` is derived from the resolved runner; autonomous / headless launches
  are Torque's responsibility, not Tachyon's.
- The engine surfaces go-agent-launch's headless-claude fail-fast gate, so a
  misconfigured non-interactive claude launch is rejected, never hung.

