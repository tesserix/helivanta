import { expect, test } from "@playwright/test";
import { login } from "./support/login";

// #689: the limiter must actually refuse, and the client must be told when
// to come back — and the answer must be true, not decorative.
//
// This spec exists because a rate limiter that is configured but never
// exercised is indistinguishable from one that is broken. The dev stack
// deliberately runs enormous per-tenant and per-principal budgets (see the
// RATE_LIMIT_* block in the Makefile) so the rest of the suite is not
// throttled by its own load, which would otherwise mean the limiter is
// never exercised anywhere before production. The mint route is the one
// budget left deliberately reachable, and this spec is what reaches it.
//
// The route: POST /v1/iam/me/tenant mints a GIP custom token per call
// against project-wide Identity Platform quota, so it carries the tightest
// budget on the platform (internal/bootstrap/ratelimit.go, Tight map) and
// gets its own bucket keyed by subject AND route. Draining it therefore
// affects nothing else this spec or any other spec does.

// The user's own first hospital, seeded by scripts/seed-dev.mjs. Switching
// to the hospital the session is already in is a real, membership-checked
// mint — the point is to consume mint budget, not to move anywhere.
const OWN_TENANT_ID = "11111111-1111-1111-1111-111111111111";

// A bound on the flood, not an expectation: the refusal is expected within
// single digits.
//
// The arithmetic that matters. The dev mint budget is 60/min — one token
// per second — against a burst of 3. A sequential loop drains a token per
// round trip, so it outruns the refill whenever the round trip is under a
// second, and the bucket empties after roughly 3 / (1 - T/1000) attempts:
// ~4 at the measured T of 9ms, ~6 at 500ms, ~15 at 800ms. 100 attempts
// covers a round trip up to ~970ms, a hundred times worse than measured.
// Above that the loop cannot trip a 60/min bucket at all and no attempt
// count would help — which is exactly why the dev budget is 60 and not the
// 1000/min (a token every 60ms) that would leave only 6x of headroom over
// the round trip and let this loop spin against a limiter that works.
const MAX_ATTEMPTS = 100;

// Retry-After has one-second granularity and is rounded up, so sleeping it
// out is always sufficient; the margin only absorbs scheduling jitter.
const RETRY_MARGIN_MS = 250;

type Flood = {
  status: number;
  retryAfter: string | null;
  attempts: number;
  firstStatus: number;
  // Reported in the failure message rather than asserted on: if this test
  // ever fails with "never refused", the first question is whether the
  // round trip has grown past the refill interval — in which case the loop
  // cannot win and the dev budget, not the test, is what needs changing.
  meanRoundTripMs: number;
};

// SKIPPED, not deleted or weakened — this spec's premise no longer holds,
// as a PRE-EXISTING gap from #838 Task 5 (backend, already committed
// before this frontend task started), found live running this suite for
// #838's frontend half, not introduced by it.
//
// Design spec D3 (docs/superpowers/specs/2026-08-15-zitadel-auth-design.md)
// is explicit: "the Tight rate-limit entry for [POST /v1/iam/me/tenant] is
// no longer protecting a project-wide external quota... The budget should
// be revisited when this lands, and this spec does not silently inherit
// its reasoning." Task 5 acted on exactly that — see
// backend/internal/bootstrap/ratelimit.go's `Tight is deliberately empty
// as of #838` comment: the route now shares the SAME (dev: 100000/min)
// Tenant/Principal budgets every other authenticated route gets, not a
// separate tight one. This spec still floods the OLD 60/min mint-specific
// budget the design deliberately removed; run against the current
// backend it correctly finds no 429 within 100 attempts, because there is
// no longer a tighter budget to trip.
//
// This is a rate-limit BUDGET decision — D3 flags it as its own follow-up,
// out of scope for both Task 5 (backend) and #838's frontend half (this
// task). Skipping rather than deleting keeps the gap visible and the
// original intent on record for whoever picks up "revisit the budget" as
// its own piece of work, rather than silently losing the coverage this
// spec used to provide.
test.skip("draining the mint budget is refused with a Retry-After that is honest", async ({
  page,
}) => {
  // Login alone can take 30s under parallel workers (see support/login.ts),
  // and the flood below is bounded at MAX_ATTEMPTS round trips on top.
  test.setTimeout(180_000);

  await login(page);

  const flood = await page.evaluate(
    async ({ tenantId, maxAttempts }): Promise<Flood> => {
      const attempt = () =>
        fetch("/api/v1/iam/me/tenant", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ tenant_id: tenantId }),
        });

      let firstStatus = 0;
      const startedAt = performance.now();
      const mean = (attempts: number) =>
        Math.round((performance.now() - startedAt) / attempts);

      for (let i = 0; i < maxAttempts; i++) {
        const res = await attempt();
        if (i === 0) firstStatus = res.status;
        if (res.status === 429) {
          return {
            status: 429,
            retryAfter: res.headers.get("Retry-After"),
            attempts: i + 1,
            firstStatus,
            meanRoundTripMs: mean(i + 1),
          };
        }
      }
      return {
        status: 0,
        retryAfter: null,
        attempts: maxAttempts,
        firstStatus,
        meanRoundTripMs: mean(maxAttempts),
      };
    },
    { tenantId: OWN_TENANT_ID, maxAttempts: MAX_ATTEMPTS },
  );

  // Asserted before the 429, deliberately: a route that answered 403 or 500
  // from the very first call would exhaust the loop and fail below with
  // "never refused", which reads as a broken limiter when the truth is a
  // broken route. This says which one it was.
  expect(
    flood.firstStatus,
    "the first mint must succeed — the limiter is supposed to be refusing real work, not a route that was already failing",
  ).toBe(200);

  expect(
    flood.status,
    `never refused after ${flood.attempts} attempts (mean round trip ${flood.meanRoundTripMs}ms; ` +
      `a mint budget refilling faster than that cannot be drained sequentially)`,
  ).toBe(429);

  // Not merely present: a header of "0" is present and truthy-as-a-string,
  // and a client told to retry after zero seconds retries immediately,
  // turning the limiter into an amplifier.
  expect(
    flood.retryAfter,
    "a refusal must tell the client when to retry",
  ).toBeTruthy();
  const retryAfterSeconds = Number(flood.retryAfter);
  expect(retryAfterSeconds).toBeGreaterThan(0);

  // And the number must mean something. Waiting exactly as long as the
  // server asked must be enough to get back in — otherwise the header is a
  // decoration, and the refusal is indistinguishable from a bucket that
  // emptied permanently. This is the assertion that fails if refill breaks
  // while denial keeps working.
  const recovered = await page.evaluate(
    async ({ tenantId, waitMs }) => {
      await new Promise((resolve) => setTimeout(resolve, waitMs));
      const res = await fetch("/api/v1/iam/me/tenant", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tenant_id: tenantId }),
      });
      return res.status;
    },
    {
      tenantId: OWN_TENANT_ID,
      waitMs: retryAfterSeconds * 1000 + RETRY_MARGIN_MS,
    },
  );

  expect(
    recovered,
    `after waiting the ${retryAfterSeconds}s the server asked for, the request must be admitted again`,
  ).toBe(200);
});
