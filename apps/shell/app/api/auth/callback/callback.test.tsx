import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { PERMISSIONS_CACHE_KEY } from "@helivanta/api";
import AuthCallbackPage from "./page";

const signinRedirectCallback = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager }));

const replace = vi.hoisted(() => vi.fn());
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));

describe("AuthCallbackPage", () => {
  beforeEach(() => {
    signinRedirectCallback.mockReset();
    replace.mockReset();
    getUserManager.mockReturnValue({ signinRedirectCallback });
    window.localStorage.clear();
  });

  it("exchanges a successfully validated id_token and redirects to the dashboard", async () => {
    signinRedirectCallback.mockResolvedValue({ id_token: "real-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ tenant_id: "t1" }) }),
    );

    render(<AuthCallbackPage />);

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/"));
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ id_token: "real-id-token" }),
      }),
    );
    vi.unstubAllGlobals();
  });

  // THE mutation-discriminating test: state/PKCE validation is entirely
  // oidc-client-ts's job (see page.tsx's doc comment), and this is what a
  // forged or replayed callback URL — one whose `state` does not match
  // what signinRedirect() stashed in sessionStorage — looks like from
  // this file's perspective: signinRedirectCallback() rejects. The
  // assertion that matters is `fetch` never being called: a state-skip
  // regression would let this exchange happen anyway using
  // whatever `id_token` a forged response carried.
  it("never exchanges anything when the callback's state cannot be validated", async () => {
    signinRedirectCallback.mockRejectedValue(new Error("No matching state found in storage"));
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    render(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent("No matching state found in storage");
    expect(fetchMock).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("clears the cached permissions before exchanging, so the next session never inherits stale nav", async () => {
    window.localStorage.setItem(
      PERMISSIONS_CACHE_KEY,
      JSON.stringify({
        subject: "old-user",
        tenantId: "t1",
        permissions: ["lab.order.read"],
        storedAt: Date.now(),
      }),
    );
    signinRedirectCallback.mockResolvedValue({ id_token: "fresh-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ tenant_id: "t1" }) }),
    );

    render(<AuthCallbackPage />);

    await waitFor(() => expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull());
    vi.unstubAllGlobals();
  });

  it("shows an error and never redirects when Zitadel returns no id_token", async () => {
    signinRedirectCallback.mockResolvedValue({ id_token: undefined });
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    render(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent(/id_token/);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("shows the exchange's error message and never redirects when POST /v1/auth/login refuses the token", async () => {
    signinRedirectCallback.mockResolvedValue({ id_token: "real-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ error: "not_found", message: "no accessible hospital for this account" }),
      }),
    );

    render(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent("no accessible hospital for this account");
    expect(replace).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});
