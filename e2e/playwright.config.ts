import { defineConfig } from "@playwright/test";

// Assumes infra + API + shell + medicore already running (make dev, make seed).
// The "idle-timeout" project additionally assumes
// `make dev-api-idle-timeout` and `make dev-web-idle-timeout` are running —
// see that project's own comment below.
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

// idle-timeout.spec.ts (#848 Task 8) needs a SHORT IDLE_TIMEOUT to prove a
// session actually goes idle without sitting for the real 15-minute
// default — and every OTHER spec, and production, needs that default left
// alone. So it runs against its own API + shell pair (Makefile's
// dev-api-idle-timeout / dev-web-idle-timeout, see the Makefile's "Idle
// timeout e2e fixture" comment for the exact ports and IDLE_TIMEOUT
// value) rather than the shared instance the "specs"/"bulk" projects
// assume is already up. A dedicated PROJECT — not just a dedicated test
// file — is what lets it point `baseURL` at that second shell instance
// without touching the other projects' own base URL.
//
// That fixture cannot run ALONGSIDE the main stack, though: both it and
// "specs"/"bulk" serve apps/shell via `next dev`, and Next.js 16 refuses a
// second dev server for the same project directory outright ("Another
// next dev server is already running" — reproduced live running this
// suite). A bare `playwright test` therefore has to leave "idle-timeout"
// out of its run so the command stays honest about what it actually
// covers, while `--project=idle-timeout` still has to work for anyone (or
// anything — see scripts/e2e.sh) that wants to run it deliberately against
// the fixture. Playwright has no first-class "exclude this project from
// the default run" switch, so PROJECT_FLAG_GIVEN below inspects argv for
// an explicit `--project`; only a project-less invocation gets the
// grepInvert filter that drops idle-timeout.spec.ts. `make e2e` is the
// command that actually runs the whole suite: "specs"+"bulk" against the
// main stack, then a swap to the fixture (Makefile's dev-api-idle-timeout
// / dev-web-idle-timeout) for `--project=idle-timeout`, then a swap back —
// see scripts/e2e.sh.
const IDLE_TIMEOUT_SPEC = /idle-timeout\.spec\.ts/;

// session-renewal.spec.ts (#916 Task 4) needs a SHORT SESSION_TTL to prove
// a session outlives its own TTL because a renewal actually re-minted it —
// and, like idle-timeout, every OTHER spec and production need that
// default left alone. So it too runs against its own API + shell pair
// (Makefile's dev-api-renewal / dev-web-renewal, see the Makefile's
// "Session-renewal e2e fixture" comment) and gets its own PROJECT, for the
// same reason and with the same Next.js "one dev server per project
// directory" constraint: it cannot run alongside the main stack's shell
// either, so scripts/e2e.sh runs it as a third phase and the default
// project-less run filters it out below.
const RENEWAL_SPEC = /session-renewal\.spec\.ts/;

// The app's host. NOT localhost (#916 Task 4, design spec D6): the whole
// point of this harness change is that the app and the IdP
// (auth.tesserix.localhost, HELIVANTA_ZITADEL_HOST) sit on DIFFERENT
// registrable domains, so browser requests between them are cross-site
// exactly as they are in production (helivanta.app vs auth.tesserix.app).
// A "site" is scheme + registrable domain and ports are not part of it, so
// the old localhost:4301 / localhost:20080 pair was same-site and could
// never have exercised #916. cross-site-harness.spec.ts proves the
// property this constant exists for rather than assuming it.
const WEB_HOST = "helivanta.localhost"; // Makefile's HELIVANTA_WEB_HOST
const BASE_URL = `http://${WEB_HOST}:4301`;
const IDLE_TIMEOUT_BASE_URL = `http://${WEB_HOST}:4399`; // Makefile's HELIVANTA_IDLE_WEB_PORT
const RENEWAL_BASE_URL = `http://${WEB_HOST}:4398`; // Makefile's HELIVANTA_RENEWAL_WEB_PORT
const PROJECT_FLAG_GIVEN = process.argv.some(
  (arg) => arg === "--project" || arg.startsWith("--project="),
);

export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  use: { baseURL: BASE_URL },
  // Only a project-less run (bare `playwright test`) gets filtered — an
  // explicit `--project=idle-timeout` (or `=specs`/`=bulk`) is already
  // scoped by Playwright's own project-name matching, and stacking
  // grepInvert on top of that would filter idle-timeout.spec.ts OUT of
  // the very project someone just asked for by name.
  grepInvert: PROJECT_FLAG_GIVEN
    ? undefined
    : new RegExp(`${IDLE_TIMEOUT_SPEC.source}|${RENEWAL_SPEC.source}`),
  projects: [
    {
      name: "specs",
      testIgnore: [BULK_SPECS, IDLE_TIMEOUT_SPEC, RENEWAL_SPEC],
    },
    {
      name: "bulk",
      testMatch: BULK_SPECS,
      dependencies: ["specs"],
    },
    {
      name: "idle-timeout",
      testMatch: IDLE_TIMEOUT_SPEC,
      use: { baseURL: IDLE_TIMEOUT_BASE_URL },
    },
    {
      name: "renewal",
      testMatch: RENEWAL_SPEC,
      use: { baseURL: RENEWAL_BASE_URL },
    },
  ],
});
