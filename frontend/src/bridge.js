// Thin wrapper over the Wails call bridge.
//
// The wails3 CLI normally generates typed bindings into frontend/bindings. This
// project does not run that generator (there is no wails3 CLI in the toolchain
// here), so calls go through Call.ByName with the fully-qualified method name
// that pkg/application/bindings.go builds: "<package path>.<Type>.<Method>".
const SHELL_SERVICE = "github.com/hollis-labs/tachyon/internal/shell.Service";
const MANAGER_SERVICE = "github.com/hollis-labs/tachyon/internal/manager.Service";
const LAUNCH_PROFILE_SERVICE = "github.com/hollis-labs/tachyon/internal/launchprofile.Service";
const LAUNCH_SERVICE = "github.com/hollis-labs/tachyon/internal/launch.Service";
const LAUNCH_COMPOSER_SERVICE = "github.com/hollis-labs/tachyon/internal/launchcomposer.Service";
const COMPOSITION_PREVIEW_SERVICE = "github.com/hollis-labs/tachyon/internal/preview.Service";
const PROJECT_SERVICE = "github.com/hollis-labs/tachyon/internal/project.Service";

function callService(service, method, ...args) {
  const wails = globalThis.wails;
  if (!wails || !wails.Call) {
    return Promise.reject(new Error("wails runtime not loaded"));
  }
  return wails.Call.ByName(`${service}.${method}`, ...args);
}

export function call(method, ...args) {
  return callService(SHELL_SERVICE, method, ...args);
}

export const Shell = {
  Settings: () => call("Settings"),
  SetHotkey: (accelerator) => call("SetHotkey", accelerator),
  ValidateHotkey: (accelerator) => call("ValidateHotkey", accelerator),
  OpenManager: () => call("OpenManager"),
  HidePalette: () => call("HidePalette"),
  // SweepBootDirectories is CW-20260903-0019's manual cleanup action: it
  // runs the exact same guarded .prev-* sweep Tachyon already runs once,
  // automatically, at startup (internal/shell.Shell.wireBootSweep) --
  // never a second implementation. The resolved promise is a Report:
  // { swept: string[], skipped: {path, reason}[], guardOK: bool,
  // guardDetail: string }. guardOK false means the liveness guard itself
  // could not be trusted this run (see guardDetail) and nothing was
  // removed -- that is not the same thing as "nothing needed cleaning up".
  SweepBootDirectories: () => call("SweepBootDirectories"),
  // PickBundleRoot is CW-20260904-0019's addition: a real native macOS
  // folder picker (application.App.Dialog.OpenFile with
  // CanChooseDirectories(true), attached to the manager window -- see
  // internal/shell.Service.PickBundleRoot's own doc). Resolves to the
  // chosen absolute directory, or "" if the user dismissed the dialog
  // without picking one. It only picks -- the caller still has to hand
  // the result to Manager.SetRoot to actually change anything.
  PickBundleRoot: () => call("PickBundleRoot"),
  PickProjectPath: () => call("PickProjectPath"),
};

export const Project = {
  List: () => callService(PROJECT_SERVICE, "List"),
  Create: (project) => callService(PROJECT_SERVICE, "Create", project),
  Update: (oldName, project) => callService(PROJECT_SERVICE, "Update", oldName, project),
  Delete: (name) => callService(PROJECT_SERVICE, "Delete", name),
};

// Manager is internal/manager.Service: the bundle tree and the byte-exact
// open/save the editor uses. content, where it appears below, is the base64
// string Go's encoding/json produces for a []byte field/argument — see
// src/bytes.js for the encode/decode helpers and internal/manager's package
// doc for why base64 rather than a plain string.
// Tree()'s resolved value now also carries a State field (CW-20260904-0019),
// "ok" | "unrecognized" -- see internal/manager.Service's Tree type doc.
// "unrecognized" means the root exists and is readable but has none of the
// known artifact directories under it: a directory that was never a
// bundle (a home directory, a Desktop, a typo), not a genuinely empty one.
// A root that does not exist at all still rejects the promise, same as
// always -- State is only reached once there is a readable directory to
// describe a shape for.
export const Manager = {
  Tree: () => callService(MANAGER_SERVICE, "Tree"),
  Open: (kind, id) => callService(MANAGER_SERVICE, "Open", kind, id),
  Save: (kind, id, contentBase64) => callService(MANAGER_SERVICE, "Save", kind, id, contentBase64),
  Root: () => callService(MANAGER_SERVICE, "Root"),
  // SetRoot changes the active bundle root and persists it -- bound to
  // RootStore.Save (CW-20260904-0019). Every other call this bridge makes
  // (Tree, Open, Save, Binding.List) resolves the root fresh every time,
  // so nothing else needs to happen for the next one of those to read the
  // new bundle -- the caller (Manager.jsx) still has to actually make
  // those next calls itself, this only changes what they will return.
  SetRoot: (root) => callService(MANAGER_SERVICE, "SetRoot", root),
  // DefaultRoot is the bundle Tachyon opens when nothing has been chosen,
  // for a "reset to default" affordance that never hardcodes the default
  // path a second time here in JavaScript.
  DefaultRoot: () => callService(MANAGER_SERVICE, "DefaultRoot"),
  // NewArtifactKinds/NewArtifact are CW-20260903-0010's addition: see
  // internal/manager.Service's "new artifact entry point" section. kind is
  // one of bundle.Kind's values as a plain string, same as Open/Save above.
  NewArtifactKinds: () => callService(MANAGER_SERVICE, "NewArtifactKinds"),
  NewArtifact: (kind, id, name, description) =>
    callService(MANAGER_SERVICE, "NewArtifact", kind, id, name, description),
  // A part is a profile-placement intent, not another artifact kind. The
  // returned content still has kind === "profile" and a bare id.
  NewPart: (id) => callService(MANAGER_SERVICE, "NewPart", id),
};

