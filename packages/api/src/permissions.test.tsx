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
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    expect(screen.queryByText("Dispense")).not.toBeInTheDocument();
  });

  it('treats "public" as always allowed, mirroring the Go backend\'s authz.Public sentinel', async () => {
    mockPermissions([]);
    renderWithProviders(<Can permission="public">Departments</Can>);

    await waitFor(() => expect(screen.getByText("Departments")).toBeInTheDocument());
  });

  // "public" needs no data from /iam/me/permissions to resolve, so it must
  // not flash hidden while that fetch is still in flight — the same
  // no-wait behavior visibleZones (packages/ui/src/zones.ts) relies on by
  // calling can() directly. Unlike the test above, this asserts the
  // SYNCHRONOUS render, before the never-resolving fetch has any chance
  // to settle: if Can gated "public" behind isLoading the way it gates
  // every other permission, this content would render nothing here.
  it('renders "public" content immediately, without waiting for permissions to load', () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    renderWithProviders(<Can permission="public">Departments</Can>);

    expect(screen.getByText("Departments")).toBeInTheDocument();
  });
});
