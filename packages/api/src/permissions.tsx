"use client";

import { useMemo, type ReactNode } from "react";
import { useApiQuery } from "./hooks";

const PERMISSIONS_KEY = ["iam", "me", "permissions"];

/**
 * The caller's resolved permissions for the active tenant.
 *
 * This is a convenience layer for hiding what a user cannot do — the API
 * is the enforcement point. Never treat `can()` as a security boundary.
 */
export function usePermissions(): {
  permissions: Set<string>;
  isLoading: boolean;
  can: (permission: string) => boolean;
} {
  const { data, isLoading } = useApiQuery<{ data: string[] }>(
    PERMISSIONS_KEY,
    "/iam/me/permissions",
  );
  // Memoized so identity is stable across renders when the underlying data
  // hasn't changed — behaviour is unchanged, this only avoids rebuilding the
  // Set (and any downstream re-renders it would trigger) every render.
  const permissions = useMemo(() => new Set(data?.data ?? []), [data]);
  return {
    permissions,
    isLoading,
    // Deny while loading, so a slow response never flashes an action the
    // user cannot perform.
    can: (permission: string) => !isLoading && permissions.has(permission),
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
  if (isLoading) return null;
  return can(permission) ? <>{children}</> : <>{fallback}</>;
}
