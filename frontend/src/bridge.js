// Thin wrapper over the Wails call bridge.
//
// The wails3 CLI normally generates typed bindings into frontend/bindings. This
// project does not run that generator (there is no wails3 CLI in the toolchain
// here), so calls go through Call.ByName with the fully-qualified method name
// that pkg/application/bindings.go builds: "<package path>.<Type>.<Method>".
const SERVICE = "github.com/hollis-labs/tachyon/internal/shell.Service";

export function call(method, ...args) {
  const wails = globalThis.wails;
  if (!wails || !wails.Call) {
    return Promise.reject(new Error("wails runtime not loaded"));
  }
  return wails.Call.ByName(`${SERVICE}.${method}`, ...args);
}

export const Shell = {
  Settings: () => call("Settings"),
  SetHotkey: (accelerator) => call("SetHotkey", accelerator),
  ValidateHotkey: (accelerator) => call("ValidateHotkey", accelerator),
  OpenManager: () => call("OpenManager"),
  HidePalette: () => call("HidePalette"),
};
