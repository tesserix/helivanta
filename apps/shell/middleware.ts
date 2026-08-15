import { NextRequest, NextResponse } from "next/server";

// "/api/auth" covers both app/api/auth/callback/page.tsx (the redirect
// target Zitadel sends the browser back to) and
// app/api/auth/silent-renew/page.tsx (loaded in a hidden iframe by
// lib/renew.ts) — neither runs with an hms_session cookie yet, since
// completing either IS what obtains one. "/api/session" (the old
// Firebase-era route that set the cookie from a raw ID token) is gone;
// the API sets the cookie itself now (design spec D1).
//
// "/api/v1/auth/login" is the one exception carved out of the otherwise
// protected "/api/v1/*" surface: it is the exchange endpoint itself
// (backend/internal/modules/iam/login.go, mounted outside the API's own
// authenticated chain for the identical reason — see main.go's comment
// on bootstrap.MountUnauthenticated). Both a first login
// (app/api/auth/callback/page.tsx) and silent renewal (lib/renew.ts) call
// it with a Zitadel ID token, never an hms_session, so this middleware
// must not demand one either — the exact bug that produced a
// `role="alert"` reading "missing credentials" on every login attempt
// before this exemption existed (login was calling the very route that
// is supposed to hand out the credential middleware was demanding).
const PUBLIC_PATHS = ["/login", "/api/auth", "/api/v1/auth/login"];

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  if (PUBLIC_PATHS.some((p) => pathname.startsWith(p))) {
    return NextResponse.next();
  }
  if (!req.cookies.get("hms_session")?.value) {
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
