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
  it("requires confirmation and ends the current session before clearing cached data", async () => {
    const fetch = vi.fn().mockResolvedValue(Response.json({ ok: true }));
    vi.stubGlobal("fetch", fetch);
    window.localStorage.setItem(PERMISSIONS_CACHE_KEY, "old permissions");
    const { user } = renderPicker();
    await user.click(screen.getByRole("button", { name: "Switch account" }));
    expect(fetch).not.toHaveBeenCalled();
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
    expect(fetch).not.toHaveBeenCalled();
    expect(endSession).not.toHaveBeenCalled();
  });
});
