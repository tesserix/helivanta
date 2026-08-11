import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { PERMISSIONS_CACHE_KEY } from "@hms/api";
import { HmsShell } from "./hms-shell";

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

describe("HmsShell", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    // Deliberately different from the seeded cache: the two prove
    // different things. If the stub matched the cache, `Lab` would render
    // whether or not the cache was ever read, since the network response
    // would paint the same zone anyway.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({
          data: ["pharmacy.dispense.read"],
          subject: "doc",
          tenant_id: "tenant-a",
        }),
      }),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  // Proves the cache is actually read on first paint (not just present in
  // storage): the cache and the network stub grant different permissions,
  // and the zone visible immediately is the cached one, not the network
  // one — usePermissions paints from cache before the fetch resolves.
  it("paints the cached zone before the network response arrives", async () => {
    seedCache(["lab.order.read"]);
    renderWithProviders(<HmsShell active="/">content</HmsShell>);

    expect(screen.getByLabelText("Lab")).toBeInTheDocument();
  });

  // A shared hospital terminal must not show the next user the previous
  // user's nav, so signing out drops the cached set.
  it("clears the cached permissions when signing out", async () => {
    seedCache(["lab.order.read"]);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });
});
