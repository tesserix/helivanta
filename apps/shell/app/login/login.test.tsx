import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { SIGNED_OUT_MARK, IDLE_ENDED_MARK } from "@hms/ui";

import LoginPage from "./page";

const signinRedirect = vi.hoisted(() => vi.fn());
const getUserManager = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager }));

// LoginPageContent reads `authRequest` off useSearchParams(). Rather than
// mocking next/navigation to return a hoisted, hand-poked object (review
// finding: that mocked nothing about actually reading the URL), this
// derives the answer from jsdom's real window.location.search — so a
// test drives this page's URL-reading branch the same way it would drive
// any other value: by setting the URL, via window.history.pushState in
// the beforeEach blocks below, not by swapping out a returned object.
// This still does not exercise Next's OWN App Router context (there is
// no App Router test harness in this repo to exercise it against), but
// it does mean the mock's behaviour is genuinely driven by the URL
// rather than an independent fixture that could drift from it.
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(window.location.search),
}));

const AUTH_REQUEST_ID = "V2_test-auth-request";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

// The default, successful answer GET /v1/auth/login/request/:id gives —
// used by every "with an auth request" test that is not specifically
// exercising the auth-request-validation step itself, so those tests can
// stay focused on what they actually assert without each reimplementing
// this response.
function authRequestOkResponse() {
  return jsonResponse(200, {
    id: AUTH_REQUEST_ID,
    client_id: "hms-web",
    redirect_uri: "http://localhost:4301/api/auth/callback",
    scope: ["openid", "profile", "email"],
  });
}

