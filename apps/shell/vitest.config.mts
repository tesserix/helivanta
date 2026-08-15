import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { resolve } from "path";
import { hmsVitest } from "@hms/config/vitest";

const config = hmsVitest();

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "./"),
    },
  },
  ...config,
  test: {
    ...config.test,
    // lib/env.ts (apps/shell/lib/oidc.ts's dependency) refuses to
    // construct without both — unlike the old GIP config, there is no
    // built-in "demo-key"-style dev fallback (see env.ts's doc comment:
    // a wrong Zitadel value fails real OIDC calls, not a silent no-op).
    // Every test in this suite mocks "@/lib/oidc" directly rather than
    // exercising real network calls, so these only need to be
    // valid-shaped, never reachable.
    env: {
      NEXT_PUBLIC_ZITADEL_ISSUER_URL: "http://localhost:20080",
      NEXT_PUBLIC_ZITADEL_CLIENT_ID: "test-client-id",
    },
  },
});
