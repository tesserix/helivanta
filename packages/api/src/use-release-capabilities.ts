"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { usePermissions } from "./permissions";
import {
  readPublicReleaseCapabilities,
  readReleaseCapabilities,
  type ReleaseCapabilityKey,
} from "./release-capabilities";
import { useTenantTransition } from "./tenant-transition";

export function useReleaseCapabilities() {
  const { tenantId, subject } = usePermissions();
  const scope = tenantId && subject ? { tenantId, subject } : null;
  const transitioning = useTenantTransition();

  const query = useQuery({
    queryKey: ["release-capabilities", scope?.tenantId, scope?.subject],
    queryFn: ({ signal }) => {
      if (!scope) throw new Error("Wait for the current account.");
      return readReleaseCapabilities(scope, signal);
    },
    enabled: !!scope && !transitioning,
    staleTime: 15_000,
    gcTime: 60_000,
    refetchInterval: 15_000,
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
    retry: false,
  });
  const { refetch } = query;
  const recheck = useCallback(() => {
    void refetch({ cancelRefetch: false });
  }, [refetch]);
  const current = useCurrentSnapshot(query.data?.expires_at, recheck);
  const data = query.data;
  const valid =
    !transitioning &&
    !!scope &&
    !!data &&
    data.tenant_id === scope.tenantId.toLowerCase() &&
    data.subject === scope.subject &&
    current;
  return {
    isTransitioning: transitioning,
    isLoading: transitioning || (!!scope && query.isPending),
    error: query.error,
    refresh: query.refetch,
    available: (key: ReleaseCapabilityKey | "baseline" | undefined) =>
      key === "baseline" ||
      (valid && data.capabilities.some((row) => row.key === key && row.available)),
  };
}

function useCurrentSnapshot(expires: string | undefined, recheck: () => void) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!expires) return;
    const timer = setTimeout(
      () => {
        setNow(Date.now());
        recheck();
      },
      Math.max(0, Date.parse(expires) - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [expires, recheck]);
  return !!expires && Date.parse(expires) > now;
}

export function usePublicReleaseCapabilities(enabled: boolean) {
  const query = useQuery({
    queryKey: ["public-release-capabilities"],
    queryFn: ({ signal }) => readPublicReleaseCapabilities(signal),
    enabled,
    staleTime: 15_000,
    refetchInterval: 15_000,
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
    retry: false,
  });
  const { refetch } = query;
  const recheck = useCallback(() => {
    void refetch({ cancelRefetch: false });
  }, [refetch]);
  const current = useCurrentSnapshot(query.data?.expires_at, recheck);
  return { ...query, available: current && !!query.data?.onboarding };
}