// Routes a stubbed fetch by path: GET .../auth/login/request/:id vs POST
// .../auth/login/password, since ValidatedCredentialForm now calls the
// former before CredentialForm ever calls the latter. Defaults both to a
// success response so a test only has to override the one call it cares
// about.
function stubAuthFlow(opts: { authRequest?: () => Response; password?: () => Response } = {}) {
  const fetchMock = vi.fn((url: string) => {
    if (url.includes("/auth/login/request/")) {
      return Promise.resolve((opts.authRequest ?? authRequestOkResponse)());
    }
    if (url.includes("/auth/login/password")) {
      return Promise.resolve(
        (
          opts.password ??
          (() => jsonResponse(200, { callback_url: "https://hms.example/api/auth/callback" }))
        )(),
      );
    }
    throw new Error(`stubAuthFlow: unexpected fetch call to ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("LoginPage", () => {
  beforeEach(() => {
    signinRedirect.mockReset();
    getUserManager.mockReturnValue({ signinRedirect });
    window.sessionStorage.clear();
    window.history.pushState({}, "", "/login");
  });

  describe("with no auth request (landing page)", () => {
    // Arriving here unauthenticated is the COMMON case — middleware.ts
    // redirects any unauthenticated request to /login — so the default
    // wording must not claim a sign-out happened. Saying "You are signed
    // out" to someone who was never signed in is untrue, and on a shared
    // terminal it implies the previous clinician's session was ended when
    // nothing of the sort occurred.
    it("does not claim the user signed out when they simply arrived here", () => {
      renderWithProviders(<LoginPage />);
      expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
      expect(screen.getByText(/sign in to continue/i)).toBeInTheDocument();
    });

    it("confirms the sign-out when the user actually signed out", async () => {
      window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
      renderWithProviders(<LoginPage />);
      expect(await screen.findByText(/you are signed out/i)).toBeInTheDocument();
    });

    // #848 D6: the idle-timeout teardown is a THIRD, distinct outcome from
    // a deliberate sign-out — #850 exists precisely because claiming
    // someone signed out when they did not is untrue, and telling an
    // idle-ended session "you are signed out" is the same class of lie.
    it("says the session ended through inactivity, not that the user signed out", async () => {
      window.sessionStorage.setItem(IDLE_ENDED_MARK, "1");
      renderWithProviders(<LoginPage />);
      expect(await screen.findByText(/ended after a period of inactivity/i)).toBeInTheDocument();
      expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
    });

    // Consumed on read, same as SIGNED_OUT_MARK — a reload or navigating
    // back here later in the same tab is no longer "you just idled out".
    it("only shows the idle-ended wording once", async () => {
      window.sessionStorage.setItem(IDLE_ENDED_MARK, "1");
      const first = renderWithProviders(<LoginPage />);
      expect(await screen.findByText(/ended after a period of inactivity/i)).toBeInTheDocument();
      first.unmount();

      renderWithProviders(<LoginPage />);
      expect(screen.getByText(/sign in to continue/i)).toBeInTheDocument();
    });

    // Consumed on read: a reload, or navigating back here later in the same
    // tab, is no longer "you just signed out".
    it("only confirms it once", async () => {
      window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
      const first = renderWithProviders(<LoginPage />);
      expect(await screen.findByText(/you are signed out/i)).toBeInTheDocument();
      first.unmount();

      renderWithProviders(<LoginPage />);
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
      renderWithProviders(<LoginPage />);
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
      renderWithProviders(<LoginPage />);
      expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
      expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
    });

    it("starts the redirect when the user asks for it", async () => {
      signinRedirect.mockResolvedValue(undefined);
      renderWithProviders(<LoginPage />);

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
      renderWithProviders(<LoginPage />);

      await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
      await waitFor(() =>
        expect(signinRedirect).toHaveBeenCalledWith(expect.objectContaining({ prompt: "login" })),
      );
    });

    it("shows an error instead of a dead button when the redirect cannot start", async () => {
      signinRedirect.mockRejectedValue(new Error("Zitadel unreachable"));
      renderWithProviders(<LoginPage />);

      await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
      expect(await screen.findByRole("alert")).toHaveTextContent("Zitadel unreachable");
      // Re-enabled, so a transient failure does not strand the user on a page
      // whose only control is dead.
      expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
    });
  });

  describe("with an auth request (credential form)", () => {
    beforeEach(() => {
      window.history.pushState({}, "", `/login?authRequest=${AUTH_REQUEST_ID}`);
    });

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    // Spec D6: these accessible names are the contract
    // e2e/tests/support/login.ts drives every spec file's login step by.
    it("renders fields whose accessible names match the e2e contract", async () => {
      stubAuthFlow();
      renderWithProviders(<LoginPage />);
      expect(await screen.findByLabelText("Email")).toBeInTheDocument();
      expect(screen.getByLabelText("Password")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    });

    // Repo-wide: native browser validation is banned (docs/standards/
    // frontend.md §4), so every HMS form must disable it explicitly. This
    // pins the login form to that rule rather than relying on the zod
    // validation tests below to prove it only indirectly.
    it("disables native browser validation", async () => {
      stubAuthFlow();
      renderWithProviders(<LoginPage />);
      const email = await screen.findByLabelText("Email");
      expect(email.closest("form")).toHaveAttribute("novalidate");
    });

    it("shows an inline error and does not submit when the email is empty", async () => {
      const fetchMock = stubAuthFlow();

      const { user } = renderWithProviders(<LoginPage />);
      await user.click(await screen.findByRole("button", { name: "Sign in" }));

      expect(await screen.findByText(/enter your email/i)).toBeInTheDocument();
      // The auth-request GET is allowed (it is how the form got here at
      // all); the credential POST specifically must never have fired.
      expect(fetchMock).not.toHaveBeenCalledWith("/api/v1/auth/login/password", expect.anything());
    });

    // Spec D5: a wrong password and an unknown user answer identically, so
    // the form must show that ONE message without ever attributing it to a
    // specific field ("no account with that email" would rebuild the same
    // enumeration oracle the backend closed).
    it("shows one message for a refused credential and never says which field was wrong", async () => {
      stubAuthFlow({
        password: () =>
          jsonResponse(401, {
            error: "invalid_credentials",
            message: "email or password is incorrect",
          }),
      });

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

    // Review finding: useApiMutation's automatic toast.error() must be
    // suppressed for this mutation, since the inline alert above is the
    // form's own dedicated error surface (spec D5/D6) and showing the
    // SAME text twice, through two channels, is confusing rather than
    // helpful. This proves there is exactly ONE rendering of the refusal
    // text anywhere in the document — not merely that the alert exists.
    it("shows the refusal message exactly once, not also as a toast", async () => {
      stubAuthFlow({
        password: () =>
          jsonResponse(401, {
            error: "invalid_credentials",
            message: "email or password is incorrect",
          }),
      });

      const { user } = renderWithProviders(<LoginPage />);
      await user.type(await screen.findByLabelText("Email"), "clinician@hms.dev");
      await user.type(screen.getByLabelText("Password"), "wrong-password");
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      await screen.findByRole("alert");
      const occurrences =
        document.body.textContent?.split(/email or password is incorrect/i).length ?? 1;
      expect(occurrences - 1).toBe(1);
    });

    // Spec D4: a handoff is a NORMAL outcome (MFA required, a forced
    // password change, a federated hospital IdP, or a policy the API could
    // not read), never an error — the page must navigate there exactly as
    // it would a callback_url.
    it("navigates to the handoff url when the API says a handoff is required", async () => {
      stubAuthFlow({
        password: () =>
          jsonResponse(200, {
            handoff_url: "http://localhost:20080/ui/v2/login?authRequest=V2_test-auth-request",
          }),
      });
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
      stubAuthFlow({
        password: () =>
          jsonResponse(200, {
            callback_url: "https://hms.example/api/auth/callback?code=abc&state=xyz",
          }),
      });
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
      const fetchMock = stubAuthFlow();
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
              auth_request_id: AUTH_REQUEST_ID,
              login_name: "clinician@hms.dev",
              password: "correct-password",
            }),
          }),
        ),
      );
    });

    it("verifies the auth request first, via GET /v1/auth/login/request/:id, before rendering the form", async () => {
      const fetchMock = stubAuthFlow();
      renderWithProviders(<LoginPage />);

      await screen.findByLabelText("Email");
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v1/auth/login/request/${AUTH_REQUEST_ID}`,
        expect.anything(),
      );
    });

    // Review finding 2: the pending state must render the shared auth
    // chrome, not a blank page — a real network round trip (unlike the
    // synchronous useSearchParams read) makes this reachable to a real
    // browser, not just Next's build tooling.
    it("shows the auth chrome, not a blank page, while the auth request is being verified", async () => {
      let resolveAuthRequest!: (response: Response) => void;
      const pending = new Promise<Response>((resolve) => {
        resolveAuthRequest = resolve;
      });
      stubAuthFlow({ authRequest: () => pending as unknown as Response });
      // stubAuthFlow's Promise.resolve(...) around this value stays a
      // promise either way, so returning the pending promise itself works.

      renderWithProviders(<LoginPage />);

      expect(await screen.findByText("HMS")).toBeInTheDocument();
      expect(screen.getByText(/loading/i)).toBeInTheDocument();
      expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();

      resolveAuthRequest(authRequestOkResponse());
      expect(await screen.findByLabelText("Email")).toBeInTheDocument();
    });

    // Review finding 1 — this is the spec requirement the earlier
    // implementation missed: "Auth request unknown, expired or already
    // used → the form says the sign-in attempt expired and offers to
    // start again." A message with no control is not an offer.
    describe("when the auth request is unknown, expired, or already used", () => {
      it("shows the expired-attempt message instead of the credential form", async () => {
        stubAuthFlow({
          authRequest: () =>
            jsonResponse(400, {
              error: "auth_request_invalid",
              message: "this sign-in attempt has expired; start again",
            }),
        });

        renderWithProviders(<LoginPage />);

        expect(
          await screen.findByText(/this sign-in attempt has expired; start again/i),
        ).toBeInTheDocument();
        expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
        expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
      });

      // The offer must be a REAL control, not just wording — reusing
      // RedirectLanding's Sign in button, which starts a fresh
      // authorization request exactly as a direct /login visit would.
      it("offers a working Sign in button that starts a fresh authorization request", async () => {
        stubAuthFlow({
          authRequest: () =>
            jsonResponse(400, {
              error: "auth_request_invalid",
              message: "this sign-in attempt has expired; start again",
            }),
        });
        signinRedirect.mockResolvedValue(undefined);

        const { user } = renderWithProviders(<LoginPage />);
        const button = await screen.findByRole("button", { name: "Sign in" });
        await user.click(button);

        await waitFor(() =>
          expect(signinRedirect).toHaveBeenCalledWith(expect.objectContaining({ prompt: "login" })),
        );
      });

      // The expired-request wording and the sign-out wording must never
      // mix — RedirectLanding's message prop is meant to override the
      // sign-out greeting entirely, not append to it.
      it("does not also claim the user signed out", async () => {
        window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
        stubAuthFlow({
          authRequest: () =>
            jsonResponse(400, {
              error: "auth_request_invalid",
              message: "this sign-in attempt has expired; start again",
            }),
        });

        renderWithProviders(<LoginPage />);

        await screen.findByText(/this sign-in attempt has expired; start again/i);
        expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
      });
    });
  });
});
