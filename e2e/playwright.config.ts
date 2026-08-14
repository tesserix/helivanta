import { defineConfig } from "@playwright/test";

// Assumes infra + API + shell + medicore already running (make dev, make seed).
//
// Two projects, and the split is about DATA rather than about auth.
// scripts/seed-dev.mjs already gives every spec its own login account so no
// two specs share a revocation subject — but they still share one hospital's
// rows, and since #816 a list's first page is 50 rows rather than 100.
// pagination.spec.ts deliberately creates 55 visits, each of which fans out
// to a pharmacy dispense and a lab order. Run concurrently with the others,
// that flood pushes journey.spec.ts's freshly created patient off the first
// page of the pharmacy queue, and its assertion fails for a reason that has
// nothing to do with what it is testing.
//
// So the bulk spec runs after the rest, not alongside them. Rows it leaves
// behind are harmless to a later run — every other spec asserts on a row it
// just created, which sorts newest-first onto page one regardless of how
// much history sits beneath it. It is only simultaneous creation that
// displaces another spec's row.
export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  use: { baseURL: "http://localhost:4301" },
  projects: [
    {
      name: "specs",
      testIgnore: /pagination\.spec\.ts/,
    },
    {
      name: "bulk",
      testMatch: /pagination\.spec\.ts/,
      dependencies: ["specs"],
    },
  ],
});
