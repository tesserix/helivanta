import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { hmsVitest } from "@hms/config/vitest";

const base = hmsVitest();

export default defineConfig({
  plugins: [react()],
  ...base,
});
