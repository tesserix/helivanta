import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { RequestFailure, firstError } from "./request-failure";

const refused = "You don't have access to these reports.";

function noRetry() {
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  expect(screen.queryByText("Could not load.")).toBeNull();
}

describe("RequestFailure", () => {
  it.each([403, 404])("shows a %i as the caller's refusal with no retry", (status) => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={new ApiError("forbidden", "no", status)}
        refused={refused}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(refused);
    noRetry();
  });

  it.each([403, 404])("shows a %i without a sentence as the default refusal", (status) => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={new ApiError("forbidden", "no", status)}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This wasn't found, or you don't have access to it.",
    );
    noRetry();
  });

  it("shows a 401 as an ended session, even with a refusal sentence", () => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={new ApiError("unauthenticated", "no", 401)}
        refused={refused}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Your session has ended. Sign in again to continue.",
    );
    expect(screen.queryByText(refused)).toBeNull();
    noRetry();
  });

  it.each([403, 404])("offers the recheck for a %i, never Try again", async (status) => {
    const retry = vi.fn();
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={retry}
        error={new ApiError("forbidden", "no", status)}
        refused={refused}
        recheck="Check access again"
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(refused);
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Check access again" }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it("offers no recheck once the session has ended", () => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={new ApiError("unauthenticated", "no", 401)}
        recheck="Check access again"
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Your session has ended.");
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("still offers Try again for an outage when a recheck is set", () => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={new ApiError("internal", "boom", 500)}
        recheck="Check access again"
      />,
    );
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Check access again" })).toBeNull();
  });

  it.each([
    ["a server error", new ApiError("internal", "boom", 500)],
    ["a conflict", new ApiError("conflict", "stale", 409)],
    ["a network failure", new TypeError("Failed to fetch")],
    ["no error", null],
    ["a status-shaped object", { status: 403 }],
  ])("offers a retry for %s", async (_, error) => {
    const retry = vi.fn();
    renderWithProviders(
      <RequestFailure message="Could not load." retry={retry} error={error} refused={refused} />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load.");
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it.each([
    ["Try again", new ApiError("internal", "boom", 500)],
    ["Check access again", new ApiError("forbidden", "no", 403)],
  ])("retries with %s inside a form without submitting it", async (name, error) => {
    const retry = vi.fn();
    const submit = vi.fn((event: { preventDefault: () => void }) => event.preventDefault());
    renderWithProviders(
      <form onSubmit={submit}>
        <RequestFailure
          message="Could not load."
          retry={retry}
          error={error}
          recheck="Check access again"
        />
      </form>,
    );
    await userEvent.click(screen.getByRole("button", { name }));
    expect(retry).toHaveBeenCalledOnce();
    expect(submit).not.toHaveBeenCalled();
  });

  it.each([
    ["Try again", new ApiError("internal", "boom", 500)],
    ["Check access again", new ApiError("forbidden", "no", 403)],
  ])("disables %s while busy", (name, error) => {
    renderWithProviders(
      <RequestFailure
        message="Could not load."
        retry={vi.fn()}
        error={error}
        recheck="Check access again"
        busy
      />,
    );
    expect(screen.getByRole("button", { name })).toBeDisabled();
  });
});

describe("firstError", () => {
  const refusal = new ApiError("forbidden", "no", 403);
  const outage = new ApiError("internal", "boom", 500);
  it("prefers a refusal whichever query it came from", () => {
    expect(firstError(outage, refusal)).toBe(refusal);
    expect(firstError(null, refusal, outage)).toBe(refusal);
  });
  it("otherwise gives the first error, or null", () => {
    expect(firstError(null, outage)).toBe(outage);
    expect(firstError(null, undefined)).toBeNull();
  });
});

// The structural control (#1591): a call site that passes no error must not compile.
// @ts-expect-error error is required
void (<RequestFailure message="x" retry={() => {}} />);
