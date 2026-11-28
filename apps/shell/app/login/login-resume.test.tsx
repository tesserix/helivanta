import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";

import LoginPage from "./page";

vi.mock("@/lib/oidc", () => ({ getUserManager: () => ({ signinRedirect: vi.fn() }) }));
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(window.location.search),
  useRouter: () => ({ replace: vi.fn() }),
}));

const AUTH_REQUEST_ID = "V2_resume-request";
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function stub(resume: () => Response) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      if (url.includes("/auth/login/request/"))
        return json(200, {
          id: AUTH_REQUEST_ID,
          client_id: "helivanta-web",
          redirect_uri: "http://localhost:4301/api/auth/callback",
          scope: ["openid"],
          policies: {
            allow_password: true,
            second_factors: ["totp"],
            ignore_unknown_usernames: false,
          },
        });
      if (url.includes("/auth/login/resume")) return resume();
      if (url.includes("/auth/renew")) return json(401, { error: "unauthenticated" });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );
  return calls;
}

// #1778: after a setup page set the password, /login continues with the
// session the server parked, straight into the authenticator step.
describe("login after password setup", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    window.history.pushState({}, "", `/login?authRequest=${AUTH_REQUEST_ID}`);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("resumes the parked session into enrolment with no password field", async () => {
    window.sessionStorage.setItem("helivanta.setupResume", "1");
    const calls = stub(() =>
      json(200, {
        enrollment_required: ["totp"],
        totp: {
          uri: "otpauth://totp/Helivanta:new@helivanta.dev?secret=JBSWY3DPEHPK3PXP&issuer=Helivanta",
          secret: "JBSWY3DPEHPK3PXP",
        },
      }),
    );
    renderWithProviders(<LoginPage />);
    expect(
      await screen.findByRole("heading", { name: "Set up two-step verification" }),
    ).toBeVisible();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(calls.filter((url) => url.includes("/auth/login/resume"))).toHaveLength(1);
    expect(window.sessionStorage.getItem("helivanta.setupResume")).toBeNull();
  });

  it("shows the ordinary card when nothing was parked", async () => {
    window.sessionStorage.setItem("helivanta.setupResume", "1");
    stub(() => json(404, { error: "not_found", message: "Nothing to continue." }));
    renderWithProviders(<LoginPage />);
    expect(await screen.findByLabelText("Email or username")).toBeVisible();
    expect(screen.queryByText("Nothing to continue.")).not.toBeInTheDocument();
  });

  it("never asks to resume without the setup marker", async () => {
    const calls = stub(() => json(500, {}));
    renderWithProviders(<LoginPage />);
    expect(await screen.findByLabelText("Email or username")).toBeVisible();
    await waitFor(() =>
      expect(calls.some((url) => url.includes("/auth/login/request/"))).toBe(true),
    );
    expect(calls.some((url) => url.includes("/auth/login/resume"))).toBe(false);
  });
});
