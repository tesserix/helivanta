"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { useEffect, useState, type ReactNode } from "react";
import { clearPermissionsCache } from "./permissions-cache";
import { clearRenewAt } from "./renew-schedule";
import { SESSION_CHANGE_KEY, sessionChangeSource } from "./session-change";

// Every zone layout wraps its children in AppProviders: one query
// client + one toast outlet per app (spec D2/D4).
export function AppProviders({
  children,
  redirectOnSessionChange = true,
}: {
  children: ReactNode;
  redirectOnSessionChange?: boolean;
}) {
  const [sessionChanged, setSessionChanged] = useState<string | null>(null);
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: { queries: { staleTime: 5_000, retry: 1 } },
      }),
  );
  useEffect(() => {
    let changed = false;
    const invalidate = (
      message = "Your session changed in another tab. Sign in again to continue.",
    ) => {
      if (changed) return;
      changed = true;
      void client.cancelQueries();
      client.clear();
      clearPermissionsCache();
      clearRenewAt();
      if (redirectOnSessionChange) {
        setSessionChanged(message);
        window.location.replace("/login");
      }
    };
    const onChange = (event: StorageEvent) => {
      if (event.key !== SESSION_CHANGE_KEY || !event.newValue) return;
      try {
        if (event.storageArea !== window.localStorage) return;
      } catch {
        return;
      }
      invalidate();
    };
    const onBroadcast = (event: MessageEvent<unknown>) => {
      const message = event.data;
      if (
        typeof message === "object" &&
        message !== null &&
        "type" in message &&
        message.type === "changed" &&
        "source" in message &&
        typeof message.source === "string" &&
        message.source.length === 36 &&
        message.source !== sessionChangeSource()
      )
        invalidate();
    };
    const onPageShow = (event: PageTransitionEvent) => {
      if (event.persisted) {
        invalidate("Rechecking your session after restoring this page. Sign in again to continue.");
      }
    };
    let channel: BroadcastChannel | undefined;
    try {
      channel = new BroadcastChannel(SESSION_CHANGE_KEY);
      channel.addEventListener("message", onBroadcast);
    } catch {
      // Storage events remain available when BroadcastChannel is unsupported.
    }
    window.addEventListener("storage", onChange);
    window.addEventListener("pageshow", onPageShow);
    return () => {
      window.removeEventListener("storage", onChange);
      window.removeEventListener("pageshow", onPageShow);
      channel?.removeEventListener("message", onBroadcast);
      channel?.close();
    };
  }, [client, redirectOnSessionChange]);
  if (sessionChanged) {
    return <p role="status">{sessionChanged}</p>;
  }
  return (
    <QueryClientProvider client={client}>
      {children}
      <Toaster
        richColors
        position="top-right"
        offset={{ top: 80, right: 24 }}
        // Light only (#1255): the app has no dark theme to follow.
        theme="light"
      />
    </QueryClientProvider>
  );
}
