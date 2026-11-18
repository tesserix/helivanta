import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import WorkspaceError from "./error";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("conceals the failure, shows a support reference and retries the page", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(null, { status: 204 })),
  );
  vi.spyOn(console, "error").mockImplementation(() => {});
  const reset = vi.fn();
  const { user } = renderWithProviders(
    <WorkspaceError error={new Error("Could not load dispenses")} reset={reset} />,
  );

  expect(screen.getByRole("heading", { name: "Something went wrong" })).toBeInTheDocument();
  expect(screen.queryByText("Could not load dispenses")).not.toBeInTheDocument();
  expect(await screen.findByText(/^HV-[0-9a-f-]{36}$/)).toBeInTheDocument();
  // HmsShell owns the page's <main>; a second one would be an a11y error.
  expect(screen.queryByRole("main")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(reset).toHaveBeenCalledOnce();
});

it("falls back to a generic message when the error has none", () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(null, { status: 204 })),
  );
  vi.spyOn(console, "error").mockImplementation(() => {});
  renderWithProviders(<WorkspaceError error={new Error("")} reset={() => {}} />);

  expect(screen.getByText(/The page could not be loaded/)).toBeInTheDocument();
});
