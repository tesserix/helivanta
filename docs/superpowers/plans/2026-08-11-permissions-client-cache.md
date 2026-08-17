# Client-side permissions cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cache the caller's permission set in localStorage so the zone rail and page panel render their full permitted entries as soon as the page hydrates after a hard cross-zone navigation, instead of waiting on `/iam/me/permissions`.

**Architecture:** `GET /iam/me/permissions` additionally returns the caller's `subject` and `tenant_id`. A small pure module in `@helivanta/api` owns one localStorage key holding `{subject, tenantId, permissions, storedAt}` with a 24-hour TTL. `usePermissions` reads that entry after mount and feeds it to the existing React Query query as `placeholderData`, so cached permissions render while the real request is in flight; every fresh response overwrites the entry. The cache is cleared explicitly at login, logout, and tenant switch.

**Tech Stack:** Go 1.26 + Gin (backend), React 19 + TanStack Query v5 (frontend), Vitest + Testing Library (frontend tests), Go `testing` + testify (backend tests).

## Global Constraints

- Issue #760. Branch `feat/760-permissions-client-cache` (already checked out). PR body must include `Closes #760`.
- Vertical slice: backend, frontend, and tests land together in one PR.
- Data fetching goes through `useApiQuery`/`useApiMutation` from `@helivanta/api` only — no raw `fetch` in components (the shell login page and tenant picker session POSTs are the pre-existing sanctioned exception, see `docs/standards/frontend.md` section 3).
- Permission gating stays convenience-only: the API is the enforcement layer. Never present this cache as a security boundary.
- Styling: design tokens only, no hardcoded colors. No new npm dependencies.
- Every new/changed panel component needs a Vitest test using `renderWithProviders` from `@helivanta/api/testing`.
- Backend: `respond.*` helpers for every response; `slog` only; wrap errors with `%w`.
- Commit messages: single line, conventional commits, no signatures.
- Final gates before done: `pnpm turbo lint type-check test build` green; `make lint-go` clean; `cd backend && go test -race ./...` green; `cd backend && ./scripts/coverage-gate.sh` green (70% floor).

## Known limitation (accepted)

Zone apps prerender the shell on the server, where localStorage does not exist, so the server HTML still contains the Dashboard-only nav. The cache cannot change that first server paint; it removes the network round-trip, so the full nav appears at hydration rather than after `/iam/me/permissions` returns. Reading the cache in a layout effect (not during render) is deliberate — reading it during the hydration render would make the client tree disagree with the server HTML and trigger a React hydration mismatch.

## File Structure

| File | Responsibility |
|---|---|
| `backend/internal/modules/iam/me.go` (modify) | `/me/permissions` returns `subject` + `tenant_id` alongside `data` |
| `backend/internal/modules/iam/me_test.go` (modify) | Asserts the identity fields |
| `packages/api/src/permissions-cache.ts` (create) | Pure localStorage read/write/clear with TTL + corruption handling |
| `packages/api/src/permissions-cache.test.ts` (create) | Unit tests for the cache module |
| `packages/api/src/hooks.ts` (modify) | `useApiQuery` accepts `placeholderData` |
| `packages/api/src/permissions.tsx` (modify) | Hydrates from cache, writes through on fresh data |
| `packages/api/src/permissions.test.tsx` (modify) | Warm-cache paint, write-through, revocation convergence |
| `packages/api/src/index.ts` (modify) | Exports `clearPermissionsCache` |
| `packages/ui/src/hms-shell.tsx` (modify) | Logout links clear the cache |
| `packages/ui/src/hms-shell.test.tsx` (create) | Logout click clears the cache |
| `apps/shell/components/tenant-picker.tsx` (modify) | Tenant switch clears the cache |
| `apps/shell/components/tenant-picker.test.tsx` (modify) | Switch clears the cache |
| `apps/shell/app/login/page.tsx` (modify) | Successful sign-in clears the cache |

---

### Task 1: Backend returns caller identity with permissions

**Files:**
- Modify: `backend/internal/modules/iam/me.go:58-65`
- Test: `backend/internal/modules/iam/me_test.go`

