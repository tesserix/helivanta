import { expect, it } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "./middleware";
it("allows only the exact diagnostic collector before login", () => {
  expect(
    middleware(new NextRequest("https://helivanta.app/api/client-errors", { method: "POST" }))
      .status,
  ).toBe(200);
  for (const path of [
    "/api/client-errors/private",
    "/api/client-errors-backup",
    "/api/v1/workforce/staff",
  ]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(401);
  }
});
it("allows only the exact staff recipient page and proof endpoints before login", () => {
  for (const path of [
    "/staff/accept",
    ...["status", "email", "password", "resend"].map(
      (action) => `/api/v1/auth/staff-setup/${action}`,
    ),
  ]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(200);
  }
  for (const path of [
    "/staff/accept/private",
    "/staff/admin",
    "/api/v1/auth/staff-setup/reset",
    "/api/v1/auth/staff-setup/status/extra",
  ]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(
      path.startsWith("/api/") ? 401 : 307,
    );
  }
});
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

// #1835 D6: a manifest or icon fetch carries no session, so each brand file
// is public by exact path; lookalikes still go to sign-in.
it("serves the brand files before login without opening similarly named routes", () => {
  for (const path of [
    "/apple-icon.png",
    "/opengraph-image.png?abc123",
    "/manifest.webmanifest",
    "/icon-192.png",
    "/icon-512.png",
    "/maskable-icon-512.png",
  ]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status, path).toBe(200);
  }
  for (const path of ["/manifest.webmanifest/x", "/icon-512.png.bak", "/apple-icon.png/private"]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status, path).toBe(307);
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

it("allows only the exact public signup and native verification paths without a tenant session", () => {
  for (const path of [
    "/signup/verify",
    "/api/v1/auth/signup",
    "/api/v1/auth/signup/verify-email",
    "/api/v1/auth/signup/password",
    "/api/v1/auth/signup/email-link",
    "/api/v1/auth/signup/password-link",
    "/api/v1/auth/signup/sign-in",
  ]) {
    const response = middleware(new NextRequest(`https://helivanta.app${path}`));
    expect(response.headers.get("x-middleware-next")).toBe("1");
  }
  const refused = middleware(
    new NextRequest("https://helivanta.app/api/v1/auth/signup/tenant-grants"),
  );
  expect(refused.status).toBe(401);
});

it("allows only exact password recovery pages and browser endpoints before login", () => {
  for (const path of [
    "/recover-password",
    "/api/v1/auth/password-recovery",
    ...["verify", "prepare", "cancel", "password"].map(
      (action) => `/api/v1/auth/password-recovery/${action}`,
    ),
  ]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(200);
  }
  for (const path of [
    "/recover-password/admin",
    "/api/v1/auth/password-recovery/reset-all",
    "/api/v1/auth/password-recovery/password/extra",
  ]) {
    expect(middleware(new NextRequest(`https://helivanta.app${path}`)).status).toBe(
      path.startsWith("/api/") ? 401 : 307,
    );
  }
});
