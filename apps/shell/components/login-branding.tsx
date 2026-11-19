"use client";
import { createContext, useContext, useEffect, useSyncExternalStore, type ReactNode } from "react";
import { useSearchParams } from "next/navigation";
import { useApiQuery, brandingSchema, DEFAULT_BRANDING, type Branding } from "@helivanta/api";
import { LoginSurface } from "@helivanta/ui";
const Context = createContext<Branding>(DEFAULT_BRANDING);
const key = "helivanta.login.branding-tenant";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const subscribeNever = () => () => {};
function readRetained() {
  try {
    return window.sessionStorage.getItem(key);
  } catch {
    return null;
  }
}
export function LoginBrandingProvider({ children }: { children: ReactNode }) {
  const params = useSearchParams();
  // Workspace links name the organisation the same way the branding preview does.
  const requested = params.get("tenant") ?? params.get("workspace");
  // Only read when the URL names no tenant, so the write below never needs a re-read.
  const retained = useSyncExternalStore(subscribeNever, readRetained, () => null);
  useEffect(() => {
    if (requested) retainLoginBrandingTenant(requested);
  }, [requested]);
  const tenant = requested ?? retained;
  const valid = !!tenant && uuid.test(tenant);
  const query = useApiQuery<Branding>(["public-branding", tenant], `/auth/branding/${tenant}`, {
    enabled: valid,
  });
  const parsed = brandingSchema.safeParse(query.data);
  return (
    <Context.Provider value={valid && parsed.success ? parsed.data : DEFAULT_BRANDING}>
      {children}
    </Context.Provider>
  );
}
// Keeps the chosen organisation's branding across the identity-provider round trip.
export function retainLoginBrandingTenant(tenantId: string) {
  try {
    if (uuid.test(tenantId)) window.sessionStorage.setItem(key, tenantId);
  } catch {
    /* Cosmetic hint only; storage restrictions never block sign-in. */
  }
}
export function TenantLoginLayout({ children }: { children: ReactNode }) {
  const branding = useContext(Context);
  // The provider hands out DEFAULT_BRANDING itself whenever no tenant branding resolved.
  return (
    <LoginSurface branding={branding} homeHref={branding === DEFAULT_BRANDING ? "/" : undefined}>
      {children}
    </LoginSurface>
  );
}
export function TenantLoginHeading() {
  return (
    <h1 className="text-2xl font-semibold tracking-tight">{useContext(Context).display_name}</h1>
  );
}
