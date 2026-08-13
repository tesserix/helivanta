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
