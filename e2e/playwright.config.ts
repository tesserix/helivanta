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
//
// ratelimit.spec.ts (#689) joins bulk for the same reason under a different
// shared resource: it deliberately floods POST /v1/iam/me/tenant until the
// limiter refuses. The principal bucket it drains is keyed by its own
// subject and route, but the TENANT bucket is shared by every spec running
// against the same hospital, so a flood running alongside the others could
// refuse a request they depend on and fail them for a reason that has
// nothing to do with what they test.
//
// One regex, referenced twice, so the include and the exclude cannot drift:
// a spec listed in bulk's testMatch but absent from specs' testIgnore runs
// in BOTH projects — twice, once of them concurrently with everything else,
// which is precisely the arrangement both comments above exist to prevent.
const BULK_SPECS = /(pagination|ratelimit)\.spec\.ts/;

export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  use: { baseURL: "http://localhost:4301" },
  projects: [
    {
      name: "specs",
      testIgnore: BULK_SPECS,
    },
    {
      name: "bulk",
      testMatch: BULK_SPECS,
      dependencies: ["specs"],
    },
  ],
});
