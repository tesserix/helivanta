"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { useEffect, useState, type ReactNode } from "react";

// @hms/api must not depend on @hms/ui (the dependency runs the other way —
// @hms/ui depends on @hms/api), so this mirrors @hms/ui/src/theme.tsx's
// useThemeAttribute hook locally rather than importing it. Keep the
// "data-theme" attribute name and "default"/"dark" values identical to that
// contract if either copy changes.
function useThemeAttribute(): "default" | "dark" {
  const [theme, setTheme] = useState<"default" | "dark">("default");

  useEffect(() => {
    const read = () => (document.documentElement.dataset.theme === "dark" ? "dark" : "default");
    setTheme(read());

    const observer = new MutationObserver(() => setTheme(read()));
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);

  return theme;
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
        theme={themeAttribute === "dark" ? "dark" : "light"}
      />
    </QueryClientProvider>
  );
}
