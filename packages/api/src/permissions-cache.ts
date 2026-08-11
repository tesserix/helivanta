// Presentation-only cache of the caller's permission set. Cross-zone
// navigation is a hard navigation between separate Next apps, so every
// zone load starts with an empty query cache and the nav can only render
// the Dashboard entry until /iam/me/permissions returns. Persisting the
// last known set lets the nav paint at hydration instead.
//
// This hides doors that will not open — the API is the enforcement point.
// A tampered entry can only make the browser draw a link the backend then
// refuses, never grant access.

export const PERMISSIONS_CACHE_KEY = "hms.permissions.v1";

// A session is not useful much beyond a shift, and an entry older than
// this is more likely to mislead than to help, so it is ignored rather
// than painted.
export const PERMISSIONS_CACHE_TTL_MS = 24 * 60 * 60 * 1000;

export type PermissionsCacheEntry = {
  subject: string;
  tenantId: string;
  permissions: string[];
  storedAt: number;
};

function isEntry(value: unknown): value is PermissionsCacheEntry {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.subject === "string" &&
    typeof candidate.tenantId === "string" &&
    typeof candidate.storedAt === "number" &&
    Array.isArray(candidate.permissions) &&
    candidate.permissions.every((p) => typeof p === "string")
  );
}

// Every accessor swallows storage failures: private-mode Safari, a
// disabled-storage policy, or a full quota must degrade to "no cache",
// never to an error the user sees.
export function readPermissionsCache(): PermissionsCacheEntry | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(PERMISSIONS_CACHE_KEY);
    if (raw === null) return null;
    const parsed: unknown = JSON.parse(raw);
    if (!isEntry(parsed) || Date.now() - parsed.storedAt > PERMISSIONS_CACHE_TTL_MS) {
      clearPermissionsCache();
      return null;
    }
    return parsed;
  } catch {
    clearPermissionsCache();
    return null;
  }
}

export function writePermissionsCache(entry: PermissionsCacheEntry): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(PERMISSIONS_CACHE_KEY, JSON.stringify(entry));
  } catch {
    // No cache is a supported state — the nav just waits for the query.
  }
}

export function clearPermissionsCache(): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.removeItem(PERMISSIONS_CACHE_KEY);
  } catch {
    // Nothing to do: the entry is unreadable anyway.
  }
}
