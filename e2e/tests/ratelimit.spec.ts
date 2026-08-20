import { expect, test } from "@playwright/test";
import { login } from "./support/login";

// #841: the login-exchange limiter must actually refuse, and the client
// must be told when to come back — and the answer must be true, not
// decorative. Restores the coverage e2e/tests/ratelimit.spec.ts used to
// provide before #838 Task 5 removed the mint-specific budget this spec
// used to drain (see git history / the design spec below for that story);
// this version targets #841's NEW budget instead of the retired one.
//
// The route: POST /v1/auth/login, rate limited per verified Zitadel
// subject (backend/internal/modules/iam/login.go, checked between token
// verification and the OpenFGA ListRoles call — never before verification,
// see docs/superpowers/specs/2026-08-16-login-rate-limit-design.md for why).
// The budget (backend/internal/bootstrap/ratelimit.go's LoginRateLimitRule,
// RATE_LIMIT_LOGIN_PER_MIN) is left at its PRODUCTION default in dev too —
// unlike RATE_LIMIT_TENANT_PER_MIN/RATE_LIMIT_PRINCIPAL_PER_MIN, which the
// Makefile inflates to 100000/min so the rest of the e2e suite is not
// throttled by its own load. Login's bucket is keyed per-subject and every
// spec logs in under its own account (support/login.ts), so one spec's
// login traffic cannot drain another's budget, and even the sign-in retry
// loop support/login.ts itself runs (MAX_SIGN_IN_ATTEMPTS = 4) sits well
// inside the burst of 10 — nothing in the existing suite risks tripping
// this by accident.

// A bound on the flood, not an expectation: the refusal is expected within
// single digits (burst 10), same shape as the retired mint spec.
//
// The arithmetic that matters. The login budget is 20/min — one token
// every 3s — against a burst of 10. A sequential loop drains a token per
// round trip, so it outruns the refill whenever the round trip is under 3
// seconds, and the bucket empties after roughly 10 / (1 - T/3000)
// attempts: ~11 at a measured T well under a second. 100 attempts covers a
// round trip up to ~2.7s, well beyond anything this suite has measured for
// a same-origin fetch. Above that the loop cannot trip a 20/min bucket at
// all and no attempt count would help — which is exactly why the budget is
// 20 and not something an order of magnitude looser that would leave no
// headroom over the round trip and let this loop spin against a limiter
// that works.
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
  // cannot win and the budget, not the test, is what needs changing.
  meanRoundTripMs: number;
};

test("draining the login budget is refused with a Retry-After that is honest", async ({ page }) => {
  // Login alone can take 30s under parallel workers (see support/login.ts),
  // and the flood below is bounded at MAX_ATTEMPTS round trips on top.
  test.setTimeout(180_000);

  await login(page);
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  // window.__hmsUserManager (apps/shell/lib/oidc.ts, dev/test builds only)
  // is the same technique e2e/tests/signout.spec.ts uses to get at the raw
  // id_token oidc-client-ts holds in sessionStorage — there is no bundler
  // inside page.evaluate to import getUserManager() through directly.
  await page.waitForFunction(() => Boolean(window.__hmsUserManager));
  const idToken = await page.evaluate(async () => {
    const user = await window.__hmsUserManager!.getUser();
    return user?.id_token ?? null;
  });
  expect(idToken, "precondition: a live Zitadel ID token was captured after login").toBeTruthy();

  const flood = await page.evaluate(
    async ({ token, maxAttempts }): Promise<Flood> => {
      // Re-verifying the SAME ID token repeatedly is deliberate: Zitadel
      // ID tokens are not single-use (unlike the authorization code that
      // minted this one), so this isolates the login-exchange rate limit
      // from token freshness — a stale-token failure would refuse for the
      // wrong reason and this test must not confuse the two.
      const attempt = () =>
        fetch("/api/v1/auth/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ id_token: token }),
        });

      let firstStatus = 0;
      const startedAt = performance.now();
      const mean = (attempts: number) => Math.round((performance.now() - startedAt) / attempts);

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
    { token: idToken, maxAttempts: MAX_ATTEMPTS },
  );

  // Asserted before the 429, deliberately: a route that answered 401 or
  // 500 from the very first call would exhaust the loop and fail below
  // with "never refused", which reads as a broken limiter when the truth
  // is a broken route (or a token that failed to capture). This says
  // which one it was.
  expect(
    flood.firstStatus,
    "the first re-exchange must succeed — the limiter is supposed to be refusing real work, not a route that was already failing",
  ).toBe(200);

  expect(
    flood.status,
    `never refused after ${flood.attempts} attempts (mean round trip ${flood.meanRoundTripMs}ms; ` +
      `a login budget refilling faster than that cannot be drained sequentially)`,
  ).toBe(429);

  // Not merely present: a header of "0" is present and truthy-as-a-string,
  // and a client told to retry after zero seconds retries immediately,
  // turning the limiter into an amplifier.
  expect(flood.retryAfter, "a refusal must tell the client when to retry").toBeTruthy();
  const retryAfterSeconds = Number(flood.retryAfter);
  expect(retryAfterSeconds).toBeGreaterThan(0);

  // And the number must mean something. Waiting exactly as long as the
  // server asked must be enough to get back in — otherwise the header is a
  // decoration, and the refusal is indistinguishable from a bucket that
  // emptied permanently. This is the assertion that fails if refill breaks
  // while denial keeps working.
  const recovered = await page.evaluate(
    async ({ token, waitMs }) => {
      await new Promise((resolve) => setTimeout(resolve, waitMs));
      const res = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id_token: token }),
      });
      return res.status;
    },
    {
      token: idToken,
      waitMs: retryAfterSeconds * 1000 + RETRY_MARGIN_MS,
    },
  );

  expect(
    recovered,
    `after waiting the ${retryAfterSeconds}s the server asked for, the request must be admitted again`,
  ).toBe(200);
});
