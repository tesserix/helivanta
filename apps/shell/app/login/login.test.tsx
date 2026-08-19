import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { SIGNED_OUT_MARK, IDLE_ENDED_MARK } from "@helivanta/ui";

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
// Includes `policies` (#867, spec D5/D8): GET /v1/auth/login/request/:id
// now answers with the neutral policy subset CredentialForm maps onto
// @tesserix/web's AuthMethodPolicy. A TOTP-capable org is the default
// here so the OTP-step tests below don't each have to restate it.
function authRequestOkResponse() {
  return jsonResponse(200, {
    id: AUTH_REQUEST_ID,
    client_id: "helivanta-web",
    redirect_uri: "http://localhost:4301/api/auth/callback",
    scope: ["openid", "profile", "email"],
    policies: {
      allow_password: true,
      require_mfa: false,
      second_factors: ["totp"],
      ignore_unknown_usernames: false,
    },
  });
}

// Routes a stubbed fetch by path: GET .../auth/login/request/:id, POST
// .../auth/login/password, and (#867) POST .../auth/login/factor, since
// ValidatedCredentialForm now calls the first before CredentialForm ever
// calls the second, and OtpStep only ever calls the third after a
// `factor_required` outcome from the second. Defaults all three to a
// success response so a test only has to override the call it cares
// about.
function stubAuthFlow(
  opts: { authRequest?: () => Response; password?: () => Response; factor?: () => Response } = {},
) {
  const fetchMock = vi.fn((url: string) => {
    if (url.includes("/auth/login/request/")) {
      return Promise.resolve((opts.authRequest ?? authRequestOkResponse)());
    }
    if (url.includes("/auth/login/factor")) {
      return Promise.resolve(
        (
          opts.factor ??
          (() => jsonResponse(200, { callback_url: "https://hms.example/api/auth/callback" }))
        )(),
      );
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

    // CRITICAL review finding: a stale SIGNED_OUT_MARK from an earlier,
    // uncompleted sign-out (the redirect to /login never completed in
    // this tab) can coexist with a LATER idle teardown's IDLE_ENDED_MARK
    // — they are not mutually exclusive, and the previous implementation
    // (endZitadelSession() setting SIGNED_OUT_MARK unconditionally on
    // every call) made it the NORMAL case rather than a rare edge case.
    // Idle wins the first render (correct), but the stale SIGNED_OUT_MARK
    // must not survive to lie on the SECOND: a clinician who was idled
    // out and then reloads /login must see the neutral greeting, never
    // "You are signed out" for a sign-out that did not happen this time.
    it("does not let a stale signed-out mark survive an idle-ended render and lie on the next one", async () => {
      window.sessionStorage.setItem(IDLE_ENDED_MARK, "1");
      window.sessionStorage.setItem(SIGNED_OUT_MARK, "1");
      const first = renderWithProviders(<LoginPage />);
      expect(await screen.findByText(/ended after a period of inactivity/i)).toBeInTheDocument();
      expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
      first.unmount();

      renderWithProviders(<LoginPage />);
      expect(screen.getByText(/sign in to continue/i)).toBeInTheDocument();
      expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/ended after a period of inactivity/i)).not.toBeInTheDocument();
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
      expect(await screen.findByLabelText("Email or username")).toBeInTheDocument();
      expect(screen.getByLabelText("Password")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    });

    // Repo-wide: native browser validation is banned (docs/standards/
    // frontend.md §4), so every Helivanta form must disable it explicitly. This
    // pins the login form to that rule rather than relying on the zod
    // validation tests below to prove it only indirectly.
    it("disables native browser validation", async () => {
      stubAuthFlow();
      renderWithProviders(<LoginPage />);
      const email = await screen.findByLabelText("Email or username");
      expect(email.closest("form")).toHaveAttribute("novalidate");
    });

    it("shows an inline error and does not submit when the login name is empty", async () => {
      const fetchMock = stubAuthFlow();

      const { user } = renderWithProviders(<LoginPage />);
      await user.click(await screen.findByRole("button", { name: "Sign in" }));

      expect(await screen.findByText(/enter your email or username/i)).toBeInTheDocument();
      // The auth-request GET is allowed (it is how the form got here at
      // all); the credential POST specifically must never have fired.
      expect(fetchMock).not.toHaveBeenCalledWith("/api/v1/auth/login/password", expect.anything());
    });

    // #899. The field carries a Zitadel LOGIN NAME, and Zitadel derives
    // login names from `userName` — so `dr.patel` is a valid, reachable
    // account on an instance whose org domain policy does not force a
    // domain suffix, which this one does not.
    //
    // The schema previously carried `.email()`, which refused such a login
    // name in the browser and never sent the request: the operator read
    // "Enter a valid email address" from Helivanta while holding
    // credentials Zitadel would have accepted. This asserts on the REQUEST
    // actually leaving — the value reaching the API is the only thing that
    // proves the client stopped substituting its own judgement for the
    // identity provider's. Asserting merely that the error text is absent
    // would pass just as well if the form silently swallowed the submit.
    it("submits a login name that is not an email address", async () => {
      const fetchMock = stubAuthFlow();
      vi.stubGlobal("location", { ...window.location, assign: vi.fn() });

      const { user } = renderWithProviders(<LoginPage />);
      await user.type(await screen.findByLabelText("Email or username"), "dr.patel");
      await user.type(screen.getByLabelText("Password"), "correct-horse");
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/login/password",
          expect.objectContaining({
            method: "POST",
            body: JSON.stringify({
              auth_request_id: AUTH_REQUEST_ID,
              login_name: "dr.patel",
              password: "correct-horse",
            }),
          }),
        ),
      );
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
      await user.type(await screen.findByLabelText("Email or username"), "clinician@helivanta.dev");
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
      await user.type(await screen.findByLabelText("Email or username"), "clinician@helivanta.dev");
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
      await user.type(await screen.findByLabelText("Email or username"), "clinician@helivanta.dev");
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
      await user.type(await screen.findByLabelText("Email or username"), "clinician@helivanta.dev");
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
      await user.type(await screen.findByLabelText("Email or username"), "clinician@helivanta.dev");
      await user.type(screen.getByLabelText("Password"), "correct-password");
      await user.click(screen.getByRole("button", { name: "Sign in" }));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/login/password",
          expect.objectContaining({
            method: "POST",
            body: JSON.stringify({
              auth_request_id: AUTH_REQUEST_ID,
              login_name: "clinician@helivanta.dev",
              password: "correct-password",
            }),
          }),
        ),
      );
    });

    it("verifies the auth request first, via GET /v1/auth/login/request/:id, before rendering the form", async () => {
      const fetchMock = stubAuthFlow();
      renderWithProviders(<LoginPage />);

      await screen.findByLabelText("Email or username");
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

      expect(await screen.findByText("Helivanta")).toBeInTheDocument();
      expect(screen.getByText(/loading/i)).toBeInTheDocument();
      expect(screen.queryByLabelText("Email or username")).not.toBeInTheDocument();

      resolveAuthRequest(authRequestOkResponse());
      expect(await screen.findByLabelText("Email or username")).toBeInTheDocument();
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
        expect(screen.queryByLabelText("Email or username")).not.toBeInTheDocument();
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

    // #867, spec D1/D8: the native TOTP step, reachable only after a
    // `factor_required` outcome from POST /v1/auth/login/password.
    describe("when the org requires a native second factor (OTP step)", () => {
      async function submitCorrectCredentials(
        user: ReturnType<typeof renderWithProviders>["user"],
      ) {
        await user.type(await screen.findByLabelText("Email or username"), "clinician@helivanta.dev");
        await user.type(screen.getByLabelText("Password"), "correct-password");
        await user.click(screen.getByRole("button", { name: "Sign in" }));
      }

      // The same trap `handoff_url` documents (spec D3 of the login-client
      // spec, restated for this outcome by spec D8): `factor_required`
      // means the password was CORRECT. Rendering it as an error would
      // send a clinician to reset a working password — this asserts the
      // OTP step renders and NO alert/error role appears alongside it.
      it("shows the OTP step, not an error, when a factor is required", async () => {
        stubAuthFlow({
          password: () => jsonResponse(200, { factor_required: ["totp"] }),
        });

        const { user } = renderWithProviders(<LoginPage />);
        await submitCorrectCredentials(user);

        expect(await screen.findByLabelText(/verification code/i)).toBeInTheDocument();
        expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      });

      // Review finding 1, fix round 1: unlike AuthCredentialForm,
      // AuthOtpStep does NOT set `noValidate` on its own <form> — and its
      // <input> carries `pattern="\d{6}"` plus `maxLength`, both native-
      // validation attributes. Without page.tsx passing `noValidate`
      // through explicitly, pressing Enter on an incomplete code fires
      // the browser's native validation popup, which docs/standards/
      // frontend.md bans outright. Mirrors the credential-form pin above
      // (`disables native browser validation`) rather than relying on
      // that other test to prove this by proxy.
      it("disables native browser validation on the OTP step", async () => {
        stubAuthFlow({
          password: () => jsonResponse(200, { factor_required: ["totp"] }),
        });

        const { user } = renderWithProviders(<LoginPage />);
        await submitCorrectCredentials(user);

        const codeField = await screen.findByLabelText(/verification code/i);
        expect(codeField.closest("form")).toHaveAttribute("novalidate");
      });

      it("submits the code to POST /v1/auth/login/factor and navigates on a completed login", async () => {
        const fetchMock = stubAuthFlow({
          password: () => jsonResponse(200, { factor_required: ["totp"] }),
          factor: () =>
            jsonResponse(200, { callback_url: "https://hms.example/api/auth/callback?code=abc" }),
        });
        const assignSpy = vi.fn();
        vi.stubGlobal("location", { ...window.location, assign: assignSpy });

        const { user } = renderWithProviders(<LoginPage />);
        await submitCorrectCredentials(user);

        const codeField = await screen.findByLabelText(/verification code/i);
        await user.type(codeField, "123456");

        await waitFor(() =>
          expect(fetchMock).toHaveBeenCalledWith(
            "/api/v1/auth/login/factor",
            expect.objectContaining({
              method: "POST",
              body: JSON.stringify({
                auth_request_id: AUTH_REQUEST_ID,
                factor: "totp",
                code: "123456",
              }),
            }),
          ),
        );
        await waitFor(() =>
          expect(assignSpy).toHaveBeenCalledWith("https://hms.example/api/auth/callback?code=abc"),
        );
      });

      // Spec D5/D6: a wrong TOTP code answers with the SAME shared
      // refusal wording a wrong password does, and — unlike an expired
      // attempt — the clinician stays on the OTP step to retry (spec D6
      // allows five wrong codes before exhaustion).
      it("shows the shared refusal wording and keeps the OTP step visible for a wrong code", async () => {
        stubAuthFlow({
          password: () => jsonResponse(200, { factor_required: ["totp"] }),
          factor: () =>
            jsonResponse(401, {
              error: "invalid_credentials",
              message: "email or password is incorrect",
            }),
        });

        const { user } = renderWithProviders(<LoginPage />);
        await submitCorrectCredentials(user);

        const codeField = await screen.findByLabelText(/verification code/i);
        await user.type(codeField, "000000");

        const alert = await screen.findByRole("alert");
        expect(alert).toHaveTextContent(/email or password is incorrect/i);
        expect(screen.getByLabelText(/verification code/i)).toBeInTheDocument();
      });

      // A missing/expired/exhausted login_attempt row (spec D6) returns
      // the clinician to the SAME "start again" landing an expired auth
      // request renders — not a second, differently-worded dead end.
      it("returns to the start-again landing when the login attempt has expired", async () => {
        stubAuthFlow({
          password: () => jsonResponse(200, { factor_required: ["totp"] }),
          factor: () =>
            jsonResponse(400, {
              error: "auth_request_invalid",
              message: "this sign-in attempt has expired; start again",
            }),
        });

        const { user } = renderWithProviders(<LoginPage />);
        await submitCorrectCredentials(user);

        const codeField = await screen.findByLabelText(/verification code/i);
        await user.type(codeField, "000000");

        expect(
          await screen.findByText(/this sign-in attempt has expired; start again/i),
        ).toBeInTheDocument();
        expect(screen.queryByLabelText(/verification code/i)).not.toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
      });

      // Review finding 2, fix round 1: the `handoff` branch exists in
      // checkFactor's type and in OtpStep's onSuccess switch (page.tsx),
      // but was untested — exactly the shape a future refactor collapses
      // into `default: showError`, the same trap the PASSWORD step's own
      // handoff test (`navigates to the handoff url...` above) exists to
      // guard against. Mirrors that test: a verified TOTP code can still
      // hand off (loginui.go's `Factor` handler: CompleteAfterFactor
      // re-runs the uncollectible/enrolled checks and can find the
      // session insufficient even after a CORRECT code — e.g. enrollment
      // changed between the password step and this one), and that is NOT
      // a failure — no alert, straight navigation.
      it("navigates to the handoff url when a verified factor still needs Zitadel's hosted login", async () => {
        stubAuthFlow({
          password: () => jsonResponse(200, { factor_required: ["totp"] }),
          factor: () =>
            jsonResponse(200, {
              handoff_url: "http://localhost:20080/ui/v2/login?authRequest=V2_test-auth-request",
            }),
        });
        const assignSpy = vi.fn();
        vi.stubGlobal("location", { ...window.location, assign: assignSpy });

        const { user } = renderWithProviders(<LoginPage />);
        await submitCorrectCredentials(user);

        const codeField = await screen.findByLabelText(/verification code/i);
        await user.type(codeField, "123456");

        await waitFor(() =>
          expect(assignSpy).toHaveBeenCalledWith(
            "http://localhost:20080/ui/v2/login?authRequest=V2_test-auth-request",
          ),
        );
        // Not treated as a refusal — no shared-refusal alert rendered,
        // same assertion the password-step handoff test makes.
        expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      });
    });
  });
});
