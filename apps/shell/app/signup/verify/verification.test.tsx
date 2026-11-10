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
