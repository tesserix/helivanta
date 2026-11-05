import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { RefreshButton } from "./refresh-button";

describe("RefreshButton", () => {
  it("refreshes on click", async () => {
    const refresh = vi.fn();
    renderWithProviders(
      <RefreshButton refresh={refresh} error={null}>
        Refresh
      </RefreshButton>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(refresh).toHaveBeenCalledOnce();
  });

  it("is disabled while busy", () => {
    renderWithProviders(
      <RefreshButton refresh={vi.fn()} error={null} busy>
        Refresh
      </RefreshButton>,
    );
    expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
  });

  it.each([401, 403, 404])("is not offered after a %i", (status) => {
    renderWithProviders(
      <RefreshButton refresh={vi.fn()} error={new ApiError("forbidden", "no", status)}>
        Refresh
      </RefreshButton>,
    );
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("is offered after an outage", () => {
    renderWithProviders(
      <RefreshButton refresh={vi.fn()} error={new ApiError("internal", "boom", 500)}>
        Refresh
      </RefreshButton>,
    );
    expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled();
  });
});
