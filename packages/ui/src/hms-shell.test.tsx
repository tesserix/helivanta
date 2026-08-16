import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { PERMISSIONS_CACHE_KEY } from "@hms/api";
import { HmsShell } from "./hms-shell";

const endZitadelSession = vi.hoisted(() => vi.fn());
vi.mock("./zitadel-session", () => ({ endZitadelSession }));

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
    endZitadelSession.mockReset();
    endZitadelSession.mockResolvedValue(true);
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

  // Regresses #781: EVERY app — every zone, not only the shell — must
  // revoke the HMS session server-side AND end Zitadel's own SSO session
  // by default, with no `onSignOut` override needed any more. That used
  // to be shell-only; found broken live via the e2e suite (signing out
  // from a zone page left Zitadel's SSO cookie alive, so the next login —
  // even for a different identity — completed silently with no
  // credential prompt) and fixed by moving the Zitadel-ending step into
  // this package's own default (zitadel-session.ts).
  it("POSTs to /logout AND ends the Zitadel session by default when no onSignOut is supplied", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/logout", { method: "POST" }));
    await waitFor(() => expect(endZitadelSession).toHaveBeenCalledTimes(1));
  });

  // endZitadelSession() itself owns the same-origin fallback when it
  // can't reach Zitadel (no config, or the RP-initiated logout failed) —
  // see zitadel-session.ts. HmsShell must not ALSO navigate in that case,
  // or it would race whatever endZitadelSession() already started.
  it("does not navigate itself even when ending the Zitadel session falls back", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }));
    endZitadelSession.mockResolvedValue(false);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    await waitFor(() => expect(endZitadelSession).toHaveBeenCalledTimes(1));
    expect(window.location.href).toBe("");
  });

  // A caller that DOES supply onSignOut (an extension point this package
  // still offers, even though shell no longer needs it — see the prop's
  // doc comment) must have it run instead of the built-in default, and
  // HmsShell must NOT also navigate itself: a redirect onSignOut starts
  // would be raced and cancelled by a same-origin `/login` navigation
  // right after (see handleSignOut's doc comment).
  it("runs the supplied onSignOut instead of the default POST, and never navigates itself", async () => {
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
    expect(window.location.href).toBe("");
    expect(fetchMock).not.toHaveBeenCalledWith("/logout", { method: "POST" });

    resolveSignOut();
    await waitFor(() => expect(onSignOut).toHaveResolved());
    // Still no navigation from HmsShell itself, even after onSignOut
    // resolves — ownership of navigation stayed with onSignOut the whole
    // time.
    expect(window.location.href).toBe("");
  });

  // #848 D4/D7: every app that renders HmsShell tracks interaction and
  // reports it to the activity endpoint, not only the shell's dashboard.
  describe("idle activity tracking", () => {
    it("POSTs to the activity endpoint on a qualifying interaction", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: [] }),
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      window.dispatchEvent(new Event("keydown"));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/session/activity",
          expect.objectContaining({ method: "POST" }),
        ),
      );
    });

    // The single most important guarantee here (spec "Errors and failure
    // handling"): a transient failure on this background ping must never
    // end the session. Only the server-side idle_deadline may do that.
    it("does not sign the user out when the activity call fails", async () => {
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.reject(new Error("network down"));
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      window.dispatchEvent(new Event("keydown"));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/session/activity",
          expect.objectContaining({ method: "POST" }),
        ),
      );
      // Give the rejected promise's .catch() a turn, then assert nothing
      // resembling sign-out/teardown happened.
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(fetchMock).not.toHaveBeenCalledWith("/logout", { method: "POST" });
      expect(endZitadelSession).not.toHaveBeenCalled();
      expect(window.location.href).toBe("");
    });
  });
});
