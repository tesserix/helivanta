import { screen } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { releaseSnapshotForTest, renderWithProviders } from "@helivanta/api/testing";

import PasswordRecoveryPage from "./recover-password/page";
import SignupPage from "./signup/page";
import SignupVerificationPage from "./signup/verify/page";
import StaffAcceptancePage from "./staff/accept/page";

// The branding provider reads ?tenant off the real URL, as on /login.
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(window.location.search),
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));
vi.mock("@/lib/oidc", () => ({ getUserManager: () => ({ signinRedirect: vi.fn() }) }));

const TENANT = "22222222-2222-4222-8222-222222222222";

function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith(`/auth/branding/${TENANT}`)) {
        return Response.json({
          display_name: "City Hospital",
          welcome_text: "Welcome to City Hospital.",
          logo_url: "",
          theme: "blue",
          login_layout: "centered",
        });
      }
      if (url.endsWith("/auth/release-capabilities")) {
        const { policy_version, issued_at, expires_at } = releaseSnapshotForTest(
          { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" },
          [],
        );
        return Response.json({ policy_version, issued_at, expires_at, onboarding: true });
      }
      return Response.json({ state: "not_started" });
    }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  window.sessionStorage.clear();
  window.history.replaceState(null, "", "/");
});

const pages: [string, () => Promise<ReactElement> | ReactElement, string, string][] = [
  [
    "/recover-password",
    () => PasswordRecoveryPage({ searchParams: Promise.resolve({}) }),
    "Recover your password",
    "Password recovery",
  ],
  [
    "/signup",
    () => <SignupPage />,
    "Bring your organisation into Helivanta.",
    "Workspace registration",
  ],
  [
    "/signup/verify",
    () => <SignupVerificationPage />,
    "Open your latest email link",
    "Workspace login verification",
  ],
  [
    "/staff/accept",
    () => <StaffAcceptancePage />,
    "Open your invitation email",
    "Staff account setup",
  ],
];

describe.each(pages)("%s (#1853 D1)", (path, page, heading, region) => {
  it("renders in the sign-in card with the Helivanta mark, not the public shell", async () => {
    window.history.replaceState(null, "", path);
    stubFetch();
    const { container } = renderWithProviders(await page());

    expect(await screen.findByRole("heading", { level: 1, name: heading })).toBeVisible();
    expect(screen.getByRole("link", { name: "Helivanta home" })).toHaveAttribute("href", "/");
    expect(container.querySelector(".public-site")).toBeNull();
    expect(container.querySelector(".login-page")).not.toBeNull();
    expect(screen.queryByText("helivanta.")).not.toBeInTheDocument();
    // Only an entry page carries the sign-in surface's own footer, as /login does.
    if (path !== "/signup") expect(screen.queryByText(/Powered by/)).not.toBeInTheDocument();
    // AuthCardCentered's frame, the one /login's steps use.
    const card = screen.getByRole("region", { name: region });
    expect(card.parentElement).toHaveClass("rounded-2xl", "shadow-2xl", "bg-card/95");
    expect(card.parentElement?.parentElement).toHaveClass("max-w-md");
    expect(card).toContainElement(screen.getByRole("heading", { level: 1, name: heading }));
  });
});

describe.each([pages[0], pages[3]])("%s with a known tenant", (path, page, heading) => {
  it("shows the tenant's branding instead of the Helivanta mark", async () => {
    window.history.replaceState(null, "", `${path}?tenant=${TENANT}`);
    stubFetch();
    renderWithProviders(await page());

    expect(await screen.findByText("City Hospital")).toBeVisible();
    expect(screen.getByRole("heading", { level: 1, name: heading })).toBeVisible();
    // A hospital's mark never links to Helivanta's home page.
    expect(screen.queryByRole("link", { name: /home$/ })).not.toBeInTheDocument();
  });
});
