import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { releaseSnapshotForTest, renderWithProviders } from "@helivanta/api/testing";
import SignupPage from "./page";

// The sign-in card's branding provider reads the URL (#1853).
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(window.location.search),
}));

afterEach(() => vi.unstubAllGlobals());

it("offers real tenant signup for a new owner and keeps the existing sign-in path", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
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
  renderWithProviders(<SignupPage />);
  expect(await screen.findByRole("heading", { name: "Create your workspace" })).toBeVisible();
  expect(screen.getByRole("textbox", { name: "Owner email" })).toBeVisible();
  expect(screen.getByRole("link", { name: "Sign in to an existing workspace" })).toHaveAttribute(
    "href",
    "/login?start=1",
  );
  expect(screen.queryByText("Self-service signup is coming soon")).not.toBeInTheDocument();
});
