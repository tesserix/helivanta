# "Sign in again" starts a new sign-in

**Issue:** [#1820](https://github.com/tesserix/helivanta/issues/1820), slice 2. Slice 1 is [sign-in never routes by memory](2026-11-24-no-sign-in-routing-by-memory-design.md). Part of [#1767](https://github.com/tesserix/helivanta/issues/1767).
**Refines:** #1820's proposal, which was written before its paths were traced. See "Not in this slice".

## Problem

Production retest, 2026-11-23:

1. Workspace A was signed in.
2. In another tab the tester signed in to workspace B. The password and authenticator setup succeeded, but the callback failed because the started request had expired.
3. The callback's **Sign in again** link went to plain `/login`. With no `workspace` in the URL, `/login` first renews the live session (`SessionLanding`). A's session renewed, and the tester landed on A's dashboard. B was never offered.

The person had just asked to sign in again, and got a different account without being asked anything.

## Traced paths

| Entry                                     | Live session for A | Today                                   |
| ----------------------------------------- | ------------------ | --------------------------------------- |
| `/login` or `/login?start=1` (home page)  | yes                | Continues A (#1750, deliberate)         |
| `/login?workspace=B…`                     | yes                | B's password card. A is never continued |
| Callback error → **Sign in again**        | yes                | Continues A: **the defect**             |
| Session-check failure → **Sign in again** | n/a                | Already starts a sign-in                |

A workspace link already skips the session check (`LoginPageContent` renders `SessionLanding` only when the URL names no workspace). So #1820's "pinned request for B with A live" case does not continue A. The only silent continue the report reached is the callback link.

## Decisions

### D1 — The callback's link names what it was signing in to

When the failure happens after the OIDC state was read (the exchange stage) and that state names a workspace, **Sign in again** links to `/login?workspace=<tenant>&start=1`. The tenant comes from the protected OIDC state of this same flow, never from browser memory, and is checked with `workspaceSigninScope`. If the check fails, the link falls back to D2.

### D2 — Otherwise it starts a new unscoped sign-in

Any other failure, including an expired request whose state is gone, links to `/login?again=1`. `/login?again=1` skips the session check and starts the unscoped card at once. The person enters an email and password, and the API resolves the account (#1751). A's session is never consulted, so it can never answer. A successful sign-in replaces the browser's session, as any sign-in does. `again` is removed from the address once the start begins, like `start`.

The sign-out and idle marks still win (#847): with either mark present, the landing shows the button instead of starting.

### D3 — Plain `/login` still continues a live session

Home page → **Sign in** with a live session still opens that session's dashboard without a challenge (#1750). The dashboard names the workspace, and **Switch account** is one click. Changing this would add a screen to every return visit.

## Not in this slice

- **A card for "You're signed in to A"** on workspace links, as #1820 proposed. Workspace links never continue A (see the table), so the card would only announce that A's browser session is about to be replaced. That is a separate question about sign-in replacing a session without signing it out, and is left for #1820's discussion.
- **Recovering the workspace after the state expired.** The state store holds it, and it is gone. D2 asks for the email instead.

## Verification

Vitest:

- The callback's link is `/login?again=1` for an expired request, for a missing `id_token` and for an exchange refusal with no workspace in the state.
- The link is `/login?workspace=<tenant>&start=1` for an exchange refusal whose state names a workspace.
- `/login?again=1` makes no renewal request, starts the unscoped redirect (no organisation scope, purpose `email-login`) and removes `again` from the address.
- `/login?again=1` with the sign-out mark shows the button and starts nothing.

e2e (`session-entry.spec.ts`): signed in, `/login?again=1` reaches the email card with no renewal request, and the session cookie is untouched until a password is typed.

Each new assertion is shown to fail against the current code before the change.
