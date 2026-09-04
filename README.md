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

### If the hotkey stops working

macOS does not report a global-hotkey conflict. Another application can already
own the combination, and Tachyon's registration still succeeds — the key simply
never arrives. So the hotkey is configurable at runtime, and there are two ways
to change it:

- **The tray icon** — right-click it and choose *Settings…*. This is the
  intended route.
- **By hand** — edit `~/Library/Application Support/Tachyon/shell.json` and
  change the top-level `"hotkey"` key (Wails accelerator spelling, e.g.
  `"Ctrl+Option+Space"`), then restart Tachyon. A value that cannot be bound is
  replaced with the default at startup rather than leaving you with no hotkey,
  so a typo here costs a restart and nothing else.

The second route matters because Tachyon has no Dock icon and no application
menu — if the hotkey is dead *and* the status item is not drawing, the file is
the way back in.

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
- **It never stages a second copy of bundle content.** The active bundle is
  the source for editing, preview and launch; saving an edit needs no Apply or
  install step.
- **It holds no session state.** No list, no attach, no resume.
- **It never writes to a Cairn store.** The catalog is the bundle.
- **It does not validate content.** Shape and existence only.

## Status

The manager, composition, preview and launch paths work. Tachyon reads the
active bundle, including ordinary profiles in both `profiles/` and
`profiles/parts/`, shows it as a tree, and edits it as text byte-for-byte. The
manager can create bundle artifacts, edit bindings, inspect Cairn's effective
skills preview, manage project scopes and run the guarded old-boot-directory
sweep on demand. A saved edit is immediately available to the next Cairn
preview or launch because both read the same active bundle directly.

Summoning the palette can launch either a saved binding or an unsaved
composition through Cairn into an iTerm2 session. Composition drafts survive
ordinary palette dismissal until they are launched or explicitly discarded.
Boot directories have stable current paths; a relaunch moves the old directory
aside, and the guarded startup/manual sweep removes only eligible `.prev-*`
directories that are no longer in use.

The Swift menubar app, the Go sidecar behind it, its bundled catalog corpus,
the frozen `list`/`describe`/`launch` contract and the `go-agent-launch`
dependency are all gone.

## Run and build

Requirements are Go 1.26.3 or newer, Node.js/npm, the macOS command-line
developer tools and Cairn on `PATH`. iTerm2 is required when a selection is
actually launched. For source development:

```sh
npm --prefix frontend ci
npm --prefix frontend run build
go run .
```

`go run .` is useful for development, but it is not a packaged-app or macOS
privacy test: a terminal-launched process can inherit the terminal's
Accessibility grant.

Build the production macOS application with the repository's single packaging
command:

```sh
./scripts/build-macos.sh
```

It rebuilds the frontend, verifies that every hashed asset referenced by
`frontend/dist/index.html` exists and is tracked, builds the Wails production
binary, installs the checked-in icon and `Info.plist`, and signs the result at
the stable path `build/bin/Tachyon.app`. A changed frontend must therefore have
its newly generated `frontend/dist` files committed before it can be packaged.
The ordinary Go build remains npm-independent because `frontend/dist` is
committed:

```sh
go test ./...
go build ./...
npm --prefix frontend test
```

### Local signing and Accessibility identity

The first packaged build creates a Tachyon-only self-signed code-signing
identity in `~/Library/Application Support/Tachyon/signing/`. It uses a
dedicated keychain, records user-scoped code-signing trust, and restricts the
private key's noninteractive access to Apple's signing tools. No administrator
access, Apple developer account or repository-stored private key is required.
Signer creation is staged and published atomically; later builds repair a
missing trust record for that same certificate without replacing it. The
packaging command is serialized across worktrees so concurrent builds cannot
race the signing state or final app replacement.

The resulting designated requirement pins both
`com.hollislabs.tachyon` and the persistent certificate. The build prints that
requirement and refuses to replace an existing app if it changes across a
rebuild. Back up the signing directory: deleting or partially recreating it
rotates Tachyon's identity and requires granting Accessibility again. The app
is locally signed for this machine; it is not notarized or suitable for
distribution to another Mac.

Launch the package through LaunchServices, not by executing its inner binary:

```sh
open build/bin/Tachyon.app
```

With the package quit, the local tray acceptance check launches that exact app
through LaunchServices, verifies its separate Accessibility label, captures its
menu-bar pixels, toggles the palette through AXPress, exercises the real right-
click menu, exits through that exact menu's Quit action, and fails if the mark
is in macOS's visually blank overflow region:

```sh
./scripts/check-macos-tray.sh
```

The check needs Accessibility and Screen Recording access for the invoking
terminal. Use its cold-position mode to temporarily remove both the legacy and
current saved positions, verify the first-run fallback, and restore the exact
values (or absence) when the check exits:

```sh
./scripts/check-macos-tray.sh --cold-position
```

On macOS Tachyon owns one narrow AppKit status-item bridge because Wails v3
does not expose its native status item or setters for the platform-only
properties. The bridge gives the item a stable public `NSStatusItem.autosaveName`
and gives its button the public Accessibility label “Tachyon,” independently of
the visible `⌁` title. A one-time migration moves a persistent position from
Wails' old automatic `Item-0` name. AppKit exposes no public initial-position
setter, so a non-persistent registered fallback keeps a truly new item out of
the overflow measured on the target notched menu bar. A migrated or
Command-dragged named position has ordinary user-default precedence.

If a previously saved custom position is itself hidden, quit Tachyon, remove
only that placement, and relaunch; the first-run default will be registered
again:

```sh
defaults delete com.hollislabs.tachyon 'NSStatusItem Preferred Position com.hollislabs.tachyon.main-status-item'
open build/bin/Tachyon.app
```

For the required macOS verification, enable `Tachyon.app` once in **System
Settings → Privacy & Security → Accessibility**, launch it with the command
above, and observe all of the following:

1. The configured global hotkey summons the palette. An existing installation
   uses a valid value saved in Settings or `shell.json`; a fresh preferences
   file, or a missing or invalid saved value, falls back to
   `Ctrl+Option+Space`.
2. The Tachyon tray mark renders as `⌁`; VoiceOver announces it as “Tachyon”;
   left-click toggles the palette; and right-click shows Open Manager, Settings
   and Quit.
3. After quitting, rerun `./scripts/build-macos.sh` and launch the same app path
   with `open`; Accessibility remains enabled without adding the app again, and
   the hotkey and tray still work.

Those observations must be made by a person against the packaged application.
Successful signing, `go run .`, or the pixel-level tray preflight does not prove
Accessibility persistence or real hotkey/menu interaction.

### Where the authority is

**Tesseract, then code, then Chrispian.** A document in this repo describes the
moment it was written and is not binding.

- Decisions: `tachyon_vnext_target_architecture` in Tesseract
  (`user/chrispian/memory/decisions`). Fetch it by key rather than searching
  for it.
- Work and its state: `CW-20260518-0061` in Torque.
