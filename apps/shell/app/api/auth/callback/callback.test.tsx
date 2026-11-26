import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { PERMISSIONS_CACHE_KEY, loadExpiresAt, loadRenewAt } from "@helivanta/api";
import AuthCallbackPage from "./page";

const signinRedirectCallback = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager }));

const replace = vi.hoisted(() => vi.fn());
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  useSearchParams: () => new URLSearchParams(window.location.search),
}));

describe("AuthCallbackPage", () => {
  beforeEach(() => {
    signinRedirectCallback.mockReset();
    replace.mockReset();
    getUserManager.mockReturnValue({ signinRedirectCallback });
    window.localStorage.clear();
    window.sessionStorage.clear();
  });

  it("exchanges a successfully validated id_token and redirects to the dashboard", async () => {
    signinRedirectCallback.mockResolvedValue({ id_token: "real-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ tenant_id: "t1" }) }),
    );

    renderWithProviders(<AuthCallbackPage />);

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/dashboard"));
    expect(window.localStorage.getItem("hms.session-change.v1")).not.toBeNull();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ id_token: "real-id-token" }),
      }),
    );
    vi.unstubAllGlobals();
  });

  // #916 Task 4, F3. This page is the ONLY place the server's first
  // renewal schedule can be captured: components/session-renewal.tsx is
  // deliberately not mounted on the auth-callback route, so by the time
  // it does mount, this response is gone. Dropping this store is not a
  // visible failure — the renewal loop silently reverts to a hardcoded
  // five-minute interval, which outlives any SESSION_TTL shorter than
  // that and logs every clinician out mid-session. Hence an explicit
  // assertion here rather than trusting the call site.
  it("stores the server's renew_at so the first renewal follows SESSION_TTL", async () => {
    const renewAt = "2026-08-20T04:12:06.000Z";
    signinRedirectCallback.mockResolvedValue({ id_token: "real-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ tenant_id: "t1", renew_at: renewAt }),
      }),
    );

    renderWithProviders(<AuthCallbackPage />);

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/dashboard"));
    expect(loadRenewAt()?.toISOString()).toBe(renewAt);
    vi.unstubAllGlobals();
  });

  // #941: the session's expiry bounds the renewal loop's retry cadence, and
  // the loop only mounts after this page navigates, so it must be stored here.
  it("stores the server's expires_at for the renewal loop's retry cadence", async () => {
    const expiresAt = "2026-08-20T04:18:06.000Z";
    signinRedirectCallback.mockResolvedValue({ id_token: "real-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({
          tenant_id: "t1",
          renew_at: "2026-08-20T04:12:06.000Z",
          expires_at: expiresAt,
        }),
      }),
    );

    renderWithProviders(<AuthCallbackPage />);

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/dashboard"));
    expect(loadExpiresAt()?.toISOString()).toBe(expiresAt);
    vi.unstubAllGlobals();
  });

  // The compatibility direction: a login response without the field must
  // still sign the clinician in. renew_at is additive, and a response
  // that loses it degrades to the documented fallback cadence rather
  // than throwing out of the sign-in path.
  it("signs in normally when the login response carries no renew_at", async () => {
    signinRedirectCallback.mockResolvedValue({ id_token: "real-id-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ tenant_id: "t1" }) }),
    );

    renderWithProviders(<AuthCallbackPage />);

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/dashboard"));
    expect(loadRenewAt()).toBeUndefined();
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

    renderWithProviders(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your sign-in took too long. Sign in again.",
    );
    // A session already in this browser must not answer the retry (#1820 slice 2 D2).
    expect(screen.getByRole("link", { name: "Sign in again" })).toHaveAttribute(
      "href",
      "/login?again=1",
    );
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

    renderWithProviders(<AuthCallbackPage />);

    await waitFor(() => expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull());
    vi.unstubAllGlobals();
  });

  it("shows an error and never redirects when Zitadel returns no id_token", async () => {
    signinRedirectCallback.mockResolvedValue({ id_token: undefined });
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<AuthCallbackPage />);

    // Never the library's or provider's own text (#1767).
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Sign-in didn't finish. Sign in again.",
    );
    expect(screen.getByRole("link", { name: "Sign in again" })).toHaveAttribute(
      "href",
      "/login?again=1",
    );
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
        json: async () => ({
          error: "not_found",
          message: "no accessible hospital for this account",
        }),
      }),
    );

    renderWithProviders(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "no accessible hospital for this account",
    );
    expect(screen.getByRole("link", { name: "Sign in again" })).toHaveAttribute(
      "href",
      "/login?again=1",
    );
    expect(replace).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  // #1856 D2: a fetch failure during the exchange is not our wording.
  it("shows a fixed sentence, never the raw text, when the exchange cannot be reached", async () => {
    signinRedirectCallback.mockResolvedValue({ id_token: "real-id-token" });
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    renderWithProviders(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Check your connection and try again.",
    );
    expect(screen.queryByText(/Failed to fetch/)).not.toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("shows a fixed sentence, never the raw text, when the signup call cannot be reached", async () => {
    signinRedirectCallback.mockResolvedValue({
      id_token: "native-signup-proof",
      state: { purpose: "customer-signup" },
    });
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    renderWithProviders(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Check your connection and try again.",
    );
    expect(screen.queryByText(/Failed to fetch/)).not.toBeInTheDocument();
    vi.unstubAllGlobals();
  });

  // #1820 slice 2 D1: the workspace this flow's own state named, never memory.
  it.each([
    [
      "11111111-1111-4111-8111-111111111111",
      "/login?workspace=11111111-1111-4111-8111-111111111111&start=1",
    ],
    ["11111111-1111-4111-8111-11111111111A", "/login?again=1"],
  ])("sends a refused workspace sign-in back to its workspace (%s)", async (tenantId, href) => {
    signinRedirectCallback.mockResolvedValue({
      id_token: "real-id-token",
      state: { purpose: "workspace-login", tenantId },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({
          error: "not_found",
          message: "no accessible hospital for this account",
        }),
      }),
    );

    renderWithProviders(<AuthCallbackPage />);

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign in again" })).toHaveAttribute("href", href);
    vi.unstubAllGlobals();
  });

  it("resumes a retained signup with native proof before creating a tenant session", async () => {
    signinRedirectCallback.mockResolvedValue({
      id_token: "native-signup-proof",
      state: { purpose: "customer-signup" },
    });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ state: "provisioning" }), { status: 200 }),
        ),
    );
    renderWithProviders(<AuthCallbackPage />);
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/signup"));
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/auth/signup/sign-in",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ idToken: "native-signup-proof" }),
        cache: "no-store",
      }),
    );
    expect(vi.mocked(fetch).mock.calls.some(([url]) => url === "/api/v1/auth/login")).toBe(false);
    vi.unstubAllGlobals();
  });

  it.each(["customer-signup-login", "workspace-login"])(
    "opens the exact workspace for %s",
    async (purpose) => {
      const tenantId = "11111111-1111-4111-8111-111111111111";
      signinRedirectCallback.mockResolvedValue({
        id_token: "native-signup-proof",
        state: { purpose, tenantId },
      });
      vi.stubGlobal(
        "fetch",
        vi
          .fn()
          .mockResolvedValue(
            new Response(JSON.stringify({ tenant_id: tenantId }), { status: 200 }),
          ),
      );
      renderWithProviders(<AuthCallbackPage />);
      await waitFor(() => expect(replace).toHaveBeenCalledWith("/dashboard"));
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/auth/login",
        expect.objectContaining({
          body: JSON.stringify({ id_token: "native-signup-proof", tenant_id: tenantId }),
        }),
      );
      vi.unstubAllGlobals();
    },
  );

  // #1751 spec D4: the single card names no tenant; the API takes the
  // subject's only binding.
  it("opens the account an email login resolved without naming a tenant", async () => {
    const tenantId = "11111111-1111-4111-8111-111111111111";
    signinRedirectCallback.mockResolvedValue({
      id_token: "native-email-proof",
      state: { purpose: "email-login" },
    });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(new Response(JSON.stringify({ tenant_id: tenantId }), { status: 200 })),
    );
    renderWithProviders(<AuthCallbackPage />);
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/dashboard"));
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({ body: JSON.stringify({ id_token: "native-email-proof" }) }),
    );
    // Nothing routes the next sign-in by memory (#1820 spec D2).
    expect(window.localStorage.getItem("helivanta.login.workspace.v1")).toBeNull();
    vi.unstubAllGlobals();
  });

  it("keeps a refused signup on the error screen without minting a tenant session", async () => {
    signinRedirectCallback.mockResolvedValue({
      id_token: "native-signup-proof",
      state: { purpose: "customer-signup" },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: "signup_refused",
            message: "Check your details or request a fresh email link.",
          }),
          { status: 400 },
        ),
      ),
    );
    renderWithProviders(<AuthCallbackPage />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Check your details");
    expect(replace).not.toHaveBeenCalled();
    expect(vi.mocked(fetch).mock.calls.some(([url]) => url === "/api/v1/auth/login")).toBe(false);
    vi.unstubAllGlobals();
  });

  it("rejects malformed signup navigation state before any API call", async () => {
    signinRedirectCallback.mockResolvedValue({
      id_token: "native-signup-proof",
      state: { purpose: "customer-signup-login", tenantId: "foreign" },
    });
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    renderWithProviders(<AuthCallbackPage />);
    await screen.findByRole("alert");
    expect(fetcher).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
  // #1853 D2: both states sit in the sign-in card, under the Helivanta mark.
  function expectSignInCard(container: HTMLElement, content: HTMLElement) {
    expect(screen.getByRole("link", { name: "Helivanta home" })).toHaveAttribute("href", "/");
    expect(container.querySelector(".login-page")).not.toBeNull();
    expect(content.closest("section")?.parentElement).toHaveClass("rounded-2xl", "shadow-2xl");
  }

  it("shows the loading card with a status while the callback runs", () => {
    signinRedirectCallback.mockReturnValue(new Promise(() => {}));
    const { container } = renderWithProviders(<AuthCallbackPage />);
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("Signing you in…");
    expectSignInCard(container, status);
  });

  it("shows the error in the same card with a Sign in again button", async () => {
    signinRedirectCallback.mockRejectedValue(new Error("No matching state found in storage"));
    vi.stubGlobal("fetch", vi.fn());
    const { container } = renderWithProviders(<AuthCallbackPage />);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveClass("text-destructive");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    const again = screen.getByRole("link", { name: "Sign in again" });
    expect(again).toHaveAttribute("href", "/login?again=1");
    // Button asChild: the link carries the button's styling.
    expect(again).toHaveClass("w-full");
    expectSignInCard(container, alert);
    expect(alert.closest("section")).toContainElement(again);
    vi.unstubAllGlobals();
  });
});
