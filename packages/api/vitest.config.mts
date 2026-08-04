import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { hmsVitest } from "@hms/config/vitest";

export default defineConfig({ plugins: [react()], ...hmsVitest() });
