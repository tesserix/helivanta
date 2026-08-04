import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { hmsVitest } from "@hms/config/vitest";

const base = hmsVitest();

// @tesserix/web ships extensionless directory re-exports in its .mjs bundle
// (e.g. `export * from "./components/accordion"`), which Node's native ESM
// resolver rejects when Vitest externalizes the package. Inlining it forces
// Vite's own resolver (which tolerates extensionless/directory specifiers)
// to handle the import instead. This mocks nothing about the components
// themselves — only how the test runner loads the module.
export default defineConfig({
  plugins: [react()],
  ...base,
  test: {
    ...base.test,
    server: {
      ...base.test?.server,
      deps: {
        ...base.test?.server?.deps,
        inline: [...(base.test?.server?.deps?.inline ?? []), "@tesserix/web"],
      },
    },
  },
});
