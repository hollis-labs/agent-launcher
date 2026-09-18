import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The shell serves `dist/` from a Go embed.FS, so every asset reference has to
// be relative — the webview loads index.html from the asset server root and
// there is no dev server in a packaged build.
export default defineConfig({
  base: "./",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
