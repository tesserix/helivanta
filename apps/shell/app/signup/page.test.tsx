import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import SignupPage from "./page";

afterEach(() => vi.unstubAllGlobals());

it("offers real tenant signup for a new owner and keeps the existing sign-in path", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ state: "not_started" }), { status: 200 })),
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
