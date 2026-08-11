# Client-side permissions cache — design

- **Issue:** #760 — Cache permissions client-side so zone nav renders instantly after hard navigation
- **Branch:** `feat/760-permissions-client-cache`
- **Date:** 2026-08-11
- **Status:** Approved

## Problem

Cross-zone navigation is a hard navigation between separate Next apps (by design). Every zone app builds a fresh `QueryClient`, so on each zone load `usePermissions` starts in `isLoading`, `can()` denies everything non-public, and `visibleZones` renders only the Dashboard entry until `/iam/me/permissions` resolves. Users see nav items pop in on every cross-zone sidebar click.

## Decision summary

| Decision | Choice |
|---|---|
| Cache identity | Both: identity returned in the permissions payload **and** explicit clearing at login/logout/tenant-switch choke points |
| Storage & hydration | Bespoke localStorage entry fed to the existing query as targeted `initialData` (no persister dependency, only this query persisted) |
| TTL | 24 hours; older entries are ignored on hydration |
| Trust model | Unchanged — presentation-only; the API remains the enforcement layer |

Delivered as one vertical slice: backend change + `@hms/api`/`@hms/ui`/shell changes + tests on both sides, folded into a single PR closing #760.

## Design

### 1. Backend: identity in the permissions payload

`GET /iam/me/permissions` (`backend/internal/modules/iam/me.go`) additionally returns the caller's identity from the already-resolved `authn.Principal`:

```json
{ "data": ["medicore.visit.read", "..."], "subject": "<uid>", "tenant_id": "<uuid>" }
```

Additive change; existing consumers only read `data`.

### 2. Cache module: `packages/api/src/permissions-cache.ts`

A pure module owning one localStorage key, `hms.permissions.v1`, storing:

```ts
{ subject: string; tenantId: string; permissions: string[]; storedAt: number }
```

- `readPermissionsCache()` — returns the entry, or `null` when: localStorage is unavailable, JSON is corrupt, the shape is wrong, or `storedAt` is older than 24h. Corrupt/expired entries are removed on read.
- `writePermissionsCache(entry)` / `clearPermissionsCache()` — try/catch everything; storage failures are silent cache misses, never user-facing errors.
- SSR-safe via `typeof window` guards.

### 3. Hydration: `usePermissions`

The hook reads the cache once and passes it to the existing query as `initialData` with `initialDataUpdatedAt: storedAt`. Because a persisted entry is always older than the query's 5s `staleTime`, React Query paints from the cache and immediately revalidates in the background — the entire stale-while-revalidate loop with no new machinery.

- With `initialData`, `isLoading` is `false`, so `can()` answers from the cached set on first render; the zone rail and page panel paint fully. No changes to `zones.ts` or `hms-shell.tsx` rendering logic.
- On every successful response the hook overwrites the cache with fresh permissions + identity. Revocations and identity changes converge after one background fetch, with no reload loop — query data updates in place.
- Cold cache (first-time user, expired TTL, cleared storage) behaves exactly as today: loading state, deny-while-loading, nav appears when the query resolves.

### 4. Invalidation choke points

- **Tenant switch:** `TenantPicker` (`apps/shell/components/tenant-picker.tsx`) calls `clearPermissionsCache()` immediately before `window.location.reload()`.
- **Logout:** both `<a href="/logout">` links in `packages/ui/src/hms-shell.tsx` get an `onClick` that clears the cache; they remain plain hard links (no router, no preventDefault).
- **Login:** the shell login page clears the cache before posting to `/api/session`, covering the missed-logout path (session expiry, then a different user signs in on the same browser).
- **Backstop:** the identity stored with the entry plus overwrite-on-fetch means any stale-identity first paint self-corrects after one background revalidation.

### 5. Security posture

Unchanged trust model: the cache hides/shows navigation only; every API call is enforced server-side. Permission strings are role-shape metadata, not secrets; choke-point clearing exists so one user's nav shape is not shown to the next user on a shared hospital terminal.

## Acceptance criteria (from #760)

1. Zone rail and page panel render their full permitted entries on first paint after a hard navigation for a returning user (warm cache) — no pop-in.
2. Cache invalidated on logout and tenant switch.
3. Permission revocation converges after background revalidation without a reload loop.

## Testing

- `packages/api/src/permissions-cache.test.ts` — read/write round-trip; TTL expiry; corrupt-entry recovery; localStorage-unavailable no-throw.
- `packages/api/src/permissions.test.tsx` — warm cache: `can()` true on first render; revalidation overwrites the cache; revoked permission converges after refetch; cold cache matches current behaviour.
- `apps/shell/components/tenant-picker.test.tsx` — switching tenants clears the cache.
- `backend/internal/modules/iam/me_test.go` — response carries `subject` and `tenant_id`.
- Gates: `pnpm turbo lint type-check test build`, `make lint-go`, `cd backend && go test -race ./...` + coverage gate.
