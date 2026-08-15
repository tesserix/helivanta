# Zitadel authentication — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development.
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace GIP with Zitadel, with HMS issuing its own session, without
weakening a single invariant the last month of auth work established.

**Spec:** `docs/superpowers/specs/2026-08-15-zitadel-auth-design.md`
**ADR:** `docs/adr/0006-zitadel-not-gip.md` (supersedes ADR-0002)
**Spike:** `docs/superpowers/spikes/2026-08-15-zitadel-spike.md` — the observed behaviour this rests on
**Issue:** #838. Branch: `feat/838-zitadel`.

## Global constraints

- `docs/standards/engineering-principles.md` is binding. **§3: this is a data /
  identity control and fails CLOSED** — the opposite of #689's rate limiter.
  Anything that cannot decide, denies. §4 (compile error > boot failure > CI
  failure > convention). §5 (**every new assertion observed failing first**).
- **This is the most security-sensitive code in the repo.** A defect here is an
  authentication bypass, not a bug. Where a choice is between "clever" and
  "obvious", take obvious.
- `make lint-go` clean; `cd backend && go test -count=1 -race ./...` green;
  `./scripts/coverage-gate.sh` green (**unpiped**); `pnpm turbo lint type-check
  test build` green; Playwright green **twice**.
- Docker required. Run everything from the repo root.

## Shape of the delivery

**Task 1 may land on its own** — it is a complete, tested unit that nothing calls
yet, and GIP keeps working meanwhile.

**Tasks 2–8 are ONE cutover and must merge together** (spec D7). Two providers
live at once would mean two token formats, two revocation models and a
`Principal` that could come from either — the branchiest possible version of the
code least able to afford it. Do not merge a partial cutover to make a review
smaller.

---

## Task 0: resolve the seeding wrinkle (blocking spike)

**This is first because it is the most likely thing to consume the schedule, and
everything e2e depends on it.** The spike explicitly did not resolve it.

*(observed)* API-created human users land **email-verification-pending**, which
blocks interactive login even with `isEmailVerified: true` — and the e2e suite
drives a real login form, so a user that cannot log in through the hosted UI is
useless to us. A machine user also has **no permissions until granted an org
role**.

- [x] **RESOLVED 2026-08-15.** Recipe found and proven end to end: seven distinct
  users seeded and logged in through the real hosted UI, each yielding an ID
  token with `auth_time` present and distinct from `iat`. See the spike doc's
  "Task 0" section.

**The trap, and why it matters beyond seeding.** `POST /management/v1/users/human`
has **no `passwordChangeRequired` field at all**, and the endpoint **silently
drops unrecognised JSON keys** — no error, HTTP 200, a user that then stalls on a
forced password-change screen. The field exists only on the sibling
`POST /management/v1/users/human/_import`.

So on this API, **a 200 does not mean what you asked for happened.** That is what
cost the first spike, and it generalises: any Zitadel call whose body we get
subtly wrong will succeed and do something other than intended.

---

## Task 1: the HMS session token

**Files:** `backend/pkg/session/` (new: `session.go`, `session_test.go`)

The unit `Principal` comes from. Complete and tested; wired to nothing yet.

- [ ] **Step 1: failing tests first**

- Round trip: mint then verify returns the same subject, tenant and `auth_time`.
- **A token signed by a different key is refused.**
- **An expired token is refused.**
- **A token with `alg: none` is refused**, and one whose algorithm was swapped to
  HMAC using the public key as the secret is refused. This is the classic JWT
  confusion attack; if the library is used carelessly it passes.
- A tampered claim (tenant changed) is refused.
- `auth_time` survives the round trip **unmodified**.
- Minting without a key is an error, never a token.

- [ ] **Step 2: implement**

Claims: `sub`, `tenant_id`, `auth_time`, `iss`, `iat`, `exp`, and a `kid` header
from the start (rotation is out of scope but must not be precluded).

**No roles, deliberately** — permissions resolve per request from OpenFGA, and a
role in a token is a stale answer that survives until expiry.

Asymmetric (EdDSA or RS256). Pin the accepted algorithm explicitly; never accept
what the token header asks for.

- [ ] **Step 3: prove each assertion fails**

Mutation each of: accept any algorithm; skip expiry; skip signature; reset
`auth_time` to now on mint. Each must break its test. Report real output.

The `auth_time` mutation matters beyond hygiene: resetting it would launder an
old authentication into a fresh one and quietly defeat the revocation watermark.

- [ ] **Step 4: gates and commit**
`feat: add the HMS session token, signed asymmetrically with auth_time carried through (#838)`

---

## Task 2: signing key configuration, failing closed

**Files:** `backend/internal/config/config.go`, `backend/cmd/api/main.go`

