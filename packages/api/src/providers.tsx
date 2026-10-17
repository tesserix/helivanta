"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { useState, useSyncExternalStore, type ReactNode } from "react";

// @helivanta/api must not depend on @helivanta/ui (the dependency runs the other way —
// @helivanta/ui depends on @helivanta/api), so this mirrors @helivanta/ui/src/theme.tsx's
// useThemeAttribute hook locally rather than importing it. Keep the
// "data-theme" attribute name and "default"/"dark" values identical to that
// contract if either copy changes.
function subscribeTheme(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme"],
  });
  return () => observer.disconnect();
}

function useThemeAttribute(): "default" | "dark" {
  return useSyncExternalStore(
    subscribeTheme,
    () => (document.documentElement.dataset.theme === "dark" ? "dark" : "default"),
    () => "default",
  );
}

// Every zone layout wraps its children in AppProviders: one query
// client + one toast outlet per app (spec D2/D4).
export function AppProviders({ children }: { children: ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: { queries: { staleTime: 5_000, retry: 1 } },
      }),
  );
  const themeAttribute = useThemeAttribute();
  return (
    <QueryClientProvider client={client}>
      {children}
      <Toaster
        richColors
        position="top-right"
        offset={{ top: 80, right: 24 }}
        theme={themeAttribute === "dark" ? "dark" : "light"}
      />
    </QueryClientProvider>
  );
}
