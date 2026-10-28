import { NextRequest, NextResponse } from "next/server";
import { CSP_REPORT_PATH, contentSecurityPolicy, createNonce } from "@/lib/csp";

// Read directly rather than through lib/env.ts, whose @helivanta/api import
// would pull React Query into the edge bundle. new URL() throws on a missing
// value, so the shell still refuses to serve without it.
const ISSUER_ORIGIN = new URL(process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL ?? "").origin;

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
const PUBLIC_PATHS = ["/login", "/api/auth", "/api/v1/auth/login", "/api/v1/auth/branding/"];

export function middleware(req: NextRequest) {
  // A fresh nonce per request (#1113). Next.js reads it from the forwarded
  // request policy and stamps its own scripts; the root layout reads x-nonce.
  const nonce = createNonce();
  const policy = contentSecurityPolicy(nonce, {
    dev: process.env.NODE_ENV === "development",
    issuerOrigin: ISSUER_ORIGIN,
  });
  const withPolicy = (response: NextResponse) => {
    response.headers.set("Content-Security-Policy", policy);
    return response;
  };
  const next = () => {
    const headers = new Headers(req.headers);
    headers.set("x-nonce", nonce);
    headers.set("Content-Security-Policy", policy);
    return withPolicy(NextResponse.next({ request: { headers } }));
  };

  const { pathname } = req.nextUrl;
  if (
    pathname === "/" ||
    pathname === "/workspace" ||
    pathname === "/signup" ||
    pathname === "/icon.svg" ||
    pathname === "/ambulance" ||
    pathname === "/api/emergency/access" ||
    // Violations happen on /login before any session exists (spec D6).
    pathname === CSP_REPORT_PATH
  ) {
    return next();
  }
  if (PUBLIC_PATHS.some((p) => pathname.startsWith(p))) {
    return next();
  }
  if (!req.cookies.get("helivanta_session")?.value) {
    if (pathname.startsWith("/api/")) {
      return withPolicy(
        NextResponse.json(
          { error: "unauthenticated", message: "missing credentials" },
          { status: 401 },
        ),
      );
    }
    const login = new URL("/login", req.url);
    return withPolicy(NextResponse.redirect(login));
  }
  return next();
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
