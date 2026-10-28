import { AUTH_CALLBACK_PATH } from "@/lib/oidc";

// PUBLIC_PATHS mirrors middleware.ts's own list, minus "/login" (which
// this component also skips, for the same "no session yet" reason) — kept
// as a literal here rather than imported, since middleware.ts runs in the
// Edge runtime and this component runs in the browser; sharing a
// constant across that boundary is not worth the coupling for a short
// path prefix.
export function isPublicPath(pathname: string): boolean {
  return (
    pathname === "/" ||
    pathname === "/workspace" ||
    pathname === "/signup" ||
    pathname === "/ambulance" ||
    pathname.startsWith("/login") ||
    pathname.startsWith(AUTH_CALLBACK_PATH)
  );
}
