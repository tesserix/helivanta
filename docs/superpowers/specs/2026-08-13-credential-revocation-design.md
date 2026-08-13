# Credential revocation — design

**Issue:** [#781](https://github.com/tesserix/hms/issues/781) — Revocation must take
effect on the next request.
**Status:** implemented 2026-08-13. Where the implementation decided
differently from the approved design, this document has been corrected rather
than left describing a design the code does not have; each such point is marked
**Implemented as**.
**Related:** #774 (foundation audit, Tier 2), #683, #699, #35, #424, #54.

---

## Problem

Nothing in HMS can make an outstanding credential stop working. Three defects,
each verified against the code on 2026-08-13, compose into one gap: **an access
decision made at sign-in is never revisited.**

1. **No revocation check.** `pkg/authn/gip.go:85` calls `VerifyIDToken`, not
   `VerifyIDTokenAndCheckRevoked`. `grep -rn "CheckRevoked\|RevokeRefreshTokens"
   backend/` returns nothing. Disabling an account has no effect on requests
   already carrying a valid ID token, for up to an hour.

2. **Sign-out does not sign out.** `apps/shell/app/logout/route.ts` clears the
   `hms_session` cookie and redirects. It does not call Firebase `signOut()`, so
   the SDK session persists in IndexedDB and a client-side `getIdToken()` can
   re-POST `/api/session` to restore the session. On a shared ward terminal the
   previous user's session is recoverable from the browser console.

3. **`authz.Public` bypasses the resolved permission set.**
   `PermissionSet.Has` (`pkg/authz/authz.go:71`) short-circuits to `true` for
   `Public` before consulting the set, so a `Public` route serves a member whose
   membership has been revoked — their empty set is never examined.

The scope of (3) is narrower than the audit's original phrasing, and the
correction matters: `iam.switchTenant` *does* gate on membership before minting
a token (`internal/modules/iam/me.go:150`), so a non-member cannot mint their
way into a tenant. The exposure is **ex-members and revoked members**, not
arbitrary authenticated users.

### Why one story rather than three

They share a root cause and a fix surface. Fixing (1) without (2) leaves the
credential alive in IndexedDB. Fixing (2) without (3) leaves a revoked member
served on unguarded routes. Any two without the third leaves a working bypass.

---

## Decisions

Each decision below was taken explicitly; the rejected alternative is recorded
because the reasoning is what survives to the next decision.

### D1 — HMS owns the revocation control plane; GIP is mirrored into it

A `iam_credential_revocations` table holds a per-subject **watermark**. A token
whose `auth_time` predates the subject's watermark is refused. HMS-initiated
revocations write it directly and bite on the very next request, with **no
identity-provider call on the request path**.

*Rejected: GIP as the per-request authority* (`VerifyIDTokenAndCheckRevoked`).
Strictly correct for account disable, but it puts a GIP `GetUser` round trip on
every request — latency on every call, a hard availability dependency, and
Identity Platform quota consumed proportional to traffic. It also does not cover
membership removal, which GIP knows nothing about.

*Rejected: GIP cached with a TTL.* Revocation is then never immediate, and the
window is precisely what an incident responder is trying to close.

**Accepted cost:** an account disabled in the GIP console alone is not honoured
until an HMS revoke is issued. There is no reconciliation job. See Limitations.

### D2 — The watermark compares `auth_time`, never `iat`

A Firebase ID token refresh mints a token with a fresh `iat` but carries the
**original** `auth_time`: refreshing is not re-authenticating. Comparing against
`iat` lets any client holding a live refresh token walk through the watermark by
refreshing. Comparing against `auth_time` kills the entire refresh chain back to
the actual sign-in.

This is the single detail that decides whether the mechanism works at all, and
it has a dedicated test (T1) whose whole purpose is to fail if someone
"simplifies" the comparison.

A token carrying no `auth_time` claim is **rejected** — a token that cannot be
evaluated against the watermark is not a token that can be trusted.

### D3 — Revocation is subject-scoped; membership is tenant-scoped

Two mechanisms, deliberately not generalised into one.

| | Scope | Mechanism | Effect |
|---|---|---|---|
| Sign-out, admin revoke | subject (global) | watermark | ends sessions in **every** tenant |
| Membership removal | tenant | per-request `Check` | affects **only** that tenant |

Membership removal must never be a watermark write: losing membership in tenant
A must not touch a session in tenant B, where the person is still legitimately
employed.

### D4 — NATS broadcast for propagation, Postgres as the durable backstop

Revocation writes the row and publishes `hms.in.iam.credential_revoked.v1`
through the existing outbox **inside the same transaction**. Every replica runs
an ephemeral JetStream consumer, so all replicas receive every message rather
than competing for it, and the consumers vanish with the pod rather than
accumulating.

**Implemented as** an ephemeral **unnamed** consumer (`events.Broadcast`,
`Bus.StartBroadcasts`, `DeliverNew`) rather than the named
`iam-revocation-<instance>` this design first proposed. A per-instance name
solves the competing-delivery problem but reintroduces the leak it was meant to
avoid — a named consumer outlives the pod that created it, so every restart
accumulates one. Unnamed is strictly better here because a broadcast has nothing
to resume: it carries no durable state, and a replica that reconnects reads
through to Postgres anyway.

The published event's `TenantID` is **deliberately left empty**. Revocation is
subject-scoped (D3), so there is no tenant it belongs to; `events.Event.TenantID`
exists only to scope the consuming transaction's RLS GUC, and
`iam_credential_revocations` carries no `tenant_id` at all.

This deviates from the `<module>-<purpose>` consumer-naming rule in
`docs/standards/backend.md`, which assumes a work queue. The deviation and its
reason are written into that standard as part of this work.

*Rejected: Redis.* No Go code uses Redis today; it would add a runtime
dependency and a second source of truth on the authentication hot path, and
still costs a network call per request.

*Rejected: short-TTL cache with no invalidation.* The TTL then **is** the
revocation delay, which is the property #781 exists to remove.

### D5 — Sign-out ends all of that subject's sessions

GIP ID tokens carry no session identifier (`sub`, `iat`, `auth_time`; no `jti`),
so revocation is necessarily by subject. Signing out on the ward terminal also
ends that user's session on their phone.

This is correct for the shared-workstation threat that motivates the issue and
matches Firebase's own `revokeRefreshTokens` semantics. Per-session and
per-device granularity requires a session registry, which is the foundation of
device management (#424) and the identity service (#35); building a half-version
here would pre-empt both designs.

### D6 — Membership becomes a first-class, derived FGA relation

A `tenant` type whose `member` relation is derived, never granted directly:

```
tenant:<tenantID>  granted_role  role:<tenantID>/<roleKey>
member = granted_role → assignee
```

There is deliberately **no** `RevokeTenantRole` on the client. A stale tenant
edge is removed by the reconciler's prune pass through the same `DeleteTuple`
every other tuple goes through — prune has no per-type branch, so a dedicated
revoke method would be a second way to remove an edge, on a client handed to
code that must not be able to strip membership from every holder of a role in
one call.

"Member" therefore means exactly "holds a role in this tenant" — the definition
`switchTenant` already uses, expressed once in the model instead of inferred
twice in Go. The per-request cost is a `Check`, not another `ListObjects`.

*Rejected: a second `ListObjects` for role bindings.* No model change, and it
cannot drift from `switchTenant` because it is the same code — but it doubles
FGA load on the hot path, on top of the uncapped `ListObjects` #774 already
flags.

*Rejected: deriving membership from a non-empty permission set.* Zero extra
calls, but it re-couples the two things this design separates, and gives the
wrong answer for a member holding a role that grants no permissions.

### D7 — Two route markers, membership required by default

```go
authz.Public              // declares no permission; membership still required
authz.NoTenantMembership  // authenticated only; membership not examined
```

`authz.Public` keeps its name and narrows in meaning, so every existing use
silently gains a membership check. `NoTenantMembership` is deliberately awkward
to type, and an arch test pins it to an allowlist of exactly four routes.

*Rejected: one marker plus a path-prefix exemption in middleware.* The exemption
becomes an invisible string match rather than a declaration at the route, and a
new route under `/me` would silently inherit it — the weakest enforcement tier
in `docs/standards/engineering-principles.md` §4.

---

## Architecture

### Request path

```
Authorization: Bearer <id-token>   (or hms_session cookie)
  │
  ├─1─ signature + expiry      pkg/authn — unchanged, local, cached JWKS
  │
  ├─2─ revocation              NEW. watermark[subject] vs token.AuthTime
  │                            in-process LRU; miss → Postgres
  │                            error → 503 authz_unavailable
  │
  ├─3─ membership              NEW. Check(user, member, tenant:<tid>)
  │                            skipped only for authz.NoTenantMembership
  │                            error → 503 · not a member → 403
  │
  └─4─ permission              authz.Require — unchanged
                               skipped for authz.Public
```

### Ownership

`pkg/authn` defines the `RevocationChecker` interface. The `iam` module owns the
table, the migration and the implementation. `cmd/api` wires it in — the same
shape as the existing `platform.RoleLister`.

The checker is deliberately **not** implemented inside `pkg/authn`, which would
deepen the `pkg/` → `internal/` inversion the foundation audit flagged.

### Schema

```sql
CREATE TABLE iam_credential_revocations (
  subject     text PRIMARY KEY,        -- GIP UID; global, not tenant-scoped
  revoked_at  timestamptz NOT NULL,    -- the watermark
  reason      text NOT NULL,           -- 'sign_out' | 'admin_revoke'
  actor       text NOT NULL,           -- self, or the acting admin's subject
  updated_at  timestamptz NOT NULL DEFAULT now()
);
```

One row per subject, upserted with `GREATEST(existing, new)` so the watermark
can only move forward: a late-arriving retry can never resurrect a revoked
credential.

The table is **not tenant-scoped**, because a GIP subject is global. It is
therefore an explicit exception to `LintRLS`, added to the allowlist **with the
reason recorded at the allowlist entry**. It holds no tenant data and no PHI.

### Cache

Each replica keeps an LRU of `subject → watermark`, **including negative
entries** — "not revoked" is the overwhelmingly common answer and the one that
must not reach Postgres.

| Parameter | Value | Reasoning |
|---|---|---|
| Max entries | **10,000** | Bounds memory at roughly a megabyte. A single hospital's concurrent staff is two orders of magnitude below this, so eviction should effectively never occur; if it does, the evicted entry reads through and is correct, only slower. |
| TTL | **5 minutes**, applied to positive **and** negative entries | The backstop for a missed broadcast, not the propagation mechanism. Five minutes is short enough to bound the abnormal case to something an incident responder can accept and long enough that steady-state traffic does not hammer Postgres. |

Both are constants in code, not configuration: a deployment that tunes the TTL
upward silently widens the security window, so changing it should require a code
change and a reviewer.

A replica's cache starts empty and reads through, so a restart is always
correct, never stale.

### Triggers

| Trigger | Writes watermark | Scope |
|---|---|---|
| `POST /v1/iam/me/sign-out` | yes, + GIP `RevokeRefreshTokens` | all tenants |
| `POST /v1/iam/subjects/:subject/revoke` | yes, + GIP `RevokeRefreshTokens` | all tenants |
| Membership removal | **no** | that tenant only, via the `Check` |

---

## Sign-out

**Backend.** `POST /v1/iam/me/sign-out`, marked `authz.NoTenantMembership` —
someone whose membership was just revoked must still be able to sign out. It
writes the watermark, publishes the event, and calls GIP `RevokeRefreshTokens`
so the identity provider agrees rather than quietly disagreeing.

**Frontend.** Sign-out calls the API, clears the Firebase SDK's IndexedDB state
with `await signOut(firebaseAuth())`, drops the permission cache, clears the
cookie, and sends the browser to `/login`. The Firebase `signOut()` is what
closes the shared-workstation hole; without it the SDK session survives and can
rebuild the session.

**Implemented as** two halves rather than all of it in
`apps/shell/app/logout/route.ts`, because the Firebase client SDK is not
reachable from a server route handler and is configured in exactly one app:

- `apps/shell/app/logout/route.ts` (server) does the same-origin check, POSTs
  `/v1/iam/me/sign-out` to the API, and clears the cookie **only after** that
  call succeeds — telling a caller they are signed out when they are not is the
  failure this endpoint exists to prevent. It returns JSON, not a redirect,
  since it is now reached by `fetch` rather than by navigation.
- `packages/ui/src/hms-shell.tsx` (client) drops the permission cache, runs the
  sign-out sequence, then navigates to `/login`. It gained an **optional
  `onSignOut` prop** for the Firebase-aware sequence: `HmsShell` renders in
  every zone app, and only `apps/shell` carries Firebase config
  (`apps/shell/lib/firebase.ts`), the same constraint that already applies to
  `tenantPicker`. Unset, `HmsShell` still POSTs `/logout` itself, so every zone
  can revoke the session and clear the transport cookie — the server-side half,
  which is the half the watermark depends on, is never optional.

### CSRF stops being harmless

Today `/logout` is a `GET` reached by a plain `<a>`, so `<img src="…/logout">`
logs a user out — irritating, not dangerous. Once it revokes **every** session
for the subject, the same trivial attack becomes a remote denial of service
against a clinician mid-shift.

Sign-out therefore moves to `POST` with the same-origin `sec-fetch-site` check
`/api/session` already uses, and the sidebar link becomes a form submission.
`docs/standards/frontend.md` says cross-zone links are plain `<a>`; this is a
documented exception, because it is not navigation, it is a state change.

### Admin revoke has a cross-tenant effect

A credential is global, so revoking it necessarily ends that subject's sessions
in every tenant: a tenant admin at hospital A can end a locum's session at
hospital B. The alternative — forcing A to leave a known-compromised credential
alive elsewhere — is worse. The effect is accepted and bounded:

- The target subject **must be a member of the acting admin's tenant**, or any
  admin could revoke any subject on the platform.
- The permission is declared with **no system roles**, so only `tenant_admin`
  holds it via the implicit grant.
- Every revoke logs actor, target, tenant and reason.
- The cross-tenant consequence is stated in the API documentation, not
  discovered by a hospital when a doctor is logged out of somewhere else.

---

## Model versioning and the migration hazard

`ensureModel` currently returns early if **any** model exists — "the model is
immutable in practice, so a store that has one is already correct." D6 ends that
assumption. It becomes: read the latest model, compare its type definitions
against the desired ones, write a new version if they differ, then re-read and
pin to the deterministically-latest model ID — the same racing-replica
resolution `ensureStore` already performs, for the same reason.

**The hazard.** An existing store has no `tenant:… granted_role …` tuples. The
moment the membership check goes live against that store, every `Check` returns
false and **every user in every tenant is locked out** — a total outage, not a
degradation.

The boot reconciler is already authoritative (it grants *and* prunes), so the
ordering that saves us is **write model → reconcile → serve**. That ordering
must be verified rather than assumed. If the reconciler does not already run to
completion before the listener starts, making it do so is part of this work.

Acceptance test T5 is exactly this scenario.

**Verified, not assumed.** Against the running local stack: all three
`tenant:<id> granted_role role:<id>/<key>` edges were deleted from the live
OpenFGA store, and a membership-gated request from a legitimate `tenant_admin`
was then refused `403 not a member of this tenant` — the outage, reproduced. The
API was restarted against that edge-less store; the boot log shows
`reconcile: prune complete` at `16:21:02.124538` and `api listening` at
`16:21:02.129321`, the edges were back at 3, and the same request answered 200.
The listener never opens on an unreconciled store, because `platform.Reconcile`
returns before `ListenAndServe` is reached in `cmd/api/main.go`.

---

## Error handling

Every failure closes.

| Failure | Behaviour |
|---|---|
| Cache miss and Postgres unreachable | 503 `authz_unavailable` — never admit |
| FGA `Check` fails | 503 `authz_unavailable` — matches the existing contract |
| Not a member | 403 |
| Outbox publish fails | Whole transaction rolls back; the revocation did not happen, and the caller is told so |
| Consumer handler panics | Existing `recover()` containment naks; replica keeps a stale entry until TTL |
| NATS entirely down | Writes still succeed and persist; propagation degrades to the TTL bound |
| Token has no `auth_time` | Reject |

### The residual propagation window, stated plainly

Between commit and message arrival, a replica holding a cached negative entry
still admits. Normally sub-second. If a replica misses the message entirely —
disconnect, restart mid-flight — it admits until the TTL expires that entry.

**Immediate in the normal case, bounded by the 5-minute TTL in the abnormal
one.** This is materially different from the rejected cached-GIP option, where
the TTL *was* the mechanism and every revocation waited on it.

---

## Testing

Per `docs/standards/engineering-principles.md` §5, every assertion is proven
able to fail before it is trusted.

| ID | Test | Why it exists |
|---|---|---|
| T1 | A **refreshed** token is still rejected — new `iat`, original `auth_time`, post-watermark | The test that fails if someone "simplifies" D2 to `IssuedAt`, reducing revocation to a suggestion |
| T2 | Revoked membership in tenant A → **403 on a `Public` route** | The literal regression test for defect (3) |
| T3 | Membership revoked in A → session in **B still works** | Proves D3's two mechanisms stayed separate |
| T4 | A user with **zero memberships** can still call `/me/tenants` | Proves the lockout edge is handled, not asserted |
| T5 | **Old-model store, no tenant tuples, fresh boot → member's request succeeds** | Migration safety; the outage scenario above |
| T6 | Postgres unreachable on cache miss → **503, never 200** | Fail-closed, asserted on the real response |
| T7 | Watermark **cannot move backwards** under a replayed or late write | Idempotent-retry safety on a security control |
| T8 | **Cross-site POST to sign-out rejected** | The CSRF-becomes-DoS case |
| T9 | Two API replicas sharing NATS + Postgres: revoke on one → **the other rejects** | Proves propagation propagates, rather than testing the publish call and assuming |
| T10 | After shell sign-out, **`getIdToken()` + re-POST `/api/session` is refused by the API** | The shared-workstation guarantee, end to end in Playwright — not "the cookie was cleared", which is the proxy assertion that let this bug exist |
| T11 | Concurrent revoke + in-flight requests under `-race` | Cache mutation under load |
| T12 | Arch test: the `NoTenantMembership` allowlist is exactly four routes | Stops the unsafe set growing silently |
| T13 | A cached negative entry **is invalidated** by the event | The cache is the thing most likely to be wrong |

---

## Observability

Structured logs with `subject`, `reason` and `actor` on every revocation, and on
every request rejected by the watermark — the fields an incident responder needs
to answer "when did this stop working, and who did it".

Deliberately **not** building an audit trail here: #54 owns the canonical audit
event, and half-building it would pre-empt that design. These log sites become
audit emission points when #54 lands, and this spec is the record of that
intent.

---

## Limitations

Stated rather than discovered later.

- **A GIP-console-only account disable is not honoured** until an HMS revoke is
  issued. There is no reconciliation job; that option was considered and
  declined (D1). The supported path for disabling an account is the HMS revoke
  endpoint, which also calls GIP.
- **Revocation is per subject, not per session or per device.** Signing out
  anywhere signs out everywhere (D5). Per-device revocation is #424.
- **The propagation window** is sub-second normally and bounded at 5 minutes
  when a replica misses the broadcast.
- **Admin revoke crosses tenant boundaries** by construction, bounded as
  described above.
- **No user-administration UI.** The revoke endpoint is the control; the
  AdminConnect screen is a later story.
- The uncapped `ListObjects` in `Resolve`, and the connection-pool ceiling of
  5, are untouched here. Both are #774 Tier 2 items in their own right.

---

## Out of scope

- The identity service, MFA, passkeys, enterprise SSO — #35, #422, #423. This
  hardens the GIP implementation that exists today and must not pre-empt that
  design.
- Device identity and device-bound refresh tokens — #424.
- Remote session-management UI — a separate Identity & Access story.
- Login history and its query surface — #42.
- Idle timeout and step-up authentication — #35.
- The canonical audit event — #54.
