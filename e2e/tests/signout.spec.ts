import { test, expect } from "@playwright/test";
import { login } from "./support/login";

// The assertion that would have caught the original bug (#781): sign-out
// used to only clear the transport cookie, leaving the Firebase SDK's
// session alive in IndexedDB. A fresh ID token minted from that stale
// session could rebuild a working `hms_session` cookie from the browser
// console — on a shared ward terminal, the previous user's session was
// recoverable after they "logged out".
//
// This deliberately does NOT assert "the cookie was cleared" — that is
// exactly the proxy assertion that let the defect exist
// (docs/standards/engineering-principles.md §5). It reconstructs the
// actual attack instead: mint a fresh ID token from whatever Firebase
// session survives sign-out, and see whether the API still answers it.
test("a signed-out session cannot be reconstructed from the browser", async ({ page }) => {
  await login(page);
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  // Both the rail icon and the header text trigger the identical
  // sign-out sequence (packages/ui/src/hms-shell.tsx). The header one
  // (last in DOM order) is used here — the icon-only rail button sits at
  // the bottom-left, where Next.js's dev-mode tooling indicator also
  // renders, and can intercept the click in local/dev-server runs.
  await page.getByRole("button", { name: "Sign out" }).last().click();
  await expect(page).toHaveURL(/\/login/);

  // The attack: the previous user's Firebase SDK session used to survive
  // in IndexedDB, so this rebuilt a working session from the console.
  //
  // `getAuth()` from "firebase/auth" isn't reachable here: page.evaluate
  // runs as a plain script with no bundler, so a bare specifier import
  // has nothing to resolve against. `window.__hmsFirebaseAuth`
  // (apps/shell/lib/firebase.ts, dev/test builds only) hands back the
  // exact Auth instance the app itself uses, once /login has mounted and
  // constructed it (app/login/page.tsx).
  await page.waitForFunction(() => Boolean(window.__hmsFirebaseAuth));
  const restored = await page.evaluate(async () => {
    const auth = window.__hmsFirebaseAuth!;
    // The SDK rehydrates its session from IndexedDB asynchronously; a
    // fresh Auth instance answers `currentUser` with null until that
    // finishes, so this waits for the real answer rather than racing it.
    await auth.authStateReady();
    const user = auth.currentUser;
    if (!user) return "no-user";
    const idToken = await user.getIdToken(true);
    const res = await fetch("/api/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ idToken }),
    });
    if (!res.ok) return "session-refused";
    const check = await fetch("/api/v1/iam/me/permissions");
    return check.ok ? "RESTORED" : "api-refused";
  });

  expect(restored).not.toBe("RESTORED");
});

// The test above is necessary but NOT sufficient, and the gap is worth
// stating: it passes the moment `currentUser` is null, which client-side
// signOut() guarantees on its own. Comment out the server-side call in
// app/logout/route.ts and it still passes — so it proves the client half
// and silently assumes the server half.
//
// That assumption is the whole feature. Clearing browser state protects
// the shared workstation; only the watermark protects a token that has
// already left the browser — copied out of devtools, captured from a
// proxy, or held by a native client. This test holds a valid, unexpired
// ID token across the sign-out and replays it, so the only thing that
// can refuse it is the server-side revocation watermark
// (docs/superpowers/specs/2026-08-13-credential-revocation-design.md D1/D2).
test("a token captured before sign-out is refused afterwards", async ({ page }) => {
  await login(page);
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  await page.waitForFunction(() => Boolean(window.__hmsFirebaseAuth));
  const captured = await page.evaluate(async () => {
    const auth = window.__hmsFirebaseAuth!;
    await auth.authStateReady();
    return auth.currentUser ? auth.currentUser.getIdToken() : null;
  });
  expect(captured, "precondition: a live ID token was captured before sign-out").toBeTruthy();

  // Precondition: the captured token works right now. Without this, a
  // token that was never valid would make the post-sign-out refusal
  // meaningless — the test would pass for the wrong reason.
  const before = await page.evaluate(async (token) => {
    const res = await fetch("/api/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ idToken: token }),
    });
    if (!res.ok) return "session-refused";
    return (await fetch("/api/v1/iam/me/permissions")).ok ? "accepted" : "api-refused";
  }, captured);
  expect(before, "the captured token must be accepted before sign-out").toBe("accepted");

  await page.getByRole("button", { name: "Sign out" }).last().click();
  await expect(page).toHaveURL(/\/login/);

  // The same token, still cryptographically valid and unexpired, replayed
  // after sign-out. Its auth_time predates the watermark the sign-out
  // wrote, so the API must refuse it. Browser state is irrelevant here:
  // the token is supplied directly.
  const after = await page.evaluate(async (token) => {
    const res = await fetch("/api/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ idToken: token }),
    });
    if (!res.ok) return "session-refused";
    return (await fetch("/api/v1/iam/me/permissions")).ok ? "STILL-ACCEPTED" : "api-refused";
  }, captured);

  expect(
    after,
    "a token captured before sign-out was still accepted afterwards; the server-side watermark is not being written, and clearing browser state is the only thing protecting the session",
  ).not.toBe("STILL-ACCEPTED");
});
