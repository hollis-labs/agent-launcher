// Thin wrapper over the Wails call bridge.
//
// The wails3 CLI normally generates typed bindings into frontend/bindings. This
// project does not run that generator (there is no wails3 CLI in the toolchain
// here), so calls go through Call.ByName with the fully-qualified method name
// that pkg/application/bindings.go builds: "<package path>.<Type>.<Method>".
const SHELL_SERVICE = "github.com/hollis-labs/tachyon/internal/shell.Service";
const MANAGER_SERVICE = "github.com/hollis-labs/tachyon/internal/manager.Service";
const BINDING_SERVICE = "github.com/hollis-labs/tachyon/internal/binding.Service";
const LAUNCH_SERVICE = "github.com/hollis-labs/tachyon/internal/launch.Service";

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
};

// Manager is internal/manager.Service: the bundle tree and the byte-exact
// open/save the editor uses. content, where it appears below, is the base64
// string Go's encoding/json produces for a []byte field/argument — see
// src/bytes.js for the encode/decode helpers and internal/manager's package
// doc for why base64 rather than a plain string.
// Tree()'s resolved value now also carries a State field (CW-20260904-0019),
// "ok" | "unrecognized" -- see internal/manager.Service's Tree type doc.
// "unrecognized" means the root exists and is readable but has none of the
// five known artifact directories under it: a directory that was never a
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
};

// Binding is internal/binding.Service: the bundle's bindings/ directory
// (one file per binding), read and written through internal/binding.Store —
// see that package's doc for why a binding's scope is always a path here,
// never one of scopes.yaml's own alias names. Create and Update take a
// binding shaped { name, profile, scope }.
//
// List()'s resolved value is a [ListResult]-shaped object — see
// internal/binding.Service.List's own doc — not a bare array:
// { bindings, state, path, detail }. state is one of "ok" | "missing" |
// "unreadable"; bindings is only meaningful when state === "ok". This is
// what lets Palette.jsx tell "genuinely no bindings yet" apart from "the
// bundle root looks wrong" and "bindings/ exists but can't be read" — see
// that component's own comment.
export const Binding = {
  List: () => callService(BINDING_SERVICE, "List"),
  Create: (b) => callService(BINDING_SERVICE, "Create", b),
  Update: (b) => callService(BINDING_SERVICE, "Update", b),
  Delete: (name) => callService(BINDING_SERVICE, "Delete", name),
};

// Launch is internal/launch.Service: the palette's one entry point for
// turning a picked binding into a running terminal (CW-20260903-0016).
// Binding(name) resolves the named binding, runs `cairn boot` for it, and
// spawns iTerm2 on the result -- fire-and-forget, no session handle. The
// returned promise rejects with whatever internal/launch.Service.Launch
// returned as an error; there is no success payload beyond the promise
// resolving.
export const Launch = {
  Binding: (name) => callService(LAUNCH_SERVICE, "Launch", name),
};
