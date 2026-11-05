import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { ApiError } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { RequestFailure } from "./request-failure";

const refused = "You don't have access to these reports.";

describe("RequestFailure", () => {
  it.each([401, 403, 404])("shows a %i as a refusal with no retry", (status) => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={new ApiError("forbidden", "no", status)}
        refused={refused}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(refused);
    expect(screen.queryByText("Could not load.")).toBeNull();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });

  it.each([
    ["a server error", new ApiError("internal", "boom", 500)],
    ["a network failure", new TypeError("Failed to fetch")],
    ["no error", undefined],
  ])("offers a retry for %s", (_, error) => {
    renderWithProviders(
      <RequestFailure message="Could not load." retry={vi.fn()} error={error} refused={refused} />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load.");
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
  });

  it("keeps the retry for a refusal when the caller names no refusal", () => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={new ApiError("forbidden", "no", 403)}
      />,
    );
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
  });
});