- [ ] Load the private key from config (secret manager in production, per #45).
- [ ] **The API refuses to boot without it.** Not generated, not defaulted: an
  ephemeral key silently invalidates every session on restart, and a default key
  is a forged-session vulnerability. Argue the direction in a comment at the
  decision point (§3) — this fails CLOSED, unlike `LOG_LEVEL`.
- [ ] Dev uses a fixed local key, **refused outside dev**, mirroring the existing
  `HMS_ENV` emulator guard. Test that guard.
- [ ] **Prove it:** boot with no key → refuses, naming the variable. Boot with
  the dev key and `HMS_ENV=production` → refuses.

Commit: `feat: refuse to boot without a session signing key (#838)`

---

## Task 3: the Zitadel verifier

**Files:** `backend/pkg/authn/zitadel.go` (new), `gip.go` (verifier removed; minter/revoker stay until Task 5), `authn.go`, `cmd/api/main.go`

> **Sequencing corrected 2026-08-15.** This task originally said "delete
> `gip.go` and the Firebase dependency in the same commit". That is not
> buildable: `TokenMinter` is used by `iam/me.go`'s `switchTenant` and
> `TokenRevoker` by sign-out/revoke, so deleting the file before Task 5 replaces
> those call sites cannot compile.
>
> D7 forbids **merging** a dual-provider state to `main`; it does not require
> every intermediate commit on the branch to be single-provider. So: Task 3
> replaces the *verifier* only. `gip.go` keeps its minter and revoker, clearly
> marked as dying, until Task 5 removes their last callers — and Task 7's `grep`
> is what proves nothing survived.

- [ ] Implement `TokenVerifier` with `github.com/coreos/go-oidc/v3` against
  Zitadel's discovery document *(observed to work, with negative-case proof)*.
- [ ] **Delete the GIP verifier path** and switch `cmd/api` to the Zitadel one.
- [ ] **Leave `TokenMinter` and `TokenRevoker` in place**, with a comment on each
  naming Task 5 as what removes them. The Firebase dependency therefore survives
  this task; Task 5 drops it.
- [ ] Tests: a valid token verifies; wrong issuer, wrong audience, expired, and
  tampered tokens are each refused. Reuse the spike's negative cases.
- [ ] **A token with no `auth_time` is refused** — the existing contract, and the
  reason is not hygiene: a credential that cannot be evaluated against the
  watermark is not one that can be trusted.

Commit: `feat: verify Zitadel ID tokens through standard OIDC (#838)`

---

> **Interim-state note, added after Task 3 (2026-08-15).** From Task 3 until
> Task 4/5 land, the Zitadel verifier populates only `Subject` and `AuthTime`;
> `Principal.TenantID` is `""`. Task 3's report claimed a grep found no code
> reading `TenantID` raw — **that is wrong**. Several paths read it without going
> through `TenantPrincipal`:
>
> - `pkg/authz/middleware.go` → `Resolve(subject, p.TenantID)`
> - `pkg/authz/membership.go` → the membership gate
> - `pkg/ratelimit/middleware.go` → the bucket key `"tenant:"+p.TenantID`
> - `internal/platform/listroute.go` → the pagination cursor
>
> The branch is expected to be non-functional for tenant traffic in this window;
> what matters is that it is non-functional in the **closed** direction. The
> reasoning is that an empty tenant resolves to an empty permission set and no
> membership, so authz denies before any handler runs — but **that has been
> reasoned, not observed**, and reasoning is what this repo keeps catching itself
> on.
>
> **Task 4 must verify it empirically against a real OpenFGA** before relying on
> it: an empty tenant must deny, not admit. If it admits, that is a
> cross-tenant hole and it stops the task.
>
> Note also `"tenant:"+p.TenantID` collapses every caller into one shared
> rate-limit bucket while `TenantID` is empty. Harmless in this window (a
> capacity control, and nothing gets past authz anyway), and it disappears once
> the session supplies the tenant — but it is the kind of thing that would be
> mystifying if hit during local development, so it is written down.

## Task 4: login exchanges a Zitadel token for an HMS session

**Files:** `backend/internal/modules/iam/`, `apps/shell/`

- [ ] An endpoint taking a verified Zitadel ID token, resolving the caller's
  tenants from OpenFGA, and minting an HMS session for the chosen tenant.
- [ ] **A user who is a member of no tenant is refused**, and the refusal says so
  without leaking whether the account exists.
- [ ] Frontend: `apps/shell/lib/firebase.ts` → a Zitadel OIDC client; login page,
  `sign-out.ts` updated.
- [ ] Cookie flags unchanged in strictness (httpOnly, secure, sameSite).

Commit: `feat: exchange a verified Zitadel token for an HMS session (#838)`

---

## Task 5: tenant switch, and revocation

**Files:** `backend/internal/modules/iam/me.go`, `signout.go`, `revocation.go`,
`apps/shell/components/tenant-picker.tsx`

- [ ] `POST /v1/iam/me/tenant`: verify session → **check membership in OpenFGA**
  → re-mint with the new tenant and the **same `auth_time`** → set cookie. No IdP
  round trip.
- [ ] **Test that switching to a tenant the caller is not a member of is
  refused**, and that the refusal is 404 (the cross-tenant answer), not 403.
- [ ] Tenant-picker drops `signInWithCustomToken`.
- [ ] Revocation watermark unchanged and still authoritative (#781).
- [ ] **`SubjectCredentialRevoked` still publishes with NO tenant** — #835's
  outbox policy depends on it; its tests must still pass untouched.
- [ ] Session renewal re-checks the user upstream, so a Zitadel-deactivated user
  fails at next renewal. **Choose the TTL and justify it in the code comment** —
  it is the upper bound on upstream deactivation latency (spec D4).
- [ ] **Revisit the `Tight` rate-limit budget** for this route: it no longer
  calls an external service, so #689's reasoning for it no longer applies. Either
  re-justify the number or change it — do not inherit it silently.

Commit: `feat: switch tenant by re-minting the HMS session, never the IdP token (#838)`

---

## Task 6: dev stack and seeding

**Files:** `Makefile`, compose, `scripts/seed-dev.mjs`, `scripts/verify-local.sh`

- [ ] Zitadel replaces the Firebase emulator, own database, shifted ports
  *(observed to coexist with `hms-dev`)*. Promote the spike compose; delete
  `spike/zitadel-838/`.
- [ ] **Pin v4.15.3 — the version production actually runs**, not the v2.65.1 the
  first spike used. Verified 2026-08-15: the seeding recipe survives the major
  version unchanged on both `/management/v1/users/human/_import` and
  `/v2/users/human`, and both original traps reproduce identically. v4 also
  requires **Postgres 17** (Cockroach support dropped), and with
  `LOGINV2_REQUIRED=true` the core hard-404s `/ui/v2/login/*` — the separate
  `zitadel-login` service plus a same-origin reverse proxy becomes mandatory,
  not optional. `spike/zitadel-838/docker-compose.zitadel-v4.yml` is the working
  shape.
- [ ] **NOT VERIFIED, and it lands on this task's neighbour:** production runs a
  *customised* login UI (`tesserix/third-party/zitadel-login:v4.15.3-aurora.1`,
  private registry). All local verification used stock upstream. The e2e suite
  drives login UI selectors, so `e2e/tests/support/login.ts` will be written
  against a UI that is **not** the one production serves. Decide whether dev
  pulls the custom image or the suite tolerates both, and say which — do not
  discover this when the selectors fail.
- [ ] Seeding per Task 0's recipe, **one account per spec file**.
- [ ] **The seed script must verify each account by logging in, not by checking
  the status code.** Task 0 established that this API returns 200 while silently
  ignoring fields it does not recognise, so a script that trusts the response
  produces accounts that look seeded and cannot authenticate — and the failure
  surfaces later as every e2e spec timing out at the login form, which reads like
  a broken app rather than a broken seed. Assert on a completed authentication,
  the same rule §5 applies everywhere else.
- [ ] *(observed, NOT VERIFIED end-to-end)* bootstrapping the **first** machine
  PAT still needed one scripted browser login as admin. It is a one-time
  environment secret rather than a per-test one, so decide here whether that
  matters and say which.
- [ ] `verify-local.sh`'s auth round trip updated — it must still fail loudly
  when auth is broken, which is its whole purpose.
- [ ] Note *(observed)* the masterkey length gotcha: a wrong-length key
  crash-loops rather than failing fast. Add a length check or a comment where
  someone will hit it.

Commit: `feat: run Zitadel in the dev stack and seed users that can log in (#838)`

---

## Task 7: delete every GIP trace

- [ ] `grep -ri 'gip\|firebase\|identitytoolkit'` across the repo returns only
  historical references (ADR-0002, this plan, the spike). **Any live reference is
  a leftover.**
- [ ] Remove Firebase deps from `go.mod` and `package.json`.
- [ ] Update `docs/standards/backend.md` §5 Auth, both `CLAUDE.md` files, and any
  doc asserting GIP.

Commit: `chore: remove the last GIP references (#838)`

---

## Task 8: verification and PR

- [ ] Full gates, plus Playwright **twice**.
- [ ] **By hand against the running stack**, asserting on what is produced:
  login; switch hospital and confirm permissions change; sign out and confirm a
  captured token is refused; deactivate a user in Zitadel and confirm they lose
  access within the TTL; stop Zitadel and confirm sign-in **fails closed** while
  existing sessions still work.
- [ ] PR body: the three couplings and how each was replaced; that HMS now issues
  credentials and what contains that; the TTL bound on upstream deactivation; the
  invariant table, each re-proven; which assertions were observed failing.
  `Closes #838`.

---

## Known limitations (carry into the PR)

- **Upstream deactivation is bounded by the session TTL**, not immediate.
- **HMS issues credentials** — new key-management surface; #45 is a dependency.
- **Key rotation is not designed** — `kid` exists so it is not precluded. File it.
- **We operate an IdP.** An outage is a total sign-in outage; existing sessions
  survive until renewal.
- **`email`/`name` require a `userinfo` call** *(observed)*.
- **Not verified:** Zitadel deactivation events (would tighten the TTL bound),
  forcing profile/email into the ID token, production-scale behaviour.
