# Tachyon

Tachyon is the user-facing editor and launcher for agent bundles. It edits the
active bundle, invokes Cairn once for a launch, decodes Cairn's JSON report and
opens iTerm2. It is not a daemon, session manager, task tracker or install
runner.

## Start Here

- `README.md` explains the app boundary, packaging and macOS acceptance path.
- `internal/compose/` builds Cairn argv.
- `internal/boot/` manages stable boot directories, invokes Cairn and builds
  harness argv.
- `internal/launch/` glues binding/composition selection to boot and iTerm2.
- `internal/binding/`, `internal/bundle/`, `internal/manager/` and
  `internal/preview/` own bundle editing and preview.
- `internal/shell/` owns app shell, tray and settings behavior.

## Commands

```bash
go test ./internal/compose ./internal/boot ./internal/launch
go test ./...
./scripts/build-macos.sh
./scripts/check-macos-tray.sh
```

Package and tray checks are required when source changes affect the packaged
app path, macOS identity, tray, hotkey or launch route.

## Boundaries

Tachyon must not run `cairn install`. Every Claude launch argv must include
`--settings <bootdir>/.claude/settings.json` — permanently, and guarded by
`TestHarnessArgv_AlwaysIncludesSettingsFlag`.

Codex launches are native and share the same orchestration core: no branch on
provider in `internal/launch`, only different content in Cairn's `--json`
report. The interactive shape is cwd = boot dir, `CODEX_HOME=<bootdir>`,
`codex --add-dir <scope>`. `--skip-git-repo-check` belongs to `codex exec` and
must never reach an interactive launch. The operator-owned resources Cairn
reports in `home_resource_paths` are linked from the operator's own Codex home,
never copied, and a missing one refuses the launch rather than opening a
session whose hooks silently do not run.

A provider is never inferred from a binding's name. It comes from the resolved
profile cascade (a part declaring `provider: codex`), or from the palette's
own Provider control, which renders `--provider`.
