import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App.jsx";
import ErrorBoundary from "./ErrorBoundary.jsx";
import "./styles.css";

// The Wails runtime is served by the Go asset server at /wails/runtime.js, so
// it does not exist at build time. The indirection through a variable plus
// @vite-ignore is what stops Vite trying to resolve and bundle it. Importing
// it sets window.wails, which src/bridge.js calls through.
const RUNTIME_URL = "/wails/runtime.js";

async function boot() {
  try {
    await import(/* @vite-ignore */ RUNTIME_URL);
  } catch (e) {
    // Rendering without the runtime is still useful: the window comes up and
    // says what is wrong instead of staying blank.
    console.error("wails runtime failed to load", e);
  }
  createRoot(document.getElementById("root")).render(
    <StrictMode>
      <ErrorBoundary>
        <App />
      </ErrorBoundary>
    </StrictMode>,
  );
}

boot();
