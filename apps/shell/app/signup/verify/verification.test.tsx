import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import { SignupVerification } from "./verification";

const signinRedirect = vi.hoisted(() => vi.fn());
vi.mock("@/lib/oidc", () => ({ getUserManager: () => ({ signinRedirect }) }));

async function setPassword(
  user: ReturnType<typeof userEvent.setup>,
  secret = "Long-enough-pass-1!",
) {
  await user.type(await screen.findByLabelText("New password"), secret);
  await user.type(screen.getByLabelText("Confirm password"), secret);
  await user.click(screen.getByRole("button", { name: "Set password" }));
}

beforeEach(() => {
  // oidc-client-ts resolves once the browser is navigating away.
  signinRedirect.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.unstubAllGlobals();
  signinRedirect.mockReset();
  window.history.replaceState(null, "", "/");
});

it.each([
  "phase=email&userId=..%2Fother&code=proof",
  `phase=email&userId=${"x".repeat(201)}&code=proof`,
  `phase=email&userId=owner-synthetic&code=${"x".repeat(21)}`,
])("rejects malformed proof fragments before any request: %s", async (fragment) => {
  window.history.replaceState(null, "", `/signup/verify#${fragment}`);
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<SignupVerification />);
  expect(await screen.findByRole("heading", { name: "Open your latest email link" })).toBeVisible();
  expect(fetcher).not.toHaveBeenCalled();
  expect(window.location.hash).toBe("");
});

it("shows already verified for a reopened owner-bound email link without consuming it again", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=used-proof",
  );
  const fetcher = vi.fn(async (path: string) => {
    expect(path).toBe("/api/v1/auth/signup?userId=owner-synthetic");
    return new Response(JSON.stringify({ state: "password_action_required" }), { status: 200 });
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<SignupVerification />);
  expect(
    await screen.findByRole("heading", { name: "Your email is already verified" }),
  ).toBeVisible();
  expect(screen.queryByRole("button", { name: "Verify email" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Send password link" })).toBeVisible();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(fetcher.mock.calls[0][0]).toBe("/api/v1/auth/signup?userId=owner-synthetic");
  expect(window.location.hash).toBe("");
});

// #1763 D4: the password was set moments ago, so sign-in starts at once, pinned
// to this workspace with prompt=login, instead of a countdown and a button.
it("starts the workspace-pinned sign-in at once for an already set password", async () => {
  const tenantId = "11111111-1111-4111-8111-111111111111";
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=password&userId=owner-synthetic&code=used-proof",
  );
  const fetcher = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          state: "ready",
          tenantId,
          name: "QA",
          kind: "hospital",
          resourceId: "1aee9771-66c3-445c-8b3e-b18f472ca409",
        }),
        { status: 200 },
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<SignupVerification />);
  expect(await screen.findByRole("heading", { name: "Signing you in" })).toBeVisible();
  await waitFor(() => expect(signinRedirect).toHaveBeenCalledTimes(1));
  expect(signinRedirect).toHaveBeenCalledWith(
    expect.objectContaining({
      prompt: "login",
      scope: `openid profile email urn:zitadel:iam:org:id:helivanta-${tenantId}`,
      state: { purpose: "customer-signup-login", tenantId },
    }),
  );
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("does not offer credential actions when the owner status is unavailable", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=proof",
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ error: "unavailable" }), { status: 503 })),
  );
  renderWithProviders(<SignupVerification />);
  expect(await screen.findByRole("alert")).toHaveTextContent("could not check this link's status");
  expect(screen.queryByRole("button", { name: "Verify email" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Send a new link" })).not.toBeInTheDocument();
});

it.each([
  ["provisioning", "Setting up your workspace"],
  ["needs_support", "Setup needs a hand"],
])("does not reopen password setup while the signup is %s", async (state, heading) => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=password&userId=owner-synthetic&code=used-proof",
  );
  const fetcher = vi.fn(async () => new Response(JSON.stringify({ state }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<SignupVerification />);
  expect(await screen.findByRole("heading", { name: heading })).toBeVisible();
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Send a new link" })).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Sign in now" })).not.toBeInTheDocument();
  // #1767: no link back to /signup, which only showed the old button.
  expect(screen.queryByRole("link", { name: /workspace setup/ })).not.toBeInTheDocument();
  if (state === "provisioning")
    expect(screen.getByRole("status")).toHaveTextContent("up to 2 minutes");
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("removes the native email code from the address bar before submitting it", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=synthetic-proof",
  );
  const requests: Array<{ path: string; body: unknown; fragment: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      if (path.includes("?userId="))
        return new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 });
      requests.push({ path, body: JSON.parse(String(init.body)), fragment: window.location.hash });
      return new Response(JSON.stringify({ state: "provisioning" }), { status: 200 });
    }),
  );
  renderWithProviders(<SignupVerification />);
  await waitFor(() => expect(window.location.hash).toBe(""));
  expect(requests).toHaveLength(0);
  // One form, one request (#1763): the email link sets the password too.
  expect(await screen.findByRole("heading", { name: "Set your password" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Verify email" })).not.toBeInTheDocument();
  await setPassword(userEvent.setup());
  await waitFor(() =>
    expect(requests).toEqual([
      {
        path: "/api/v1/auth/signup/verify-email",
        body: {
          userId: "owner-synthetic",
          code: "synthetic-proof",
          password: "Long-enough-pass-1!",
        },
        fragment: "",
      },
    ]),
  );
  expect(await screen.findByRole("heading", { name: "Setting up your workspace" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Send password link" })).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: /is ready$/ })).not.toBeInTheDocument();
});

it("sets the password through the Global API with the native reset proof", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=password&userId=owner-synthetic&code=synthetic-reset",
  );
  const requests: Array<{ path: string; body: unknown }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      if (path.includes("?userId="))
        return new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 });
      requests.push({ path, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify({ state: "provisioning" }), { status: 200 });
    }),
  );
  renderWithProviders(<SignupVerification />);
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText("New password"), "Synthetic Password 42!");
  await user.type(screen.getByLabelText("Confirm password"), "Synthetic Password 42!");
  await user.click(screen.getByRole("button", { name: "Set password" }));
  await waitFor(() =>
    expect(requests).toEqual([
      {
        path: "/api/v1/auth/signup/password",
        body: {
          userId: "owner-synthetic",
          code: "synthetic-reset",
          password: "Synthetic Password 42!",
        },
      },
    ]),
  );
  expect(await screen.findByRole("heading", { name: "Setting up your workspace" })).toBeVisible();
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  expect(window.location.hash).toBe("");
});

