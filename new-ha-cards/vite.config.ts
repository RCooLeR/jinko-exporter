import { resolve } from "node:path";

export default {
  publicDir: false,
  build: {
    emptyOutDir: true,
    lib: {
      entry: resolve(__dirname, "src/main.ts"),
      formats: ["es"],
      fileName: () => "jinko-ha-cards.js"
    }
  }
};