**Interfaces:**
- Consumes: `authn.PrincipalFrom(c)` (already imported in `me.go`), `authz.PermissionsFrom(c)`.
- Produces: `GET /v1/iam/me/permissions` response body `{"data": []string, "subject": string, "tenant_id": string}`. Task 3 consumes `subject` and `tenant_id`.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/modules/iam/me_test.go`:

```go
// The client-side permissions cache (packages/api/src/permissions-cache.ts)
// keys its entry on who the permissions belong to, so a cached set is
// never shown to a different user or tenant on the same browser. The
// client cannot read that identity from the httpOnly session cookie, so
// this endpoint is where it comes from.
func TestMePermissionsReturnsCallerIdentity(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"doc": testutil.TenantA},
		map[string][]authz.Permission{"doc": {"medicore.visit.read"}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "doc", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data     []string `json:"data"`
		Subject  string   `json:"subject"`
		TenantID string   `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, []string{"medicore.visit.read"}, body.Data)
	require.Equal(t, "doc", body.Subject)
	require.Equal(t, testutil.TenantA, body.TenantID)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/iam/ -run TestMePermissionsReturnsCallerIdentity -v`
Expected: FAIL — `body.Subject` is `""`, `body.TenantID` is `""`.

- [ ] **Step 3: Write minimal implementation**

In `backend/internal/modules/iam/me.go`, replace the `/me/permissions` handler:

```go
	g.GET("/me/permissions", authz.Public, func(c *gin.Context) {
		set, ok := authz.PermissionsFrom(c)
		if !ok {
			respond.Internal(c, "authorization not initialized")
			return
		}
		// subject and tenant_id travel with the permission set so the
		// client-side cache in @helivanta/api can tell whose permissions it
		// holds. The session cookie is httpOnly, so this response is the
		// only place the browser can learn that identity — without it a
		// cached set could be painted for the wrong user or tenant after
		// a session change on a shared terminal.
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		respond.OK(c, gin.H{
			"data":      set.Sorted(),
			"subject":   p.Subject,
			"tenant_id": p.TenantID,
		})
	})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/iam/ -v`
Expected: PASS, including the pre-existing `TestMePermissionsReturnsResolvedSetSorted` and `TestMePermissionsIsEmptyArrayNotNullForNonMember`.

- [ ] **Step 5: Lint**

Run: `make lint-go`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/iam/me.go backend/internal/modules/iam/me_test.go
git commit -m "feat(iam): return caller identity with the permission set (#760)"
```

---

### Task 2: Permissions cache module

**Files:**
- Create: `packages/api/src/permissions-cache.ts`
- Test: `packages/api/src/permissions-cache.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type PermissionsCacheEntry = { subject: string; tenantId: string; permissions: string[]; storedAt: number }`
  - `readPermissionsCache(): PermissionsCacheEntry | null`
  - `writePermissionsCache(entry: PermissionsCacheEntry): void`
  - `clearPermissionsCache(): void`
  - `PERMISSIONS_CACHE_KEY: string` (value `"hms.permissions.v1"`) — tests and Task 4 use it.
  - `PERMISSIONS_CACHE_TTL_MS: number` (24 hours)

- [ ] **Step 1: Write the failing test**

Create `packages/api/src/permissions-cache.test.ts`:

```ts
import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import {
  PERMISSIONS_CACHE_KEY,
  PERMISSIONS_CACHE_TTL_MS,
  clearPermissionsCache,
  readPermissionsCache,
  writePermissionsCache,
} from "./permissions-cache";

const entry = {
  subject: "doc",
  tenantId: "11111111-1111-1111-1111-111111111111",
  permissions: ["medicore.visit.read"],
  storedAt: 1_700_000_000_000,
};

describe("permissions cache", () => {
  // Fake timers pinned to the entry's own storedAt: TTL expiry is the
  // behaviour under test, so the clock has to be the thing the test
  // moves, not the wall clock.
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(entry.storedAt);
  });
  afterEach(() => vi.useRealTimers());

  it("round-trips an entry", () => {
    writePermissionsCache(entry);

    expect(readPermissionsCache()).toEqual(entry);
  });

  it("ignores and removes an entry older than the TTL", () => {
    writePermissionsCache(entry);
    vi.setSystemTime(entry.storedAt + PERMISSIONS_CACHE_TTL_MS + 1);

    expect(readPermissionsCache()).toBeNull();
    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });

  it("keeps an entry that is within the TTL", () => {
    writePermissionsCache(entry);
    vi.setSystemTime(entry.storedAt + PERMISSIONS_CACHE_TTL_MS - 1);

    expect(readPermissionsCache()).toEqual(entry);
  });

  it("recovers from a corrupt entry by removing it", () => {
    window.localStorage.setItem(PERMISSIONS_CACHE_KEY, "{not json");

    expect(readPermissionsCache()).toBeNull();
    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });

  it("rejects an entry of the wrong shape", () => {
    window.localStorage.setItem(
      PERMISSIONS_CACHE_KEY,
      JSON.stringify({ subject: "doc", permissions: "not-an-array" }),
    );

    expect(readPermissionsCache()).toBeNull();
  });

  it("does not throw when localStorage is unavailable", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("QuotaExceededError");
    });

    expect(() => writePermissionsCache(entry)).not.toThrow();
    expect(readPermissionsCache()).toBeNull();
  });

  it("clears the entry", () => {
    writePermissionsCache(entry);
    clearPermissionsCache();

    expect(readPermissionsCache()).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd packages/api && pnpm vitest run src/permissions-cache.test.ts`
Expected: FAIL — cannot resolve `./permissions-cache`.

- [ ] **Step 3: Write minimal implementation**

Create `packages/api/src/permissions-cache.ts`:

```ts
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd packages/api && pnpm vitest run src/permissions-cache.test.ts`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add packages/api/src/permissions-cache.ts packages/api/src/permissions-cache.test.ts
git commit -m "feat(api): add permissions cache module with TTL and corruption recovery (#760)"
```

---

### Task 3: Hydrate `usePermissions` from the cache

**Files:**
- Modify: `packages/api/src/hooks.ts:13-23`
- Modify: `packages/api/src/permissions.tsx`
- Modify: `packages/api/src/index.ts`
- Test: `packages/api/src/permissions.test.tsx`

**Interfaces:**
- Consumes: `readPermissionsCache`, `writePermissionsCache`, `PermissionsCacheEntry` from Task 2; the `subject`/`tenant_id` response fields from Task 1.
- Produces: `useApiQuery<T>(key, path, { poll?: boolean; placeholderData?: T })`; `usePermissions()` keeps its existing shape `{ permissions: Set<string>; isLoading: boolean; can: (p: string) => boolean }`; `@helivanta/api` additionally exports `clearPermissionsCache` for Task 4.

- [ ] **Step 1: Write the failing test**

Add to `packages/api/src/permissions.test.tsx` — first extend the existing helper so responses carry identity, then add the new cases. Replace the `mockPermissions` helper at the top of the file with:

```tsx
import { PERMISSIONS_CACHE_KEY, readPermissionsCache } from "./permissions-cache";

function mockPermissions(data: string[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data, subject: "doc", tenant_id: "tenant-a" }),
    }),
  );
}

function seedCache(permissions: string[]) {
  window.localStorage.setItem(
    PERMISSIONS_CACHE_KEY,
    JSON.stringify({
      subject: "doc",
      tenantId: "tenant-a",
      permissions,
      storedAt: Date.now(),
    }),
  );
}
```

Then append this suite to the file:

```tsx
describe("permissions cache hydration", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });
  afterEach(() => vi.unstubAllGlobals());

  // The point of the cache: a returning user's nav does not wait on the
  // network. The fetch here never resolves, so anything that renders is
  // necessarily coming from the cached set.
  it("renders permitted content from the cache without waiting for the request", async () => {
    seedCache(["pharmacy.dispense.fulfil"]);
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.getByText("Dispense")).toBeInTheDocument());
  });

  it("writes the fresh permission set to the cache", async () => {
    mockPermissions(["lab.order.read"]);
    renderWithProviders(<Can permission="lab.order.read">Orders</Can>);

    await waitFor(() =>
      expect(readPermissionsCache()).toMatchObject({
        subject: "doc",
        tenantId: "tenant-a",
        permissions: ["lab.order.read"],
      }),
    );
  });

  // Revocation converges on the response, with no reload: the cached
  // permission paints first, then the fresh set replaces it.
  it("drops a revoked permission once the request resolves", async () => {
    seedCache(["pharmacy.dispense.fulfil"]);
    mockPermissions([]);
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.queryByText("Dispense")).not.toBeInTheDocument());
    expect(readPermissionsCache()?.permissions).toEqual([]);
  });

  it("falls back to the loading state when nothing is cached", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    expect(screen.queryByText("Dispense")).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd packages/api && pnpm vitest run src/permissions.test.tsx`
Expected: FAIL — "renders permitted content from the cache" times out (nothing reads the cache) and "writes the fresh permission set" finds `null`.

- [ ] **Step 3: Add `placeholderData` to `useApiQuery`**

In `packages/api/src/hooks.ts`, replace `useApiQuery`:

```ts
export function useApiQuery<T>(
  key: unknown[],
  path: string,
  opts: { poll?: boolean; placeholderData?: T } = {},
): UseQueryResult<T, Error> {
  return useQuery({
    queryKey: key,
    queryFn: () => apiFetch<T>(path),
    refetchInterval: opts.poll ? POLL_INTERVAL_MS : undefined,
    // placeholderData, not initialData: it is shown while the request is
    // in flight but never written to the query cache, so a locally cached
    // value can never be mistaken for a server response or served to a
    // later consumer of the same key.
    placeholderData: opts.placeholderData,
  });
}
```

- [ ] **Step 4: Write the hydration in `usePermissions`**

Replace the body of `packages/api/src/permissions.tsx` above `Can` with:

```tsx
"use client";

import { useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { useApiQuery } from "./hooks";
import {
  readPermissionsCache,
  writePermissionsCache,
  type PermissionsCacheEntry,
} from "./permissions-cache";

const PERMISSIONS_KEY = ["iam", "me", "permissions"];

// Public marks a route as deliberately unguarded. It mirrors the Go
// backend's authz.Public sentinel (backend/pkg/authz/authz.go) — the
// backend's PermissionSet.Has treats it as always-allowed, and this hook
// is the single place the frontend must do the same, so callers never
// need to special-case the string "public" themselves.
const PUBLIC_PERMISSION = "public";

type PermissionsResponse = { data: string[]; subject?: string; tenant_id?: string };

// Zone apps prerender this tree on the server, where localStorage does not
// exist. Reading the cache during render would make the hydration render
// disagree with the server HTML, so the read happens in a layout effect —
// after hydration commits, but before the browser paints the hydrated
// tree, so the nav does not visibly pop in.
const useIsomorphicLayoutEffect = typeof window === "undefined" ? useEffect : useLayoutEffect;

function useCachedPermissions(): PermissionsCacheEntry | null {
  const [cached, setCached] = useState<PermissionsCacheEntry | null>(null);
  useIsomorphicLayoutEffect(() => {
    setCached(readPermissionsCache());
  }, []);
  return cached;
}

/**
 * The caller's resolved permissions for the active tenant.
 *
 * This is a convenience layer for hiding what a user cannot do — the API
 * is the enforcement point. Never treat `can()` as a security boundary.
 *
 * The last known set is cached in localStorage and shown while the
 * request is in flight, so navigating between zones (a hard navigation by
 * design) does not blank the nav on every click. The response always
 * wins: a revoked permission disappears as soon as it lands, without a
 * reload.
 */
export function usePermissions(): {
  permissions: Set<string>;
  isLoading: boolean;
  can: (permission: string) => boolean;
} {
  const cached = useCachedPermissions();
  const { data, isPlaceholderData } = useApiQuery<PermissionsResponse>(
    PERMISSIONS_KEY,
    "/iam/me/permissions",
    { placeholderData: cached ? { data: cached.permissions } : undefined },
  );

  // Write through only on a real response. Placeholder data is the cache
  // itself, so persisting it would keep refreshing storedAt and hold a
  // stale entry alive past its TTL forever.
  useEffect(() => {
    if (isPlaceholderData || !data?.subject || !data.tenant_id) return;
    writePermissionsCache({
      subject: data.subject,
      tenantId: data.tenant_id,
      permissions: data.data,
      storedAt: Date.now(),
    });
  }, [data, isPlaceholderData]);

  // Memoized so identity is stable across renders when the underlying data
  // hasn't changed — behaviour is unchanged, this only avoids rebuilding the
  // Set (and any downstream re-renders it would trigger) every render.
  const permissions = useMemo(() => new Set(data?.data ?? []), [data]);
  // Derived from the data itself rather than the query's status flag: with
  // placeholder data there is a set to answer from even though the request
  // is still in flight.
  const isLoading = data === undefined;
  return {
    permissions,
    isLoading,
    can: (permission: string) =>
      // Public is always allowed, even while loading — it needs no data
      // from /iam/me/permissions to resolve, so there is nothing to wait
      // on. Every other permission is denied while loading, so a slow
      // response never flashes an action the user cannot perform.
      permission === PUBLIC_PERMISSION || (!isLoading && permissions.has(permission)),
  };
}
```

Leave the `Can` component below unchanged.

- [ ] **Step 5: Export the clear helper**

In `packages/api/src/index.ts`, add:

```ts
export { PERMISSIONS_CACHE_KEY, clearPermissionsCache } from "./permissions-cache";
```

`PERMISSIONS_CACHE_KEY` is exported so consumer tests assert against the constant instead of re-typing the literal key.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd packages/api && pnpm vitest run`
Expected: PASS — all pre-existing `Can` tests plus the four new hydration tests.

- [ ] **Step 7: Type-check and lint**

Run: `cd packages/api && pnpm type-check && pnpm lint`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add packages/api/src/hooks.ts packages/api/src/permissions.tsx packages/api/src/permissions.test.tsx packages/api/src/index.ts
git commit -m "feat(api): paint nav from cached permissions while revalidating (#760)"
```

---

### Task 4: Clear the cache at login, logout, and tenant switch

**Files:**
- Modify: `packages/ui/src/hms-shell.tsx:108-116` and `:189-194`
- Create: `packages/ui/src/hms-shell.test.tsx`
- Modify: `apps/shell/components/tenant-picker.tsx:74-81`
- Modify: `apps/shell/components/tenant-picker.test.tsx`
- Modify: `apps/shell/app/login/page.tsx:24-39`

**Interfaces:**
- Consumes: `clearPermissionsCache` and `PERMISSIONS_CACHE_KEY`, both exported from `@helivanta/api` in Task 3. Tests assert against the exported constant — never a re-typed `"hms.permissions.v1"` literal.
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing shell test**

Create `packages/ui/src/hms-shell.test.tsx`:

```tsx
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { PERMISSIONS_CACHE_KEY } from "@helivanta/api";
import { HmsShell } from "./hms-shell";

function seedCache(permissions: string[]) {
  window.localStorage.setItem(
    PERMISSIONS_CACHE_KEY,
    JSON.stringify({
      subject: "doc",
      tenantId: "tenant-a",
      permissions,
      storedAt: Date.now(),
    }),
  );
}

describe("HmsShell", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: ["lab.order.read"], subject: "doc", tenant_id: "tenant-a" }),
      }),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("renders zones the cached permissions allow", async () => {
    seedCache(["lab.order.read"]);
    renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await waitFor(() => expect(screen.getByLabelText("Lab")).toBeInTheDocument());
  });

  // A shared hospital terminal must not show the next user the previous
  // user's nav, so signing out drops the cached set.
  it("clears the cached permissions when signing out", async () => {
    seedCache(["lab.order.read"]);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd packages/ui && pnpm vitest run src/hms-shell.test.tsx`
Expected: FAIL on "clears the cached permissions when signing out" — the entry is still present.

- [ ] **Step 3: Clear the cache from the logout links**

In `packages/ui/src/hms-shell.tsx`, add the import:

```tsx
import { usePermissions, clearPermissionsCache } from "@helivanta/api";
```

Add this handler inside `HmsShell`, next to the existing `usePersistedFlag` call:

```tsx
  // Both sign-out links are plain <a> (cross-zone navigation is a hard
  // navigation by design), so this only drops the cached permission set
  // before the browser follows the link — the navigation itself is
  // untouched.
  const onSignOut = () => clearPermissionsCache();
```

Add `onClick={onSignOut}` to both sign-out anchors — the rail link at `href="/logout"` with `aria-label="Sign out"`, and the header link at `href="/logout"` reading "Sign out".

- [ ] **Step 4: Run the shell tests to verify they pass**

Run: `cd packages/ui && pnpm vitest run src/hms-shell.test.tsx`
Expected: PASS (2 tests). A jsdom "Not implemented: navigation" console notice from the anchor click is expected and does not fail the run.

- [ ] **Step 5: Write the failing tenant-switch test**

Append to the existing `describe` in `apps/shell/components/tenant-picker.test.tsx`:

```tsx
  // A tenant switch changes the whole permission set, so the cached one
  // must not survive the reload that follows.
  it("clears the cached permissions when switching tenants", async () => {
    window.localStorage.setItem(
      PERMISSIONS_CACHE_KEY,
      JSON.stringify({
        subject: "doc",
        tenantId: "t1",
        permissions: ["lab.order.read"],
        storedAt: Date.now(),
      }),
    );
    signInWithCustomToken.mockResolvedValue({
      user: { getIdTokenResult: async () => ({ claims: { tenant_id: "t2" }, token: "id-token" }) },
    });
    stubFetch();
    const { user } = renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByTitle("Hospital")).toBeInTheDocument());
    await user.selectOptions(screen.getByTitle("Hospital"), "t2");

    await waitFor(() => expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull());
  });
```

Add `PERMISSIONS_CACHE_KEY` to the file's existing `@helivanta/api` import (the test file already imports `renderWithProviders` from `@helivanta/api/testing`; the constant comes from `@helivanta/api`).

- [ ] **Step 6: Run test to verify it fails**

Run: `cd apps/shell && pnpm vitest run components/tenant-picker.test.tsx`
Expected: FAIL — the cache entry is still present after the switch.

- [ ] **Step 7: Clear the cache on tenant switch**

In `apps/shell/components/tenant-picker.tsx`, extend the import:

```tsx
import { useApiMutation, useApiQuery, apiFetch, clearPermissionsCache } from "@helivanta/api";
```

Replace the mutation's `onSuccess` options block:

```tsx
    {
      successToast: "Switched hospital",
      // A tenant switch changes every cached query, so reload rather than
      // trying to invalidate selectively. The permission cache lives in
      // localStorage and would survive that reload, so it is dropped
      // explicitly first — otherwise the new tenant's first paint would
      // show the old tenant's nav.
      onSuccess: () => {
        clearPermissionsCache();
        window.location.reload();
      },
    },
```

- [ ] **Step 8: Run the tenant-picker tests to verify they pass**

Run: `cd apps/shell && pnpm vitest run components/tenant-picker.test.tsx`
Expected: PASS, including all pre-existing switch-sequence tests.

- [ ] **Step 9: Clear the cache on successful sign-in**

In `apps/shell/app/login/page.tsx`, extend the import:

```tsx
import { clearPermissionsCache } from "@helivanta/api";
```

In `onSubmit`, replace the success tail so the cache is dropped before navigating:

```tsx
      if (!res.ok) throw new Error("session");
      // Whoever signs in now owns this browser's permission cache. A
      // session that expired without an explicit sign-out leaves the
      // previous user's entry behind, so clearing here — not only on the
      // logout link — is what keeps the next user from seeing their nav.
      clearPermissionsCache();
      router.replace("/");
```

- [ ] **Step 10: Run the login tests**

Run: `cd apps/shell && pnpm vitest run app/login/login.test.tsx`
Expected: PASS (unchanged behaviour; the clear is additive).

- [ ] **Step 11: Commit**

```bash
git add packages/ui/src/hms-shell.tsx packages/ui/src/hms-shell.test.tsx apps/shell/components/tenant-picker.tsx apps/shell/components/tenant-picker.test.tsx apps/shell/app/login/page.tsx
git commit -m "feat(shell): clear cached permissions on login, logout and tenant switch (#760)"
```

---

### Task 5: Verify the whole slice and open the PR

**Files:**
- Modify: none (verification only, plus the plan/spec docs already committed)

**Interfaces:**
- Consumes: everything from Tasks 1-4.
- Produces: a PR closing #760.

- [ ] **Step 1: Run the full frontend gate**

Run: `pnpm turbo lint type-check test build`
Expected: all tasks green. Fix any failure before continuing — do not proceed with a red gate.

- [ ] **Step 2: Run the full backend gate**

Run: `cd backend && make -C .. lint-go && go test -race ./... && ./scripts/coverage-gate.sh`
Expected: lint clean, tests pass, coverage at or above the 70% floor.

- [ ] **Step 3: Check the smoke test selectors still work**

Run: `grep -n "Sign out\|Zones\|aria-label" e2e/tests/smoke.spec.ts`
Expected: the sign-out and zone-nav selectors this plan touched (`aria-label="Sign out"`, `aria-label="Zones"`) are unchanged in `hms-shell.tsx`. If the smoke spec asserts on nav timing, confirm it still passes: `pnpm test:e2e` if the harness is available locally, otherwise note it for CI.

- [ ] **Step 4: Push the branch**

```bash
git push -u origin feat/760-permissions-client-cache
```

- [ ] **Step 5: Open the PR**

```bash
gh pr create --title "feat: cache permissions client-side so zone nav paints on hydration" --body "$(cat <<'EOF'
## Summary

Cross-zone navigation is a hard navigation between separate Next apps, so every zone load started with an empty query cache and rendered only the Dashboard nav entry until `/iam/me/permissions` resolved. This caches the last known permission set in localStorage and paints from it while the request revalidates in the background.

Vertical slice: backend, frontend, and tests together.

- `GET /iam/me/permissions` now also returns the caller's `subject` and `tenant_id`, so the browser can tell whose permissions it holds (the session cookie is httpOnly).
- New `permissions-cache` module in `@helivanta/api`: one localStorage key, 24h TTL, corruption and storage-unavailable both degrade to a cache miss.
- `usePermissions` reads the cache in a layout effect (not during render, to avoid a hydration mismatch) and passes it as `placeholderData`; every fresh response overwrites the entry, so a revoked permission converges with no reload.
- Cache cleared on sign-in, sign-out, and tenant switch.

Trust model is unchanged: this hides doors that will not open, the API remains the enforcement layer.

## Known limitation

The server-rendered HTML still contains the Dashboard-only nav — localStorage does not exist during prerender. The cache removes the network round-trip, so the full nav appears at hydration rather than after the request returns.

## Test plan

- [x] `packages/api/src/permissions-cache.test.ts` — round-trip, TTL expiry, corrupt entry, storage unavailable
- [x] `packages/api/src/permissions.test.tsx` — warm-cache paint without waiting on the request, write-through, revocation convergence, cold-cache fallback
- [x] `packages/ui/src/hms-shell.test.tsx` — zones render from cache; sign-out clears it
- [x] `apps/shell/components/tenant-picker.test.tsx` — tenant switch clears it
- [x] `backend/internal/modules/iam/me_test.go` — response carries `subject` and `tenant_id`
- [x] `pnpm turbo lint type-check test build`, `make lint-go`, `go test -race ./...`, coverage gate

Closes #760
EOF
)"
```

- [ ] **Step 6: Report the PR URL**

Print the URL `gh pr create` returned so the work can be reviewed.
