import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "./testing";
import { Can } from "./permissions";
import { PERMISSIONS_CACHE_KEY, readPermissionsCache } from "./permissions-cache";

function mockPermissions(data: string[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data, subject: "doc", tenant_id: "tenant-a" }),
    }),
  );
}

function seedCache(permissions: string[]) {
  window.localStorage.setItem(
    PERMISSIONS_CACHE_KEY,
    JSON.stringify({
      subject: "doc",
      tenantId: "tenant-a",
      permissions,
      storedAt: Date.now(),
    }),
  );
}

describe("Can", () => {
  // The cache is process-wide state now that usePermissions writes through
  // to it, so it is reset per test to keep these cases independent.
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });
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

describe("permissions cache hydration", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });
  afterEach(() => vi.unstubAllGlobals());

  // The point of the cache: a returning user's nav does not wait on the
  // network. The fetch here never resolves, so anything that renders is
  // necessarily coming from the cached set.
  it("renders permitted content from the cache without waiting for the request", async () => {
    seedCache(["pharmacy.dispense.fulfil"]);
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.getByText("Dispense")).toBeInTheDocument());
  });

  it("writes the fresh permission set to the cache", async () => {
    mockPermissions(["lab.order.read"]);
    renderWithProviders(<Can permission="lab.order.read">Orders</Can>);

    await waitFor(() =>
      expect(readPermissionsCache()).toMatchObject({
        subject: "doc",
        tenantId: "tenant-a",
        permissions: ["lab.order.read"],
      }),
    );
  });

  // Revocation converges on the response, with no reload: the cached
  // permission paints first, then the fresh set replaces it.
  it("drops a revoked permission once the request resolves", async () => {
    seedCache(["pharmacy.dispense.fulfil"]);
    mockPermissions([]);
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.queryByText("Dispense")).not.toBeInTheDocument());
    expect(readPermissionsCache()?.permissions).toEqual([]);
  });

  it("falls back to the loading state when nothing is cached", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    expect(screen.queryByText("Dispense")).not.toBeInTheDocument();
  });
});
