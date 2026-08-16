import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { SIGNED_OUT_MARK } from "@hms/ui";

import LoginPage from "./page";

const signinRedirect = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager }));

// LoginPageContent reads `authRequest` off useSearchParams(), so every
// test controls which of the page's two states it exercises through this
// mock rather than depending on jsdom's default (query-string-less) URL.
const searchParams = vi.hoisted(() => new URLSearchParams());
vi.mock("next/navigation", () => ({
  useSearchParams: () => searchParams,
}));

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("LoginPage", () => {
  beforeEach(() => {
    signinRedirect.mockReset();
    getUserManager.mockReturnValue({ signinRedirect });
    window.sessionStorage.clear();
    searchParams.delete("authRequest");
  });

  describe("with no auth request (landing page)", () => {
    // Arriving here unauthenticated is the COMMON case — middleware.ts
    // redirects any unauthenticated request to /login — so the default
    // wording must not claim a sign-out happened. Saying "You are signed
    // out" to someone who was never signed in is untrue, and on a shared
    // terminal it implies the previous clinician's session was ended when
    // nothing of the sort occurred.
    it("does not claim the user signed out when they simply arrived here", () => {
      render(<LoginPage />);
      expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
      expect(screen.getByText(/sign in to continue/i)).toBeInTheDocument();
    });

    it("confirms the sign-out when the user actually signed out", async () => {
      window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
      render(<LoginPage />);
      expect(await screen.findByText(/you are signed out/i)).toBeInTheDocument();
    });

    // Consumed on read: a reload, or navigating back here later in the same
    // tab, is no longer "you just signed out".
    it("only confirms it once", async () => {
      window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
      const first = render(<LoginPage />);
      expect(await screen.findByText(/you are signed out/i)).toBeInTheDocument();
      first.unmount();

      render(<LoginPage />);
      expect(screen.getByText(/sign in to continue/i)).toBeInTheDocument();
    });

    // #847: the redirect must NOT fire on mount. Sign-out lands on this page
    // (it is the registered post_logout_redirect_uri), and an authorization
    // request started on arrival raced Zitadel's session teardown — after
    // which signing in as a different user failed with "User not found in
    // the system". This is the assertion that fails if anyone reinstates the
    // useEffect, and its failure mode is a ward terminal the next clinician
    // cannot sign in to.
    it("does not start an authorization request on mount", async () => {
      render(<LoginPage />);
      // Give any stray effect a chance to run before asserting absence.
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument(),
      );
      expect(signinRedirect).not.toHaveBeenCalled();
    });

    // Design spec D6 (formerly D5a): with no auth request yet, there is
    // nothing for a credential form to check against, so this state still
    // renders none.
    it("renders no credential form", () => {
      render(<LoginPage />);
      expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
      expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
    });

    it("starts the redirect when the user asks for it", async () => {
      signinRedirect.mockResolvedValue(undefined);
      render(<LoginPage />);

      await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
      await waitFor(() => expect(signinRedirect).toHaveBeenCalledTimes(1));
    });

    // prompt=login belongs on THIS request and nowhere else. Silent renewal
    // (lib/renew.ts) uses prompt=none; forcing re-authentication there would
    // make every background refresh demand a password mid-consultation. And
    // omitting it here would let a live Zitadel session answer silently for
    // whoever is now at the keyboard, which is the whole point of the button.
    it("forces re-authentication rather than accepting a live IdP session", async () => {
      signinRedirect.mockResolvedValue(undefined);
      render(<LoginPage />);

      await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
      await waitFor(() =>
        expect(signinRedirect).toHaveBeenCalledWith(expect.objectContaining({ prompt: "login" })),
      );
    });

    it("shows an error instead of a dead button when the redirect cannot start", async () => {
      signinRedirect.mockRejectedValue(new Error("Zitadel unreachable"));
      render(<LoginPage />);

      await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
      expect(await screen.findByRole("alert")).toHaveTextContent("Zitadel unreachable");
      // Re-enabled, so a transient failure does not strand the user on a page
      // whose only control is dead.
      expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
    });
  });

  describe("with an auth request (credential form)", () => {
    beforeEach(() => {
      searchParams.set("authRequest", "V2_test-auth-request");
    });

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    // Spec D6: these accessible names are the contract
    // e2e/tests/support/login.ts drives every spec file's login step by.
    it("renders fields whose accessible names match the e2e contract", async () => {
      renderWithProviders(<LoginPage />);
      expect(await screen.findByLabelText("Email")).toBeInTheDocument();
      expect(screen.getByLabelText("Password")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    });

    it("shows an inline error and does not submit when the email is empty", async () => {
      const fetchMock = vi.fn();
      vi.stubGlobal("fetch", fetchMock);

      const { user } = renderWithProviders(<LoginPage />);
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      expect(await screen.findByText(/enter your email/i)).toBeInTheDocument();
      expect(fetchMock).not.toHaveBeenCalled();
    });

    // Spec D5: a wrong password and an unknown user answer identically, so
    // the form must show that ONE message without ever attributing it to a
    // specific field ("no account with that email" would rebuild the same
    // enumeration oracle the backend closed).
    it("shows one message for a refused credential and never says which field was wrong", async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue(
          jsonResponse(401, {
            error: "invalid_credentials",
            message: "email or password is incorrect",
          }),
        );
      vi.stubGlobal("fetch", fetchMock);

      const { user } = renderWithProviders(<LoginPage />);
      await user.type(await screen.findByLabelText("Email"), "clinician@hms.dev");
      await user.type(screen.getByLabelText("Password"), "wrong-password");
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      const alert = await screen.findByRole("alert");
      expect(alert).toHaveTextContent(/email or password is incorrect/i);
      // Neither field individually reports the refusal — Field only
      // renders a role="alert" paragraph when react-hook-form has an
      // error for that specific field, and validation passed here.
      expect(screen.queryByText(/no account/i)).not.toBeInTheDocument();
    });

    // Spec D4: a handoff is a NORMAL outcome (MFA required, a forced
    // password change, a federated hospital IdP, or a policy the API could
    // not read), never an error — the page must navigate there exactly as
    // it would a callback_url.
    it("navigates to the handoff url when the API says a handoff is required", async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue(
          jsonResponse(200, {
            handoff_url: "http://localhost:20080/ui/v2/login?authRequest=V2_test-auth-request",
          }),
        );
      vi.stubGlobal("fetch", fetchMock);
      const assignSpy = vi.fn();
      vi.stubGlobal("location", { ...window.location, assign: assignSpy });

      const { user } = renderWithProviders(<LoginPage />);
      await user.type(await screen.findByLabelText("Email"), "clinician@hms.dev");
      await user.type(screen.getByLabelText("Password"), "correct-password");
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      await waitFor(() =>
        expect(assignSpy).toHaveBeenCalledWith(
          "http://localhost:20080/ui/v2/login?authRequest=V2_test-auth-request",
        ),
      );
      // Not treated as a refusal — no shared-refusal alert rendered.
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    });

    it("navigates to the callback url when the credential completes the login", async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue(
          jsonResponse(200, {
            callback_url: "https://hms.example/api/auth/callback?code=abc&state=xyz",
          }),
        );
      vi.stubGlobal("fetch", fetchMock);
      const assignSpy = vi.fn();
      vi.stubGlobal("location", { ...window.location, assign: assignSpy });

      const { user } = renderWithProviders(<LoginPage />);
      await user.type(await screen.findByLabelText("Email"), "clinician@hms.dev");
      await user.type(screen.getByLabelText("Password"), "correct-password");
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      await waitFor(() =>
        expect(assignSpy).toHaveBeenCalledWith(
          "https://hms.example/api/auth/callback?code=abc&state=xyz",
        ),
      );
    });

    it("posts the credential to POST /v1/auth/login/password", async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue(
          jsonResponse(200, { callback_url: "https://hms.example/api/auth/callback" }),
        );
      vi.stubGlobal("fetch", fetchMock);
      vi.stubGlobal("location", { ...window.location, assign: vi.fn() });

      const { user } = renderWithProviders(<LoginPage />);
      await user.type(await screen.findByLabelText("Email"), "clinician@hms.dev");
      await user.type(screen.getByLabelText("Password"), "correct-password");
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/login/password",
          expect.objectContaining({
            method: "POST",
            body: JSON.stringify({
              auth_request_id: "V2_test-auth-request",
              login_name: "clinician@hms.dev",
              password: "correct-password",
            }),
          }),
        ),
      );
    });
  });
});
