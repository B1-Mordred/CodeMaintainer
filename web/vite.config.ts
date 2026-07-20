import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/ui/dist",
    emptyOutDir: true,
    // Browser-facing source maps expose internal implementation details. Keep
    // them out of the production static bundle; CI retains original sources.
    sourcemap: false,
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test-setup.ts",
    restoreMocks: true,
  },
});
