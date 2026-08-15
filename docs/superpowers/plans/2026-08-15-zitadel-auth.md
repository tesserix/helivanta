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

- [ ] Establish, against a running Zitadel, a **non-interactive** recipe that
  produces a user who can immediately complete a password login through the
  hosted UI. Record the exact API calls.
- [ ] Confirm it scales to one account per spec file (sign-out revokes globally
  per subject, so specs must not share logins — #781).
- [ ] **If no such recipe exists**, stop and report before writing any code.
  Every e2e spec depends on it, and the alternative — bypassing the login UI in
  tests — would stop testing the thing most likely to break in this migration.

Append findings to the spike doc. Commit: `docs: record how to seed a Zitadel user that can actually log in (#838)`

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

**Files:** `backend/pkg/authn/zitadel.go` (new), `gip.go` (**deleted**), `authn.go`

- [ ] Implement `TokenVerifier` with `github.com/coreos/go-oidc/v3` against
  Zitadel's discovery document *(observed to work, with negative-case proof)*.
- [ ] **Delete `gip.go` and the Firebase dependency in the same commit.**
- [ ] `TokenMinter` (the custom-token concept) **goes away entirely** — nothing
  mints IdP tokens now. `TokenRevoker`'s fate follows Task 5.
- [ ] Tests: a valid token verifies; wrong issuer, wrong audience, expired, and
  tampered tokens are each refused. Reuse the spike's negative cases.
- [ ] **A token with no `auth_time` is refused** — the existing contract, and the
  reason is not hygiene: a credential that cannot be evaluated against the
  watermark is not one that can be trusted.

Commit: `feat: verify Zitadel ID tokens through standard OIDC, deleting the Firebase SDK (#838)`

---

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
- [ ] Seeding per Task 0's recipe, **one account per spec file**.
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
