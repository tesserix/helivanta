# Sign-in never routes by memory

**Issue:** [#1820](https://github.com/tesserix/helivanta/issues/1820), part of [#1767](https://github.com/tesserix/helivanta/issues/1767).
**Supersedes:** D1 and D2 of [sign-in continuation](2026-11-19-sign-in-continuation-design.md), the remembered-workspace paragraph of D7 in [single-screen sign-in](2026-11-19-single-screen-sign-in-design.md), and the routing-hint paragraph of [session lifetime and remembered login](../../architecture/session-lifetime-and-remembered-login.md).

## Problem

Found in the production retest on 2026-11-23. The tester went home page → **Sign in**, entered the qa-auth3 owner's email and password, and got "Email or password is incorrect" twice. The API logged `auth.login.password_failed` with outcome `user_not_found` both times, so the password was never checked.

`/login` with no `workspace` parameter starts a request scoped to `helivanta.login.workspace.v1`, the workspace this browser last _started_ a sign-in to. A workspace link opened minutes earlier for a different account (the qa-auth2 pharmacy) had written it. The card said "Helivanta", because an unbranded workspace's public branding name is "Helivanta", so nothing on screen showed which workspace it was signing in to. The refusal stays generic on purpose (it must not reveal whether an account exists), so the tester could not tell what was wrong.

The same hint does this on a shared ward terminal: the next person is silently sent to the previous person's workspace.

The hint was added (#1722, #1750) to save the emailed discovery code. #1751 removed that cost: an unscoped card takes email and password and the API resolves the account, with the chooser when there are several. The hint now saves one server-side lookup and costs a wrong-workspace refusal.

The same pinning also hides the remembered username (#1819): a pinned card offers only a name remembered for that workspace, and a name saved before #1819 has no workspace attached.

## Decisions

### D1 — Only the URL pins a workspace

`/login` starts a scoped request only when the URL names the workspace (`?workspace=`), or when the signup hand-off or the chooser selects one in this same flow. Otherwise it starts the unscoped request (#1751 D1). Nothing stored in the browser decides which workspace a sign-in goes to.

### D2 — Delete the remembered workspace

`rememberWorkspace`, `readRememberedWorkspace`, `forgetWorkspace` and `subscribeWorkspace` are removed with their four write sites (login start, discovery choice, session exchange, tenant picker) and discovery's **Sign in to your previous account** button. `workspaceSigninScope` stays. A value left in `helivanta.login.workspace.v1` is never read again. It holds only a tenant UUID and expires within 30 days, so no migration clears it.

Branding retention across the identity-provider round trip (`retainLoginBrandingTenant`, per tab in `sessionStorage`) is unchanged. It is display-only and is set only by the flow in that tab.

### D3 — A pinned card can be left

A card pinned by a workspace link shows **Not your workspace? Sign in with your email** under the form. It starts the unscoped request (D1). The refusal copy is unchanged.

### D4 — Remembered username

Unchanged from #1819: the unscoped card offers the remembered name whatever workspace it was saved for, and a pinned card offers it only when it was saved for that workspace. With D1 the home-page card is unscoped, so the remembered name returns there.

## Not in this slice

- **The workspace's name on a pinned card.** The public branding endpoint answers "Helivanta" for an unbranded workspace, and the registered tenant name is not public before sign-in. Showing it is a disclosure decision (anyone holding a workspace UUID would learn the organisation's name) and a data-ownership one (the workspace module does not own the tenant name). It needs its own decision before code.
- **A live session for another workspace** (#1820's original report): `/login` still continues a live session when no workspace is named. The "You're signed in to A" card with **Continue** and **Switch** is the next slice under #1820.

## Verification

Vitest (`apps/shell/app/login/login.test.tsx`, `components/account-discovery.test.tsx`):

- A home-page start with an old `helivanta.login.workspace.v1` value in storage starts the unscoped request (no organisation scope, purpose `email-login`).
- A `?workspace=` start is scoped to that workspace and writes nothing to local storage.
- **Not your workspace?** on a pinned card starts the unscoped request.
- Discovery no longer offers **Sign in to your previous account**. Choosing an account starts a sign-in scoped to that account.
- The unscoped card fills in a name remembered for any workspace.

Each new assertion is shown to fail against the current code before the change.

e2e: `smoke.spec.ts` selectors keep working. Gates: `pnpm turbo lint type-check test build format:check`.