// LaunchProfile is internal/launchprofile.Service: Tachyon's own store of
// launch profiles — the files that say HOW an agent runs, as distinct from
// the bundle, which says what the agent IS.
//
// Each one is an ordinary Cairn part, not a format of Tachyon's own, so what
// is saved here is exactly what `cairn boot --with <path>` consumes. They
// live under ~/.config/tachyon/launch and are NOT in the bundle: changing
// the active bundle changes what agents are available, never how they run.
//
// List()'s resolved value is a ListResult-shaped object, not a bare array:
// { profiles, state, path, detail }. state is one of "ok" | "missing" |
// "unreadable"; profiles is only meaningful when state === "ok". That is
// what lets Palette.jsx tell "you have not written one yet" apart from
// "this build cannot read the directory" — the same three-state shape the
// binding list had, and for the reason recorded there.
//
// A profile is { name, path, provider, description }. It deliberately
// carries no skills or prompts field: cairn's --skill and --prompt are
// additive only, so a control that looked pre-checked would let someone
// uncheck a skill and silently get it anyway. See Palette.jsx.
export const LaunchProfile = {
  List: () => callService(LAUNCH_PROFILE_SERVICE, "List"),
  Get: (name) => callService(LAUNCH_PROFILE_SERVICE, "Get", name),
  Read: (name) => callService(LAUNCH_PROFILE_SERVICE, "Read", name),
  Save: (name, content) => callService(LAUNCH_PROFILE_SERVICE, "Save", name, content),
  Create: (name, provider) => callService(LAUNCH_PROFILE_SERVICE, "Create", name, provider),
  Dir: () => callService(LAUNCH_PROFILE_SERVICE, "Dir"),
};

// Launch is internal/launch.Service: the palette's entry point for turning a
// selection into a running terminal. Fire-and-forget, no session handle --
// the returned promise rejects with whatever the Go error was, and there is
// no success payload beyond it resolving.
//
// input carries everything the palette can compose:
// { target, launchProfile, skills, prompts, scope, sets, parts }, matching
// internal/launch.CompositionInput field for field.
//
//   target         an agent profile id from the bundle    WHAT this is
//   launchProfile  a launch profile NAME, never a path    HOW it runs
//   scope          the selected project's path            WHERE it works
//
// launchProfile is a name because the Go side resolves it against the store,
// which validates it before joining — so nothing crossing this boundary can
// point --with at an arbitrary file. An empty one means no provider, which
// cairn refuses; that is the intended shape rather than a gap, and
// internal/launchprofile seeds a default so a first run has one.
//
// There is no provider field. It is declared in the launch profile and
// folded in by cairn's own cascade; a second source here could disagree with
// the file.
//
// skills and prompts are additive ONLY -- see Palette.jsx's own comment on
// why those arrays must always start empty and grow only from direct user
// action, never from anything a profile already resolves to.
export const Launch = {
  // Targets is the agent profiles in the active bundle that can be booted —
  // what the palette lists. Abstract profiles are excluded (cairn refuses to
  // boot one); parts are not, because cairn treats a part as an ordinary
  // bootable profile and hiding them would invent a distinction the catalog
  // does not make. Each is { id, name, description }.
  Targets: () => callService(LAUNCH_SERVICE, "Targets"),
  Composition: (input) => callService(LAUNCH_SERVICE, "LaunchComposition", input),
};

// LaunchComposer is internal/launchcomposer.Service: saving a compose-form
// selection as a launch profile, and launching it.
//
// Only the durable half is saved -- provider, skills, prompts. A target, a
// scope, a one-off --with part and a --set are facts about one launch, and
// Save's result carries a `dropped` array naming whichever of them held a
// value and was not written, so the UI can say so rather than losing them
// silently.
export const LaunchComposer = {
  Save: (input) => callService(LAUNCH_COMPOSER_SERVICE, "Save", input),
  Launch: (input) => callService(LAUNCH_COMPOSER_SERVICE, "Launch", input),
  SaveAndLaunch: (input) => callService(LAUNCH_COMPOSER_SERVICE, "SaveAndLaunch", input),
};

// The raw Wails promise is intentionally returned unchanged. Wails adds a
// cancel() method that cancels the context accepted by preview.Service;
// wrapping this call in an async function would discard that capability.
export const CompositionPreview = {
  Preview: (input) => callService(COMPOSITION_PREVIEW_SERVICE, "Preview", input),
};
