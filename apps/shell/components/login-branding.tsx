"use client";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { useSearchParams } from "next/navigation";
import { useApiQuery, brandingSchema, DEFAULT_BRANDING, type Branding } from "@helivanta/api";
import { LoginSurface } from "@helivanta/ui";
const Context = createContext<Branding>(DEFAULT_BRANDING);
const key = "helivanta.login.branding-tenant";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export function LoginBrandingProvider({ children }: { children: ReactNode }) {
  const params = useSearchParams();
  const requested = params.get("tenant");
  const [retained, setRetained] = useState<string | null>(null);
  useEffect(() => {
    try {
      if (requested && uuid.test(requested)) window.sessionStorage.setItem(key, requested);
      setRetained(window.sessionStorage.getItem(key));
    } catch {
      /* Cosmetic hint only; storage restrictions never block sign-in. */
    }
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
export function TenantLoginLayout({ children }: { children: ReactNode }) {
  return <LoginSurface branding={useContext(Context)}>{children}</LoginSurface>;
}
export function TenantLoginHeading() {
  return (
    <h1 className="text-2xl font-semibold tracking-tight">{useContext(Context).display_name}</h1>
  );
}
