import { expect, it } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "./middleware";
it("allows published branding before login but protects workspace settings", () => {
  expect(
    middleware(
      new NextRequest(
        "https://helivanta.app/api/v1/auth/branding/11111111-1111-4111-8111-111111111111",
      ),
    ).status,
  ).toBe(200);
  expect(
    middleware(new NextRequest("https://helivanta.app/api/v1/workspace/branding")).status,
  ).toBe(401);
});

it("serves the public homepage, workspace page and signup without opening clinical routes", () => {
  for (const path of ["/", "/workspace", "/signup"]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(200);
  }
  for (const path of ["/dashboard", "/medicore/opd", "/pharmacy", "/lab", "/workspace/settings"]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(307);
  }
});

it("serves the favicon before login without opening similarly named routes", () => {
  expect(middleware(new NextRequest("https://helivanta.app/icon.svg?v=1")).status).toBe(200);
  for (const path of ["/icon.svg/private", "/icon.svg-backup", "/dashboard"]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(307);
  }
});

it("keeps only exact emergency access routes public", () => {
  for (const path of ["/ambulance", "/api/emergency/access?country=IN"]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(200);
  }
  for (const path of [
    "/ambulance/admin",
    "/ambulance-records",
    "/api/emergency/access/admin",
    "/api/emergency/access-token",
    "/api/v1/emergency/cases",
    "/api/v1/emergency/bases",
  ]) {
    const response = middleware(new NextRequest(`https://helivanta.app${path}`));
    expect(response.status).toBe(path.startsWith("/api/") ? 401 : 307);
  }
});

const POLICY = /script-src 'self' 'nonce-([A-Za-z0-9+/=]{24})' 'strict-dynamic'/;

function nonceOf(response: Response): string | undefined {
  return POLICY.exec(response.headers.get("Content-Security-Policy") ?? "")?.[1];
}

function signedIn(path: string): NextRequest {
  return new NextRequest(`https://helivanta.app${path}`, {
    headers: { cookie: "helivanta_session=s" },
  });
}

it("sets the policy on every exit path", () => {
  const cases: [NextRequest, number][] = [
    [new NextRequest("https://helivanta.app/"), 200],
    [new NextRequest("https://helivanta.app/login"), 200],
    [new NextRequest("https://helivanta.app/api/csp-report", { method: "POST" }), 200],
    [signedIn("/dashboard"), 200],
    [new NextRequest("https://helivanta.app/dashboard"), 307],
    [new NextRequest("https://helivanta.app/api/v1/workspace/branding"), 401],
  ];
  for (const [request, status] of cases) {
    const response = middleware(request);
    expect(response.status, request.nextUrl.pathname).toBe(status);
    expect(nonceOf(response), request.nextUrl.pathname).toBeDefined();
  }
});

it("mints a fresh nonce per request", () => {
  expect(nonceOf(middleware(signedIn("/dashboard")))).not.toBe(
    nonceOf(middleware(signedIn("/dashboard"))),
  );
});

// Next.js reads the nonce from the request's policy while rendering, and the
// root layout reads x-nonce; NextResponse.next forwards both as
// x-middleware-request-* headers.
it("forwards the nonce to rendering", () => {
  for (const request of [signedIn("/dashboard"), new NextRequest("https://helivanta.app/login")]) {
    const response = middleware(request);
    expect(response.headers.get("x-middleware-request-x-nonce")).toBe(nonceOf(response));
    expect(response.headers.get("x-middleware-request-content-security-policy")).toBe(
      response.headers.get("Content-Security-Policy"),
    );
  }
});

it("keeps only the exact report endpoint public", () => {
  expect(
    middleware(new NextRequest("https://helivanta.app/api/csp-report", { method: "POST" })).status,
  ).toBe(200);
  expect(middleware(new NextRequest("https://helivanta.app/api/csp-report/x")).status).toBe(401);
});
