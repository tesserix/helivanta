import { signOut } from "firebase/auth";
import { firebaseAuth } from "./firebase";

/**
 * The full client-side sign-out sequence, in order:
 *
 *  1. `POST /logout` — same-origin checked (`app/logout/route.ts`), which
 *     revokes the session server-side (writes the revocation watermark and
 *     revokes GIP refresh tokens, `POST /v1/iam/me/sign-out`) and only
 *     THEN clears the transport cookie. Best-effort here: its failure must
 *     not block step 2, or a clinician on a shared workstation could never
 *     leave the terminal signed in just because the API was briefly
 *     unreachable.
 *  2. Firebase `signOut()` — clears the SDK session cached in IndexedDB.
 *     Without this the original defect (#781) persists even after step 1:
 *     a stale `currentUser` can still mint a fresh ID token via
 *     `getIdToken(user, true)` from the browser console, because a
 *     refreshed token carries the same `auth_time` and the ID token itself
 *     is never individually invalidated — only the SDK's cached session
 *     stops the mint from being attempted at all.
 *
 * `HmsShell` (`@hms/ui`) cannot own this itself: it is rendered by every
 * zone app, and only the shell app carries the Firebase client config
 * (`apps/shell/lib/firebase.ts`) — the same constraint already documented
 * on `TenantPicker`. Only the shell's own dashboard
 * (`apps/shell/app/page.tsx`) supplies this as `HmsShell`'s `onSignOut`
 * prop; other zones fall back to `HmsShell`'s built-in POST-only default,
 * which still revokes the session server-side but cannot reach IndexedDB.
 */
export async function signOutEverywhere(): Promise<void> {
  try {
    await fetch("/logout", { method: "POST" });
  } catch {
    // Network failure: proceed to clear the client-side Firebase session
    // regardless — see the module doc above.
  }
  await signOut(firebaseAuth());
}
