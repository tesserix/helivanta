import { NextRequest, NextResponse } from "next/server";

// "/api/auth" covers app/api/auth/callback/page.tsx, the redirect target
// Zitadel sends the browser back to — it does not run with an
// helivanta_session cookie yet, since completing it IS what obtains one.
// "/api/session" (the old Firebase-era route that set the cookie from a
// raw ID token) is gone; the API sets the cookie itself now (design spec
// D1). There used to be a second route here,
// app/api/auth/silent-renew/page.tsx, loaded in a hidden iframe by
// lib/renew.ts for Zitadel-driven silent renewal — deleted by #916
// (design spec D1): that mechanism depended on Zitadel's SameSite=Lax
// session cookie travelling on a cross-site iframe request, which it
// never did. Renewal is now POST /v1/auth/renew, a same-origin call that
// carries the EXISTING helivanta_session cookie and is therefore covered
// by this middleware's normal, cookie-required path below, not by
// PUBLIC_PATHS.
//
// "/api/v1/auth/login" is the one exception carved out of the otherwise
// protected "/api/v1/*" surface: it is the exchange endpoint itself
// (backend/internal/modules/iam/login.go, mounted outside the API's own
// authenticated chain for the identical reason — see main.go's comment
// on bootstrap.MountUnauthenticated). The first login
// (app/api/auth/callback/page.tsx) calls it with a Zitadel ID token,
// never an helivanta_session, so this middleware must not demand one
// either — the exact bug that produced a `role="alert"` reading "missing
// credentials" on every login attempt before this exemption existed
// (login was calling the very route that is supposed to hand out the
// credential middleware was demanding).
const PUBLIC_PATHS = ["/login", "/api/auth", "/api/v1/auth/login"];

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  if (PUBLIC_PATHS.some((p) => pathname.startsWith(p))) {
    return NextResponse.next();
  }
  if (!req.cookies.get("helivanta_session")?.value) {
    if (pathname.startsWith("/api/")) {
      return NextResponse.json(
        { error: "unauthenticated", message: "missing credentials" },
        { status: 401 },
      );
    }
    const login = new URL("/login", req.url);
    return NextResponse.redirect(login);
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
