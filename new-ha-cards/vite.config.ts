import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";

export default defineConfig({
  publicDir: false,
  server: {
    forwardConsole: true
  },
  build: {
    emptyOutDir: true,
    target: "baseline-widely-available",
    lib: {
      entry: fileURLToPath(new URL("./src/main.ts", import.meta.url)),
      formats: ["es"],
      fileName: () => "jinko-ha-cards.js"
    }
  }
});
