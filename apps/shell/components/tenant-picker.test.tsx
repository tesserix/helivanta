import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { cancelTenantTransition, PERMISSIONS_CACHE_KEY } from "@helivanta/api";
import { NavigationGuardProvider } from "@helivanta/ui";
import { TenantPicker } from "./tenant-picker";
const endSession = vi.hoisted(() => vi.fn());
vi.mock("@helivanta/ui", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@helivanta/ui")>()),
  endZitadelSession: endSession,
}));
beforeEach(() => {
  endSession.mockReset();
  window.localStorage.clear();
});
afterEach(() => {
  cancelTenantTransition();
  vi.unstubAllGlobals();
});
function renderPicker() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <TenantPicker />
    </NavigationGuardProvider>,
  );
}
describe("Separate account switching", () => {
  it("shows the verified current tenant name even for a single account", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        Response.json({
          subject: "synthetic-owner",
          tenant_id: "11111111-1111-4111-8111-111111111111",
          display_name: "Synthetic Owner",
          email: "owner@example.test",
          roles: [],
          tenant_name: "Yes Hospital",
        }),
      ),
    );
    renderPicker();
    await waitFor(() =>
      expect(screen.getByLabelText("Current tenant")).toHaveTextContent("Yes Hospital"),
    );
    expect(screen.getByRole("button", { name: "Switch account" })).toBeEnabled();
  });
  it.each([
    [
      200,
      {
        subject: "synthetic-owner",
        tenant_id: "private-tenant-id",
        display_name: "Owner",
        email: "owner@example.test",
        roles: [],
      },
    ],
    [
      200,
      {
        subject: "synthetic-owner",
        tenant_id: "private-tenant-id",
        display_name: "Owner",
        email: "owner@example.test",
        roles: [],
        tenant_name: " invalid name ",
      },
    ],
    [503, { error: "profile_unavailable" }],
  ])(
    "keeps switching available without inventing a name for an unavailable profile (%s)",
    async (status, body) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => Response.json(body, { status })),
      );
      const { user } = renderPicker();
      await waitFor(() =>
        expect(screen.getByLabelText("Current tenant")).toHaveTextContent(
          "Account name unavailable",
        ),
      );
      expect(screen.queryByText("private-tenant-id")).toBeNull();
      await user.click(screen.getByRole("button", { name: "Switch account" }));
      await user.click(screen.getByRole("button", { name: "Cancel" }));
      expect(endSession).not.toHaveBeenCalled();
    },
  );
  it("requires confirmation and ends the current session before clearing cached data", async () => {
    const fetch = vi.fn(async () => Response.json({ ok: true }));
    vi.stubGlobal("fetch", fetch);
    window.localStorage.setItem(PERMISSIONS_CACHE_KEY, "old permissions");
    const { user } = renderPicker();
    await user.click(screen.getByRole("button", { name: "Switch account" }));
    expect(fetch).not.toHaveBeenCalledWith("/logout", { method: "POST" });
    await user.click(screen.getByRole("button", { name: "Sign out and choose account" }));
    await waitFor(() => expect(endSession).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith("/logout", { method: "POST" });
    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });
  it("keeps the current account when logout fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ error: "sign_out_failed" }, { status: 502 })),
    );
    window.localStorage.setItem(PERMISSIONS_CACHE_KEY, "old permissions");
    const { user } = renderPicker();
    await user.click(screen.getByRole("button", { name: "Switch account" }));
    await user.click(screen.getByRole("button", { name: "Sign out and choose account" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /current account has not been replaced/,
    );
    expect(endSession).not.toHaveBeenCalled();
    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBe("old permissions");
  });
  it("cancels without changing the session", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const { user } = renderPicker();
    await user.click(screen.getByRole("button", { name: "Switch account" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(fetch).not.toHaveBeenCalledWith("/logout", { method: "POST" });
    expect(endSession).not.toHaveBeenCalled();
  });
});
