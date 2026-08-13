import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
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
    // Sign-out ends in a hard navigation to /login (packages/ui/src/hms-shell.tsx),
    // which jsdom does not implement; stub it so that step is observable
    // instead of throwing "Not implemented: navigation".
    vi.stubGlobal("location", { ...window.location, href: "" });
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

  // Regresses #781: a zone app that supplies no `onSignOut` (every zone
  // but the shell — see the prop's doc comment) must still revoke the
  // session server-side rather than only navigating away.
  it("POSTs to /logout by default when no onSignOut is supplied", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/logout", { method: "POST" }));
    expect(window.location.href).toBe("/login");
  });

  // The shell app supplies the Firebase-aware sequence (apps/shell/lib/sign-out.ts)
  // instead of the built-in default — HmsShell must run it, not its own
  // fetch, and only navigate once it has finished.
  it("runs the supplied onSignOut instead of the default POST, then navigates to /login", async () => {
    // renderWithProviders mounts usePermissions too, which fetches
    // /api/v1/iam/me/permissions on its own regardless of sign-out; the
    // assertion below only cares whether /logout specifically was hit.
    const fetchMock = vi
      .fn()
      .mockResolvedValue({ ok: true, status: 200, json: async () => ({ data: [] }) });
    vi.stubGlobal("fetch", fetchMock);
    let resolveSignOut: () => void = () => {};
    const onSignOut = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveSignOut = resolve;
        }),
    );
    const { user } = renderWithProviders(
      <HmsShell active="/" onSignOut={onSignOut}>
        content
      </HmsShell>,
    );

    await user.click(screen.getByLabelText("Sign out"));

    expect(onSignOut).toHaveBeenCalledTimes(1);
    // Navigation must wait for the injected sequence to finish — a caller
    // who supplied Firebase teardown must have it actually run before the
    // browser leaves the page.
    expect(window.location.href).toBe("");
    expect(fetchMock).not.toHaveBeenCalledWith("/logout", { method: "POST" });

    resolveSignOut();
    await waitFor(() => expect(window.location.href).toBe("/login"));
  });
});
