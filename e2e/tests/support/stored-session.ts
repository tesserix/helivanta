import type { Page } from "@playwright/test";

// readStoredIdToken returns the Zitadel ID token oidc-client-ts has stored for
// the current Helivanta page, read straight out of sessionStorage — or null if
// none is stored.
//
// It replaces `window.__hmsUserManager.getUser()`, a hook apps/shell exposed
// only in non-production builds, which stopped existing once e2e began running
// against production builds (#976). Reading storage directly is not a
// weaker substitute: getUser() only ever read this same entry, and storage is
// what script running in the page — the attacker signout.spec.ts models —
// actually reaches.
//
// The key is oidc-client-ts's own: `oidc.` (WebStorageStateStore's default
// prefix) + `user:<authority>:<client_id>` (UserManager._userStoreKey,
// oidc-client-ts 3.5). Matched by prefix rather than rebuilt from the issuer
// and client id, so the helper does not need to know which of the three e2e
// shells it is on. More than one stored user would mean two OIDC clients
// sharing one origin's storage, which no e2e stack does; it is refused rather
// than silently picking one.
export async function readStoredIdToken(page: Page): Promise<string | null> {
  const tokens = await page.evaluate(() => {
    const found: string[] = [];
    for (let i = 0; i < window.sessionStorage.length; i++) {
      const key = window.sessionStorage.key(i);
      if (!key?.startsWith("oidc.user:")) continue;
      const raw = window.sessionStorage.getItem(key);
      if (!raw) continue;
      try {
        const parsed = JSON.parse(raw) as { id_token?: unknown };
        if (typeof parsed.id_token === "string" && parsed.id_token) found.push(parsed.id_token);
      } catch {
        // Not oidc-client-ts's JSON; not a stored user.
      }
    }
    return found;
  });
  if (tokens.length > 1) {
    throw new Error(
      `readStoredIdToken: ${tokens.length} stored OIDC users on one origin — expected at most one`,
    );
  }
  return tokens[0] ?? null;
}
