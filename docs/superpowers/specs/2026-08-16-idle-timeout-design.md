# Idle timeout: a walked-away ward terminal must stop being signed in

**Issue:** [#848](https://github.com/tesserix/hms/issues/848)
**Builds on:** `2026-08-15-zitadel-auth-design.md` (D2 session claims, D4a renewal),
`2026-08-16-hms-login-client-design.md` (#854 — HMS now owns the login page)

A clinician is called away mid-shift and does not sign out. The next person at
the terminal is simply *in*, as them. Nothing today prevents it: the HMS session
cookie is still valid, so `/login` is never reached and no `prompt` value can
intervene. #847 fixed the explicit sign-in paths and cannot cover this.

The cost is not inconvenience. Every action is attributed to whoever the session
says they are, so an inherited session puts the wrong name on clinical actions —
and **[#54](https://github.com/tesserix/hms/issues/54)'s audit trail is only
trustworthy if this lands first.** In an accreditation review that is an
access-control finding; in a patient-safety investigation it makes the record
actively misleading rather than merely incomplete.

---

## D1 — The window is 15 minutes, uniform

Long enough to read a chart, take a call, or talk to a patient without being
interrupted. Short enough that a walk-away is caught well inside a shift.

Uniform rather than per-zone or per-role. A per-zone window is more faithful to
real workflow, but it creates a policy surface where every value must be
defended, and the difference between 10 and 20 minutes on a pharmacy screen is
not the thing standing between us and an inherited session. If a ward asks for
something tighter later, this becomes configuration — the mechanism below does
not change.

It happens to equal `SESSION_TTL`, which is convenient but **not** a coupling:
the two are independent clocks and D3 depends on them staying independent.

## D2 — Two clocks, and the browser may only move one of them

There are now two independent expiries on an HMS session:

| Clock | Meaning | Moved by |
|---|---|---|
| `exp` | this token's own lifetime (15m) | silent renewal, every 5m (D4a) |
| `idle_deadline` | last human interaction + 15m | **only** an explicit activity call |

`idle_deadline` is a new claim in the HMS session token, carried like `auth_time`
is: **set once, carried forward across every re-mint, never quietly reset to
`now`.** `authn.Middleware` refuses a token past its `idle_deadline` exactly as
it already refuses one past `exp` or behind the #781 revocation watermark.

**That is what makes this server-enforced.** A killed tab, a suspended laptop, a
tampered client, or a direct `curl` with a stolen cookie all fail closed, because
the deadline travels inside the signed token rather than living in a timer the
browser owns.

The browser is nonetheless the only thing that can see a human, so it supplies
the signal — but it can only ever move the deadline by calling one endpoint that
re-mints, and an untouched tab never calls it. The threat model here is a
bystander at an unattended terminal, not a clinician forging their own
extension: someone who wants to keep their own session alive can simply move the
mouse.

## D3 — Silent renewal must not count as activity

**The single most important rule in this spec, and the one most likely to be got
wrong, because everything looks like it works when it is broken.**

D4a's renewal runs every 5 minutes against Zitadel to re-establish that the
account is still good. It re-mints the HMS session. If that re-mint set
`idle_deadline = now + 15m`, an untouched tab would renew itself forever and the
timeout would never fire — while the feature appeared to be implemented.

So renewal **carries `idle_deadline` through unchanged**, exactly as it already
carries `auth_time`. Only `POST /v1/auth/session/activity` moves it.

Enforce this structurally rather than by convention: the mint path takes the
deadline as an explicit parameter with no default, so a caller must *say* what
it is doing. A test must exist that runs several renewals against an untouched
session and asserts the deadline did not move — and it must be observed failing
against an implementation that resets it.

## D4 — Activity is a deliberate signal, debounced, and shared across tabs

New endpoint, in `iam` beside the other pre-session auth routes:

```
POST /v1/auth/session/activity
  → 200 {"idle_deadline": "<RFC3339>"}     re-mints with idle_deadline = now + 15m
  → 401                                     session already expired or idle-expired
```

It requires a currently-valid session, carries `sub`, `tenant_id` and `auth_time`
through untouched, and is rate limited on the existing limiter like every other
unauthenticated-adjacent route.

The browser calls it on genuine interaction — `pointerdown`, `keydown`, and a
real scroll — **debounced to at most once per 60 seconds**, so an active
clinician produces one cheap request a minute rather than one per keystroke.
Mouse movement alone does not count: a cleaner's sleeve on a desk is not a
clinician.

**The response body returns the new deadline** because the session cookie is
`httpOnly` and carries nothing the client can read. That is how the browser knows
when to warn (D5).

**Tabs share one timer.** Every HMS app is served from one origin, so interaction
is broadcast over a `BroadcastChannel`; a clinician reading in one tab keeps
their other tabs alive, and only one activity call is made for all of them. Where
`BroadcastChannel` is unavailable, each tab falls back to its own timer — more
requests, same behaviour, never a wrong sign-out.

## D5 — Warn at two minutes, and make the warning the thing that saves the work

A modal at `idle_deadline - 2m` with a live countdown and a **Stay signed in**
button. Dismissing it, or any interaction, calls the activity endpoint and the
modal closes.

Modal rather than a toast: a toast on a terminal nobody is watching is invisible,
and it is easy to miss on glancing back — which is exactly the moment the warning
exists for. Unsaved clinical work is expensive to lose.

Before showing it, the client re-checks the deadline it holds. Another tab may
have extended the session, in which case the modal must not appear at all.

## D6 — What ends: this browser, not this person

On expiry the client tears down **this browser's** session:

1. clear the HMS session (the existing `/logout` route's cookie clearing), and
2. end the Zitadel SSO session for this browser (`endZitadelSession`).

Then land on `/login` with wording that says the session ended because of
inactivity — distinguishable from a deliberate sign-out (which #850 already
distinguishes from "never signed in"), because telling someone they signed out
when they did not is untrue and, on a shared terminal, misleading.

**Deliberately NOT subject-wide revocation.** That is sign-out's semantics
(#781, by design global across every device). Applying it here would sign a
clinician out of their office desktop and phone because they left a ward terminal
idle — surprising, and it would make carrying a second device unusable during a
shift.

Ending the Zitadel session matters because otherwise the next authorization
completes silently against a live IdP session, reintroducing the problem one
layer down — the same trap #847 documents. Cost, stated: Zitadel's SSO cookie is
instance-wide, so this also signs that browser out of other Tesserix products. On
a ward terminal that is desirable.

**This got cheaper with #854.** Re-authentication after a timeout now lands on
HMS's own login form rather than a redirect to a hosted page, so "your session
ended, sign in again" is one page we control.

## D7 — Every app, not just the shell

Interaction tracking and the warning modal live in **`@hms/ui`**, so
`apps/medicore`, `apps/pharmacy` and `apps/lab` get them by rendering `HmsShell`.

This is not optional polish. A clinician working inside `/medicore` for twenty
minutes is *active*; if only the shell tracked interaction, they would be signed
out mid-consultation — failing the issue's third acceptance criterion while
appearing to implement it. `endZitadelSession` learned this exact lesson the hard
way: sign-out was shell-only, and the majority path (signing out from a zone
page) silently did the wrong thing.

The activity endpoint is HMS-only and needs no Zitadel client config, so unlike
renewal there is no reason for it to stay central.

---

## Errors and failure handling

- **Activity call fails (network, 5xx):** do not sign the user out. Retry on the
  next interaction. A transient failure must never end a clinical session; the
  server-side deadline remains the backstop.
- **Activity call returns 401:** the session is already gone. Run D6's teardown
  immediately rather than waiting for a timer.
- **Clock skew:** the server is authoritative. The client's countdown is a
  display derived from the server's returned deadline, never its own arithmetic
  on a locally-stored timestamp.
- **`BroadcastChannel` unavailable:** per-tab timers, as in D4.

## Testing

- **Go:** a token past `idle_deadline` is refused with 401; one inside it passes;
  the deadline survives a tenant switch and a renewal unchanged.
- **The D3 regression test, which is the point of the spec:** several renewals
  against an untouched session leave the deadline where it was. It must be
  observed failing against an implementation that resets it — a passing test here
  that cannot fail is worse than none, because D3 is invisible when broken.
- **Vitest:** the debounce fires once per window under a burst; the warning
  appears at the right offset; dismissing it extends; another tab's activity
  suppresses it; a failed activity call does NOT sign the user out.
- **E2E:** with a shortened window, a session left untouched ends and returns to
  `/login`, and the next sign-in requires credentials — asserted against the API,
  not the dashboard heading, since the dashboard renders for a session the API
  refuses.

## Out of scope

- OS-level screen lock; this covers the application session only.
- Changing sign-out (#781) or the explicit sign-in paths (#847).
- Per-zone or per-role windows (D1).
- Re-authenticating *in place* (unlocking without a full sign-in) — a real future
  refinement, but it needs a credential prompt inside the app, and that decision
  belongs with #41's MFA work rather than here.
