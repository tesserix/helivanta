import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import { SignupVerification } from "./verification";

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
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
      requests.push({ path, body: JSON.parse(String(init.body)), fragment: window.location.hash });
      return new Response(JSON.stringify({ state: "provisioning" }), { status: 200 });
    }),
  );
  renderWithProviders(<SignupVerification />);
  await waitFor(() => expect(window.location.hash).toBe(""));
  expect(requests).toHaveLength(0);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Verify email address" }));
  await waitFor(() =>
    expect(requests).toEqual([
      {
        path: "/api/v1/auth/signup/verify-email",
        body: { userId: "owner-synthetic", code: "synthetic-proof" },
        fragment: "",
      },
    ]),
  );
  expect(await screen.findByRole("button", { name: "Send password setup email" })).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "Your workspace is ready" }),
  ).not.toBeInTheDocument();
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
  expect(await screen.findByRole("heading", { name: "Your password is set" })).toBeVisible();
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  expect(window.location.hash).toBe("");
});

it("explains link lifetime and lets an expired email link request verification without a password reset", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=expired-proof",
  );
  const requests: Array<{ path: string; body: unknown }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      requests.push({ path, body: JSON.parse(String(init.body)) });
      return path.endsWith("verify-email")
        ? new Response(JSON.stringify({ error: "invalid_request", message: "Invalid proof" }), {
            status: 400,
          })
        : new Response(JSON.stringify({ state: "email_action_required" }), { status: 200 });
    }),
  );
  renderWithProviders(<SignupVerification />);
  expect(await screen.findByText(/Links expire after 1 hour/)).toBeVisible();
  expect(screen.getByText(/3 emails per hour and 10 per 24 hours/)).toBeVisible();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Verify email address" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/expired|fresh email link/);
  await user.click(screen.getByRole("button", { name: "Request a fresh email link" }));
  await waitFor(() =>
    expect(requests).toEqual([
      {
        path: "/api/v1/auth/signup/verify-email",
        body: { userId: "owner-synthetic", code: "expired-proof" },
      },
      { path: "/api/v1/auth/signup/email-link", body: { userId: "owner-synthetic" } },
    ]),
  );
});

it("keeps expiry guidance after verification and describes a password-link request without promising delivery", async () => {
  window.history.replaceState(
    null,
    "",
    "/signup/verify#phase=email&userId=owner-synthetic&code=valid-proof",
  );
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      requests.push(path);
      return new Response(
        JSON.stringify({
          state: path.endsWith("verify-email")
            ? "password_action_required"
            : "email_action_required",
        }),
        { status: 202 },
      );
    }),
  );
  renderWithProviders(<SignupVerification />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Verify email address" }));
  expect(await screen.findByRole("heading", { name: "Your email is verified" })).toBeVisible();
  expect(screen.getByText(/Links expire after 1 hour/)).toBeVisible();
  expect(screen.getByText(/3 emails per hour and 10 per 24 hours/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Send password setup email" }));
  expect(await screen.findByRole("status")).toHaveTextContent(
    "If this request can receive an email",
  );
  expect(screen.getByRole("button", { name: "Send password setup email" })).toBeEnabled();
  expect(requests).toEqual([
    "/api/v1/auth/signup/verify-email",
    "/api/v1/auth/signup/password-link",
  ]);
});
