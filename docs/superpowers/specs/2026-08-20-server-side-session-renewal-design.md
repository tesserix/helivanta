# Session renewal must not depend on a cross-site cookie

**Issue:** [#916](https://github.com/tesserix/helivanta/issues/916)
**Amends:** D4a of `docs/superpowers/specs/2026-08-15-zitadel-auth-design.md`
(":140-179"). D4a chose browser-driven renewal on a premise that has since
lapsed — see D2. D4a's *guarantee* is preserved; only its mechanism changes.
**Preserves unchanged:** D3 (renewal never moves `idle_deadline`) and every
test pinning it.
**Adjacent:** [#915](https://github.com/tesserix/helivanta/issues/915) — moving
the OIDC code exchange server-side. Independent of this, but the two meet: once
renewal no longer needs the browser, the only remaining browser-side token
handling is the initial exchange.

## The defect, observed in production

A signed-in clinician is thrown back to `/login` roughly **five minutes** after
signing in, mid-task, in every browser.

Zitadel's session cookie on `auth.tesserix.app` is **`SameSite=Lax`** (read from
DevTools against production, 2026-08-20). A `Lax` cookie is never sent on a
cross-site subresource or iframe request. Renewal is exactly that: a hidden
iframe on `helivanta.app` making a `prompt=none` request to `auth.tesserix.app`
(`apps/shell/lib/oidc.ts:38`). The cookie is withheld, Zitadel sees no session,
`prompt=none` answers `login_required`, and `signinSilent()` throws.

`apps/shell/components/session-renewal.tsx:42-76` then does:

```js
renewSession(currentTenantId).catch(() => {
  if (!cancelled) window.location.href = "/login";
});
```

on a 5-minute interval (`RENEWAL_INTERVAL_MS`, `apps/shell/lib/renew.ts:16`).
**The first renewal attempt evicts the user**, well before the 15-minute
`SESSION_TTL` (`backend/internal/config/config.go:323`) or the idle deadline.
Observed live: bounce at ≤334s after the session token was issued, with `exp`
and `idle_deadline` both still 566s in the future.

This is not pending third-party-cookie deprecation. It is broken now, for the
ordinary reason that a `Lax` cookie does not travel in an iframe.

**Why it stayed invisible:** sign-in is a top-level navigation — the one case
`Lax` permits — so login works and every server-side signal reads green.
Renewal failure is purely client-side; Helivanta's logs cannot show it. The only
symptom is a user saying the system keeps logging them out, which reads as a
session-length complaint rather than an auth defect.

## D1 — Renewal moves server-side, and the browser leaves the loop entirely

Renewal becomes a same-origin call carrying only the existing Helivanta session
cookie. No IdP round trip through the browser, no iframe, no `prompt=none`, no
dependence on any cookie policy.

`oidc-client-ts`'s `signinSilent`, the silent-renew route
(`apps/shell/app/api/auth/silent-renew/page.tsx`) and `silent_redirect_uri`
(`oidc.ts:38`) are removed rather than left dormant — a mechanism that cannot
work must not stay in the bundle looking like a fallback.

**Why this and not the alternatives**, all four of which were considered:

| Option | Rejected because |
|---|---|
| Top-level `prompt=none` redirect every 5 min | A full page navigation mid-consultation, losing in-progress form state. Trades a session bug for a clinical-safety one. |
| Serve Zitadel same-site (`auth.helivanta.app`) | Cheapest, and genuinely fixes today's case. But the issuer is baked into the shell image at build time (`.github/workflows/images.yml:271,357`), pinned in the API chart (`helivanta-api/values.yaml:217`), boot-checked against `hostedLoginUrl`, and stamped as `iss` on every existing token. And it does nothing for per-tenant custom domains, where the IdP is third-party to every tenant origin permanently. |
| `SameSite=None` on Zitadel's session cookie | Makes the IdP session cookie sendable from any site on the internet, to fix a Helivanta problem. Also dies under custom domains. |
| **Server-side renewal (this spec)** | Immune to cookie policy, survives per-tenant custom domains, and removes the browser from a security decision. |

## D2 — D4a's premise lapsed; its guarantee does not have to

D4a chose browser-driven renewal for a stated and, at the time, correct reason:

> Helivanta verifies the Zitadel ID token at login and keeps nothing; `userinfo`
> needs the **access** token. So an upstream re-check demands Helivanta hold an
> IdP credential — **unless renewal is not a server-side operation at all.**

**Helivanta now holds an IdP credential.** #854/#867 gave the API the
**login-client PAT** (`ZITADEL_LOGIN_CLIENT_TOKEN`), an instance-level
credential that can already create a session for any user — strictly more
powerful than what an upstream re-check needs. The condition D4a's choice hung
on is no longer true, so the choice is open again. (This is the same
lapsed-premise argument as #915, and for the same reason.)

So the server-side re-check D4a ruled out is now available **without** storing a
refresh token and **without** requesting `offline_access`. D4a's two stated
properties survive intact:

- **Helivanta still stores no IdP refresh token**, and still never requests
  `offline_access`. A PAT is a service credential, not a user credential; it
  grants nothing per-user that a stolen refresh token would.
- **Membership is still re-checked every renewal** — that check is OpenFGA, and
  it never needed the browser.

## D3 — What renewal must still prove, and what already proves it

D4a's real guarantee is **bounded latency on upstream deactivation**: a user
disabled directly in Zitadel, with no corresponding Helivanta action, must lose
access within one TTL. That is narrower than "revocation", because revocation is
already enforced elsewhere — on **every request**, not merely at renewal:

| Mechanism | Where | Covers |
|---|---|---|
| Credential-revocation watermark | `backend/pkg/authn/authn.go:70-107`, per request | Self sign-out, admin revoke. `signout.go:92-99`: *"This is the WHOLE of revocation, post-#838 … refused by authn.Middleware on their very next request, regardless of what Zitadel still believes."* |
| OpenFGA permission resolve | `backend/pkg/authz/middleware.go:29-47`, per request | Removed membership, narrowed role — effective on the next request |

Neither depends on renewal or on any IdP round trip. **Only the pure-Zitadel
deactivation case rides on renewal**, and this spec keeps it by making the check
server-side:

> On renewal the API asks Zitadel, using the login-client PAT, whether the
> subject is still active, and refuses the renewal if not.

That is a *stronger* bound than today's, because it no longer depends on the
browser being able to reach the IdP. Today, ironically, this check does not run
at all in production — the round trip it relies on always fails.

**Fail closed.** If Zitadel cannot be reached to answer, renewal is refused
rather than granted. A refused renewal costs a re-login; a granted one on an
unreadable answer is exactly the bound this decision exists to hold. The
direction is argued at the decision point in code, per the repo standard.

## D4 — Renewal keeps its current shape where that shape is load-bearing

Two properties of today's implementation are not incidental and must survive:

1. **Renewal is distinguished from a genuine login by the presence of the
   existing session cookie** (`credentials: "same-origin"`,
   `apps/shell/lib/auth-exchange.ts:33-44`), and that is what carries
   `idle_deadline` forward (`idleDeadlineFor`,
   `backend/internal/modules/iam/login.go:413-435`). A redesign that loses this
   makes every renewal mint a *fresh* idle window — silently defeating the idle
   timeout (#848) and the shared-ward-terminal threat model behind it. D3 of the
   idle-timeout design and `TestRenewalDoesNotMoveTheIdleDeadline`
   (`login_test.go:596-616`) stay green, untouched.
2. **A since-revoked member is refused at renewal**
   (`TestLogin_RenewalForSinceRevokedMemberIsRefused`, `login_test.go:297-330`).

The renewal endpoint therefore reuses the existing idle-deadline and membership
logic rather than reimplementing it beside `Login`.

## D5 — The client interval's coupling gap is in scope

`RENEWAL_INTERVAL_MS` is a hardcoded client constant (`renew.ts:16`), and the
file already flags the problem itself: nothing couples it to the server's
`SESSION_TTL`, which is env-driven (`config.go:323`). Set `SESSION_TTL` below
5 minutes and every session dies mid-use; nothing catches it.

Since renewal is being rebuilt, the renewal response states when the client
should next renew, and the client obeys that rather than a constant it invented.
The coupling becomes structural instead of a comment describing a gap.

## D6 — The test harness must be able to see this class of bug

**No test could have caught #916, and that is the finding worth the most.**
`e2e/playwright.config.ts:74,94` serves the app at `localhost:4301` while the
dev Zitadel issuer is `localhost:20080`. For `SameSite` purposes a site is
scheme + registrable domain — **ports are not part of it** — so dev and CI are
*same-site*, the cookie is sent, renewal succeeds, and production's cross-site
reality is never exercised.

This is the same shape as #894: a harness more permissive than production
manufactures confidence. It is fixed here rather than noted:

- The e2e stack serves the app and the IdP on **different registrable domains**
  (e.g. `helivanta.localhost` and `auth.tesserix.localhost`, which Chrome treats
  as loopback and therefore as secure contexts, while being distinct sites).
- An e2e test signs in and **stays signed in across at least one renewal
  interval**, asserting the user is still authenticated — the assertion that
  would have failed against production today.

Without D6 this fix is unverifiable by CI and the next regression is silent
again.

## Out of scope

- **#915** — the initial code exchange stays in the browser in this slice.
  Renewal and initial login are separable, and this issue is the live defect.
- **Changing the Zitadel issuer origin.** Considered and rejected in D1.
- **Session lifetime and idle timeout policy.** `SESSION_TTL`, `IDLE_TIMEOUT`
  and D3's carry-forward semantics are unchanged. This slice changes *how*
  renewal happens, never *how long* a session lives.
