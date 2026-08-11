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
  // A faithful stand-in for the response it replaces, identity included —
  // so `isPlaceholderData` is what keeps it out of the cache, rather than
  // the write-through happening to trip over a missing `subject`.
  // Memoized so the placeholder keeps a stable identity across renders;
  // a fresh object every render would make the query observer rebuild the
  // placeholder result each time.
  const placeholderData = useMemo(
    () =>
      cached
        ? { data: cached.permissions, subject: cached.subject, tenant_id: cached.tenantId }
        : undefined,
    [cached],
  );
  const { data, isPending, isPlaceholderData } = useApiQuery<PermissionsResponse>(
    PERMISSIONS_KEY,
    "/iam/me/permissions",
    { placeholderData },
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
  // is still in flight. `isPending` keeps the failed-request case behaving
  // as before — an error resolves loading rather than hanging on it, so
  // `Can` still renders its fallback.
  const isLoading = data === undefined && isPending;
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

export function Can({
  permission,
  children,
  fallback = null,
}: {
  permission: string;
  children: ReactNode;
  fallback?: ReactNode;
}) {
  const { can, isLoading } = usePermissions();
  // Gate on loading only when it could still change the answer. `can()`
  // already resolves "public" to true regardless of isLoading (see
  // usePermissions above), so gating unconditionally here would hide
  // `<Can permission="public">` content behind the same fetch it needs
  // no data from — the exact flash the "public" sentinel exists to avoid
  // (mirrored by `visibleZones` in packages/ui/src/zones.ts, which calls
  // `can()` directly and relies on this same bypass). Every other
  // permission is still denied while loading, so a slow response never
  // flashes an action the user cannot perform.
  if (isLoading && !can(permission)) return null;
  return can(permission) ? <>{children}</> : <>{fallback}</>;
}
