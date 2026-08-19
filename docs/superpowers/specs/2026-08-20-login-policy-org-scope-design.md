# The MFA policy must be read against the authenticating user's organization

**Issue:** [#913](https://github.com/tesserix/helivanta/issues/913)
**Supersedes:** `loginclient/sufficiency.go`'s KNOWN LIMITATIONS §2, which
documented this as an accepted limitation and named the precondition
("Adding a second org to this instance REQUIRES fixing this first") that has
since been violated — the instance has three orgs. That section is deleted,
not amended, by this change.
**Adjacent, deliberately not touched:** [#856](https://github.com/tesserix/helivanta/issues/856)
(`passwordChangeRequired` invisible on the wire — still KNOWN LIMITATIONS §1)
and [#901](https://github.com/tesserix/helivanta/issues/901) (any HTTP 400
maps to `bad_credentials`). Same file, same family, separately tracked.

## The problem, stated precisely

`CompleteIfSufficient` (`sufficiency.go`) is the whole MFA enforcement for
Helivanta's own login form. The spike proved Zitadel does **not** refuse to
finalize a password-only session against a `forceMfa` policy for a login
client — it issues an authorization code regardless. So the policy read *is*
the control, not a redundant second opinion.

That read is unscoped:

```go
if err := c.do(ctx, http.MethodGet, "/management/v1/policies/login", nil, &wire, ErrUnavailable);
```

`GET /management/v1/policies/login` resolves against the **login client PAT's
own resource owner** when the request carries no `x-zitadel-orgid` header. A
user in org B is therefore judged by org A's policy. If org A does not force
MFA and org B does, the login **completes** — an authentication bypass that
emits no error, no warning, and no distinguishable log line anywhere.

Nothing is exposed today: only `TESSERIX` holds users who sign in to
Helivanta, and it does not force MFA. **The exposure arrives on a
configuration change** — a new org, or a user moved into one — with no code
change and nothing to notice.

## What was unknown, and is not any more

`sufficiency.go` declined to guess at two facts the spike had not recorded.
Both were tested against the live instance on 2026-08-19 (#913):

1. **Zitadel honours `x-zitadel-orgid` on this endpoint.** Two org ids
   returned two different `resourceOwner`s.
2. **The session carries the user's org.** `GET /v2/sessions/{id}` returns
   `factors.user.organizationId`. The spike captured `factors: {user,
   password}` and never recorded the field, which is why it was believed
   absent.

Both are re-proven by this change's own integration test rather than trusted
from the issue: an assertion that the read is org-scoped is only worth what
the live behaviour behind it is.

## D1 — The org id comes from the session read that already happens, not from `Session`

The issue proposed carrying `organizationId` on `loginclient.Session`. **This
spec deviates**, for two reasons that are properties of the existing code, not
preferences:

- `POST /v2/sessions` does **not** return the org (its response is
  `{details, sessionId, sessionToken}` — the same reason `sessionUserID`
  exists at all). A field on `Session` would therefore be empty at every
  construction site, filled only by a later read: a field whose zero value is
  the bug.
- `Session` is **reconstructed from a database row** on the TOTP path
  (`loginui.go`: `loginclient.Session{ID: attempt.SessionID, Token:
  attempt.SessionToken}`). An `OrgID` field would need a migration and a
  column that can go stale, to carry a value that path does not use —
  `CompleteAfterFactor` reads no policy.

Instead the org id is read where the code **already** reads it.
`CompleteIfSufficient` calls `classifyEnrolledMethods` → `enrolledMethodTypes`
→ `sessionUserID`, which is a `GET /v2/sessions/{id}` on every login. That one
response carries `factors.user.organizationId` beside `factors.user.id`.

`sessionUserID` becomes `sessionSubject`, returning both ids:

```go
type sessionSubject struct{ UserID, OrgID string }
```

**No new round trip.** `classifyEnrolledMethods` returns the subject alongside
its two booleans so `CompleteIfSufficient` can scope the policy read with it.

## D2 — `LoginPolicy` takes an org id and refuses an empty one

```go
func (c *Client) LoginPolicyForOrg(ctx context.Context, orgID string) (LoginPolicy, error)
```

An empty `orgID` returns `ErrUnavailable` **before any HTTP request is made**.
It does not fall back to an unscoped read. That fallback is the present bug,
and it would reproduce it for exactly the users hardest to notice — the ones
whose session response shape drifted.

`ErrUnavailable` rather than a new sentinel, for the same reason
`LoginPolicy` already uses it for an unrecognizable body: from the caller's
point of view "Zitadel did not tell Helivanta whether MFA is required" is one
situation, and all of its causes must reach the same fail-closed branch —
`CompleteIfSufficient`'s existing `OutcomeHandoff` + `slog.Warn`.

This is why the org id is **not** validated at the session read: one control
point, at the call that would otherwise be wrong, rather than a check
duplicated in every reader of the session response.

## D3 — The pre-credential display read is a separate, explicitly named method

`loginui.go`'s `AuthRequest` handler (`GET /v1/auth/login/request/:id`) also
reads the login policy, to tell the form whether to advertise an MFA step
(spec D5 of the login-client design). That call happens **before the user has
typed anything**, so no user org exists to scope it to — not "we forgot to
pass it", but "there is no correct value at that moment".

It keeps an unscoped read, under a name that cannot be mistaken for the
enforcer's:

```go
func (c *Client) InstanceLoginPolicyForDisplay(ctx context.Context) (LoginPolicy, error)
```

Both methods share one unexported implementation, so the body-parsing and
rename-guard behaviour cannot drift between them.

**State the limit plainly.** On a multi-org instance this display value can be
wrong — the form may advertise "no MFA" to a user whose org forces it. That is
a cosmetic wrong answer *followed by a correctly enforced handoff*, and it is
exactly today's behaviour, so this change makes it no worse. It is not
silently accepted: it is documented on the method and filed as a follow-up
(the form would have to re-read the policy after the login name is known,
which is a login-form flow change, not a client change).

**Enforced structurally**, not by convention: an archtest asserts
`InstanceLoginPolicyForDisplay` is never referenced from `sufficiency.go`,
alongside the existing `TestFinalizeCallSiteIsUnique` and the
`sufficient{}`-construction test. A future contributor who reaches for the
unscoped read inside the enforcer fails CI, not review.

## D4 — `do` gains typed request options, not a header map

`do` takes no per-request headers today, and this is the first caller that
must scope a request — it will not be the last (org-scoped user reads,
org-scoped policy writes).

```go
type requestOptions struct{ orgID string }
type requestOption func(*requestOptions)
func withOrgID(orgID string) requestOption

func (c *Client) do(ctx context.Context, method, path string, body, out any,
	notFound error, opts ...requestOption) error
```

`do` sets `x-zitadel-orgid` **itself** from `opts.orgID`; an option cannot
reach the `http.Request`. A `func(*http.Request)` option would have let a
future option overwrite `Authorization` — this shape makes that
unrepresentable rather than merely discouraged.

Variadic so the seven existing call sites are unchanged, which keeps this
diff about the security fix rather than about churn.

## D5 — The proof is a live login by a user in a second org

The test that matters is not "the header is set". It is: **a user in an org
that forces MFA is not completed by a policy read against a different org.**

`loginui_integration_test.go` gains
`TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting`,
alongside the existing `ForceMFAPolicy` / `ForceMFALocalOnly` pair it mirrors:

1. Create a second org (seed PAT, `POST /management/v1/orgs`).
2. Create a human user **in that org** (`x-zitadel-orgid` on the import call).
3. Set `forceMfa: true` on **that org's** login policy — leaving the login
   client's own org at its default (`forceMfa: false`).
4. Sign that user in through the real `POST /v1/auth/login/password`.
5. Assert `handoff_url`, and assert `callback_url` is **absent**.

On the current code step 5 fails with a `callback_url`: the policy read
resolves to the login client's org, sees MFA off, and finalizes a
password-only session for a user whose org forbids exactly that. That failure
**is** the bug, reproduced end to end, and it is required to be observed
before the fix lands.

Cleanup deletes the org and verifies the delete, matching
`resetOrgLoginPolicy`/`deleteAndVerifyUser`'s existing "verify the restore,
never trust the 200" discipline in this file.

**Two live unknowns this test must answer rather than assume**, because a
wrong guess here is a test that passes for the wrong reason:

- Whether the **login-client PAT** (not the seed PAT the issue used) is
  permitted to read another org's policy with `x-zitadel-orgid`. If it is
  refused, the read errors → `ErrUnavailable` → handoff: still fail-closed,
  still not a bypass, but the test would then be passing on the error path
  rather than on the policy value. The test asserts `assertLoginSucceeds`
  against the same second-org user with `forceMfa: false` to separate those
  two — a completed login proves the scoped read genuinely resolved.
- Whether Zitadel's project settings let a user from a second org sign in to
  the Helivanta app at all. If a project org-grant is required, the test
  provisions it explicitly.

Unit coverage in `loginclient` alongside it: an empty org id errors without
issuing a request; a non-empty org id puts the exact header on the wire; the
existing fake-Zitadel session fixtures gain `organizationId` so they match
what the real instance actually sends.

## Out of scope

- **Multi-org support generally.** This makes the existing single-org
  assumption safe to violate. It adds no org selection, discovery or routing.
- **#856 and #901.** Same file, separately tracked.
- **Making the display read org-aware** (D3's residual) — needs a login-form
  flow change; filed as a follow-up, #917.
