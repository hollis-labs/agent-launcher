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
- `internal/launch/` glues a palette selection to boot and iTerm2, and lists
  the agent profiles that can be launched.
- `internal/launchprofile/` and `internal/launchcomposer/` own Tachyon's launch
  profiles — the files that say *how* an agent runs.
- `internal/bundle/`, `internal/manager/` and `internal/preview/` own bundle
  editing and preview.
- `internal/state/` is the one definition of where Tachyon's files live.
- `internal/shell/` owns app shell, tray and settings behavior.

## Commands

```bash
go test ./internal/compose ./internal/boot ./internal/launch
go test ./...
(cd frontend && npm test && npx vite build)
./scripts/build-macos.sh
./scripts/check-macos-tray.sh
```

Package and tray checks are required when source changes affect the packaged
app path, macOS identity, tray, hotkey or launch route.

## Three things make a launch, and each has one owner

| | Owner | Reaches Cairn as |
|---|---|---|
| **agent profile** — what this is | the agent bundle | `cairn boot <target>` |
| **launch profile** — how it runs | Tachyon, `~/.config/tachyon/launch/*.md` | `--with <path>` |
| **project** — where it works | Tachyon's own project list | `--scope <path>` |

A launch profile is an ordinary Cairn part, not a format of Tachyon's own, so
what is saved is exactly what Cairn consumes. Bindings were retired on 2026-09-10:
Cairn dropped bindings and `--save-as`.

## Boundaries

Tachyon must not run `cairn install`. Every Claude launch argv must include
`--settings`, permanently, guarded by
`TestHarnessArgv_AlwaysIncludesSettingsFlag`. The path it names is Cairn's own
reported `settings_path` when there is one, falling back to
`<bootdir>/.claude/settings.json` — the flag's presence is unconditional, the
path is not.

**No `--provider` flag, ever.** No profile in the agent bundle declares a provider,
so Cairn's own default resolves to nothing and it refuses to render rather than
writing one harness's files into another's directory. The provider is declared
in the launch profile and folded in through Cairn's ordinary cascade. A flag
here would be a second source for a value a file already carries. Guarded by
`TestProviderFlagIsNeverEmitted` and `TestPaletteCompositionContract`.

**A scope can only be a flag.** Cairn refuses `scope:` as frontmatter, so a
launch profile cannot carry one and `--scope` is its only source.

**The boot directory is one segment per axis**, in the order a person
browses them:

```
~/.local/state/tachyon/boot/cairn-8cbb5cc873/engineer/codex
                            <project>        <profile> <launch profile>
```

Cairn plants only the last two — its own layout is `<boot-root>/<target>/
<session>` — so the project is folded into what it is handed as its boot root
(`boot.ProjectRoot`, which keeps an empty boot root empty so D9's refusal
still fires). The project segment carries a short digest of the full scope
because two projects can share a basename; the other two are literal names.

Every segment must stay stable for a given composition: each directory a
harness opens leaves a permanent trust entry in `~/.claude.json` keyed to its
path, so one path per composition means one entry per composition rather than
one per launch.

`internal/boot`'s sweep walks that exact shape to find `.prev-*` siblings, and
`TestSweepFindsWhatPrepareMovedAside` plants through `Prepare` and finds it
through `Sweep` so the two cannot drift — a sweep looking at the wrong depth
finds nothing and reports the same empty result as a sweep with nothing to
do.

Codex launches are native and share the same orchestration core: no branch on
provider in `internal/launch`, only different content in Cairn's `--json`
report. The interactive shape is cwd = boot dir, `CODEX_HOME=<bootdir>`,
`codex --add-dir <scope>`. `--skip-git-repo-check` belongs to `codex exec` and
must never reach an interactive launch. The operator-owned resources Cairn
reports in `home_resource_paths` are linked from the operator's own Codex home,
never copied, and a missing one refuses the launch rather than opening a
session whose hooks silently do not run.

Storage is XDG, through `internal/state`: `~/.config/tachyon` for what a person
chose (bundle root, projects, shell prefs, launch profiles) and
`~/.local/state/tachyon` for what Tachyon regenerates (boot directories). The
split is load-bearing — one tree makes "back this up" and "delete this safely"
unanswerable.