it("an expired email link says so after the form is submitted and offers a new link", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=expired-proof",
  );
  const requests: Array<{ path: string; body: unknown }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      if (path.includes("?userId="))
        return new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 });
      requests.push({ path, body: JSON.parse(String(init.body)) });
      return path.endsWith("verify-email")
        ? new Response(JSON.stringify({ error: "invalid_request", message: "Invalid proof" }), {
            status: 400,
          })
        : new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 });
    }),
  );
  renderWithProviders(<SignupVerification />);
  // #1762: the email limits are told only on refusal, never as preamble.
  expect(await screen.findByRole("heading", { name: "Set your password" })).toBeVisible();
  expect(screen.queryByText(/emails per hour/)).not.toBeInTheDocument();
  const user = userEvent.setup();
  await setPassword(user);
  expect(await screen.findByRole("alert")).toHaveTextContent(/expired or was already used/);
  await user.click(screen.getByRole("button", { name: "Send a new link" }));
  await waitFor(() =>
    expect(requests).toEqual([
      {
        path: "/api/v1/auth/signup/verify-email",
        body: { userId: "owner-synthetic", code: "expired-proof", password: "Long-enough-pass-1!" },
      },
      { path: "/api/v1/auth/signup/email-link", body: { userId: "owner-synthetic" } },
    ]),
  );
});

// #1763: a submitted email link verifies the address and sets the password in
// one request. The page never asks for a password link afterwards; it waits
// for the workspace and signs in.
it("needs no password link after the one-step form and signs in once the workspace is ready", async () => {
  const tenantId = "11111111-1111-4111-8111-111111111111";
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=valid-proof",
  );
  let proven = false;
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path.includes("?userId="))
        return new Response(
          JSON.stringify(
            proven
              ? {
                  state: "ready",
                  tenantId,
                  name: "QA",
                  kind: "hospital",
                  resourceId: "1aee9771-66c3-445c-8b3e-b18f472ca409",
                }
              : { state: "email_action_required" },
          ),
          { status: 200 },
        );
      requests.push(path);
      proven = true;
      return new Response(JSON.stringify({ state: "provisioning" }), { status: 202 });
    }),
  );
  renderWithProviders(<SignupVerification />);
  await setPassword(userEvent.setup());
  expect(await screen.findByRole("heading", { name: "Signing you in" })).toBeVisible();
  await waitFor(() => expect(signinRedirect).toHaveBeenCalledTimes(1));
  expect(screen.queryByRole("button", { name: "Send password link" })).not.toBeInTheDocument();
  expect(requests).toEqual(["/api/v1/auth/signup/verify-email"]);
});

// #1763 D3: a password the identity owner refuses leaves the link intact. The
// form says why, keeps the code, and the next submit goes out again.
it("keeps the link and asks again when the password is refused by policy", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=valid-proof",
  );
  const bodies: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.includes("?userId="))
        return new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 });
      bodies.push(JSON.parse(String(init?.body)));
      return bodies.length === 1
        ? new Response(JSON.stringify({ error: "password_policy", message: "weak" }), {
            status: 422,
          })
        : new Response(JSON.stringify({ state: "provisioning" }), { status: 202 });
    }),
  );
  renderWithProviders(<SignupVerification />);
  const user = userEvent.setup();
  await setPassword(user, "short");
  expect(await screen.findByRole("alert")).toHaveTextContent(/password rules/);
  expect(screen.queryByText(/expired or was already used/)).not.toBeInTheDocument();
  await setPassword(user, "Long-enough-pass-1!");
  expect(await screen.findByRole("heading", { name: "Setting up your workspace" })).toBeVisible();
  expect(bodies).toEqual([
    { userId: "owner-synthetic", code: "valid-proof", password: "short" },
    { userId: "owner-synthetic", code: "valid-proof", password: "Long-enough-pass-1!" },
  ]);
});

// #1767: the password form has one button. "Send a new link" exists only
// after the link was refused, as a quiet text link.
it("shows one button on the password form until the link is refused", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=expired-proof",
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path.includes("?userId=")
        ? new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 })
        : new Response(JSON.stringify({ error: "invalid_request", message: "x" }), { status: 400 }),
    ),
  );
  renderWithProviders(<SignupVerification />);
  expect(await screen.findByRole("heading", { name: "Set your password" })).toBeVisible();
  expect(screen.getAllByRole("button")).toHaveLength(1);
  await setPassword(userEvent.setup());
  expect(await screen.findByRole("button", { name: "Send a new link" })).toBeVisible();
});
