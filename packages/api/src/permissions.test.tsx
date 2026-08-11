import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "./testing";
import { Can } from "./permissions";

function mockPermissions(data: string[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data }),
    }),
  );
}

describe("Can", () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => vi.unstubAllGlobals());

  it("renders children when the permission is held", async () => {
    mockPermissions(["pharmacy.dispense.fulfil"]);
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.getByText("Dispense")).toBeInTheDocument());
  });

  it("renders nothing when the permission is missing", async () => {
    mockPermissions(["medicore.visit.read"]);
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.queryByText("Dispense")).not.toBeInTheDocument());
  });

  it("renders the fallback when the permission is missing", async () => {
    mockPermissions([]);
    renderWithProviders(
      <Can permission="pharmacy.dispense.fulfil" fallback={<span>Not allowed</span>}>
        Dispense
      </Can>,
    );

    await waitFor(() => expect(screen.getByText("Not allowed")).toBeInTheDocument());
  });

  it("renders nothing while permissions are loading", () => {
    vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    expect(screen.queryByText("Dispense")).not.toBeInTheDocument();
  });
});
