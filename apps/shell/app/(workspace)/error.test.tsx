import { screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import WorkspaceError from "./error";

it("shows the failure and retries the page when asked", async () => {
  const reset = vi.fn();
  const { user } = renderWithProviders(
    <WorkspaceError error={new Error("Could not load dispenses")} reset={reset} />,
  );

  expect(screen.getByRole("heading", { name: "Something went wrong" })).toBeInTheDocument();
  expect(screen.getByText("Could not load dispenses")).toBeInTheDocument();
  // HmsShell owns the page's <main>; a second one would be an a11y error.
  expect(screen.queryByRole("main")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(reset).toHaveBeenCalledOnce();
});

it("falls back to a generic message when the error has none", () => {
  renderWithProviders(<WorkspaceError error={new Error("")} reset={() => {}} />);

  expect(screen.getByText("An unexpected error occurred.")).toBeInTheDocument();
});
