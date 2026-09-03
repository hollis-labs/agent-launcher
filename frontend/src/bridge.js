// Thin wrapper over the Wails call bridge.
//
// The wails3 CLI normally generates typed bindings into frontend/bindings. This
// project does not run that generator (there is no wails3 CLI in the toolchain
// here), so calls go through Call.ByName with the fully-qualified method name
// that pkg/application/bindings.go builds: "<package path>.<Type>.<Method>".
const SHELL_SERVICE = "github.com/hollis-labs/tachyon/internal/shell.Service";
const MANAGER_SERVICE = "github.com/hollis-labs/tachyon/internal/manager.Service";
const BINDING_SERVICE = "github.com/hollis-labs/tachyon/internal/binding.Service";

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
};

// Manager is internal/manager.Service: the bundle tree and the byte-exact
// open/save the editor uses. content, where it appears below, is the base64
// string Go's encoding/json produces for a []byte field/argument — see
// src/bytes.js for the encode/decode helpers and internal/manager's package
// doc for why base64 rather than a plain string.
export const Manager = {
  Tree: () => callService(MANAGER_SERVICE, "Tree"),
  Open: (kind, id) => callService(MANAGER_SERVICE, "Open", kind, id),
  Save: (kind, id, contentBase64) => callService(MANAGER_SERVICE, "Save", kind, id, contentBase64),
  Root: () => callService(MANAGER_SERVICE, "Root"),
};

// Binding is internal/binding.Service: the bundle's bindings.yaml, read and
// written through internal/binding.Store — see that package's doc for why a
// binding's scope is always a path here, never one of bindings.yaml's own
// scopes: alias names. Create and Update take a binding shaped
// { name, profile, scope }.
export const Binding = {
  List: () => callService(BINDING_SERVICE, "List"),
  Create: (b) => callService(BINDING_SERVICE, "Create", b),
  Update: (b) => callService(BINDING_SERVICE, "Update", b),
  Delete: (name) => callService(BINDING_SERVICE, "Delete", name),
};
