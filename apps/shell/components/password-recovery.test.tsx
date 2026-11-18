import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PasswordRecovery } from "./password-recovery";

afterEach(() => vi.unstubAllGlobals());

it("prepares before password entry and never resubmits an uncertain password", async () => {
  let state = "ready";
  const calls: { url: string; body: Record<string, string> | undefined }[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ url, body });
    if (url.endsWith("/prepare")) {
      state = "prepared";
      return Response.json({ state });
    }
    if (url.endsWith("/password")) {
      state = "prepared";
      return Response.json({ error: "password_recovery_unavailable" }, { status: 503 });
    }
    if (url.endsWith("/cancel")) {
      state = "cancelled";
      return Response.json({ state });
    }
    return Response.json({ state });
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  await user.click(await screen.findByRole("button", { name: "Choose a new password" }));
  await user.type(await screen.findByLabelText("New password"), "Synthetic-secret-123!");
  await user.type(screen.getByLabelText("Confirm new password"), "Synthetic-secret-123!");
  await user.click(screen.getByRole("button", { name: "Set new password" }));
  await screen.findByText(/could not confirm the result/i);
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Check recovery status" }));
  await screen.findByRole("button", { name: "Cancel prepared attempt" });
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  expect(calls.filter((c) => c.url.endsWith("/password"))).toHaveLength(1);
  expect(calls.findIndex((c) => c.url.endsWith("/prepare"))).toBeLessThan(
    calls.findIndex((c) => c.url.endsWith("/password")),
  );
  await user.click(screen.getByRole("button", { name: "Cancel prepared attempt" }));
  await screen.findByRole("button", { name: "Choose a new password" });
  expect(calls.find((c) => c.url.endsWith("/cancel"))?.body).not.toHaveProperty("password");
});

it("restores a prepared attempt after reload and cancels using its HttpOnly marker", async () => {
  let state = "prepared";
  const cancel = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/cancel")) {
        cancel(JSON.parse(String(init?.body)));
        state = "cancelled";
      }
      return Response.json({ state });
    }),
  );
  const { user } = renderWithProviders(<PasswordRecovery />);
  await user.click(await screen.findByRole("button", { name: "Cancel prepared attempt" }));
  await waitFor(() => expect(cancel).toHaveBeenCalledWith({}));
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
});

it("verifies an eight-digit code and stops offering mail once verified", async () => {
  let verified = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/verify")) {
        verified = true;
        return Response.json({ state: "verified" });
      }
      if (init?.method === "POST") return Response.json({ state: "check_email" }, { status: 202 });
      return verified
        ? Response.json({ state: "ready" })
        : Response.json({ error: "password_recovery_expired" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.type(await screen.findByLabelText("Email"), "person@example.test");
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  const digits = within(
    await screen.findByRole("group", { name: "Recovery email code" }),
  ).getAllByRole("textbox");
  expect(digits).toHaveLength(8);
  for (let i = 0; i < 8; i++) await user.type(digits[i]!, String(i + 1));
  await screen.findByRole("button", { name: "Choose a new password" });
  expect(screen.queryByRole("button", { name: "Send a new code" })).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
});

it("shows completion only for an acknowledged completed receipt and returns to normal sign-in", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ state: "completed" })),
  );
  renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  expect(
    await screen.findByRole("link", { name: "Sign in with your new password" }),
  ).toHaveAttribute("href", "/login?authRequest=V2_test");
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
});

it("recovers a consumed code after its verification response was lost", async () => {
  let verified = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/verify")) {
        verified = true;
        return Response.json({ error: "password_recovery_unavailable" }, { status: 503 });
      }
      if (init?.method === "POST") return Response.json({ state: "check_email" }, { status: 202 });
      return verified
        ? Response.json({ state: "ready" })
        : Response.json({ error: "password_recovery_expired" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.type(await screen.findByLabelText("Email"), "person@example.test");
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  await screen.findByRole("group", { name: "Recovery email code" });
  await user.keyboard("12345678");
  await screen.findByText(/could not be verified/i);
  await user.click(screen.getByRole("button", { name: "Check recovery status" }));
  await screen.findByRole("button", { name: "Choose a new password" });
  expect(screen.queryByRole("button", { name: "Send a new code" })).not.toBeInTheDocument();
});

it("retains a missing prepared intent after reload until safe cancellation", async () => {
  let cancelled = false;
  const cancel = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/cancel")) {
        cancel(JSON.parse(String(init?.body)));
        cancelled = true;
        return Response.json({ state: "cancelled" });
      }
      return cancelled
        ? Response.json({ state: "cancelled" })
        : Response.json(
            { error: "password_recovery_command_unknown", message: "Check the retained command." },
            { status: 409 },
          );
    }),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.click(await screen.findByRole("button", { name: "Cancel prepared attempt" }));
  await waitFor(() => expect(cancel).toHaveBeenCalledWith({}));
  await screen.findByRole("button", { name: "Choose a new password" });
  expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
});

it("keeps submitted passwords out of query/mutation cache and browser storage", async () => {
  const secret = "Synthetic-secret-123!";
  let state = "ready";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/prepare")) {
        state = "prepared";
        return Response.json({ state });
      }
      if (url.endsWith("/password")) {
        state = "unconfirmed";
        return Response.json({ state });
      }
      return Response.json({ state });
    }),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={client}>
      <PasswordRecovery authRequestId="V2_test" />
    </QueryClientProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose a new password" }));
  await user.type(await screen.findByLabelText("New password"), secret);
  await user.type(screen.getByLabelText("Confirm new password"), secret);
  await user.click(screen.getByRole("button", { name: "Set new password" }));
  await screen.findByText(/awaiting confirmation/);
  const retained = JSON.stringify({
    mutations: client
      .getMutationCache()
      .getAll()
      .map((m) => m.state),
    queries: client
      .getQueryCache()
      .getAll()
      .map((q) => q.state),
    local: window.localStorage,
    session: window.sessionStorage,
  });
  expect(retained).not.toContain(secret);
  expect(screen.queryByDisplayValue(secret)).not.toBeInTheDocument();
  expect(
    client
      .getMutationCache()
      .getAll()
      .every((m) => m.state.variables === undefined),
  ).toBe(true);
});

it("retains a delivery ID on uncertain retry and changes it only for explicit resend", async () => {
  const requests: Record<string, string>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method !== "POST")
        return Response.json({ error: "password_recovery_expired" }, { status: 404 });
      requests.push(JSON.parse(String(init.body)));
      return requests.length === 1
        ? Response.json({ error: "password_recovery_unavailable" }, { status: 503 })
        : Response.json({ state: "check_email" }, { status: 202 });
    }),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.type(await screen.findByLabelText("Email"), "person@example.test");
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  await screen.findByText(/delivery could not be confirmed/i);
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  await user.click(await screen.findByRole("button", { name: "Send a new code" }));
  await waitFor(() => expect(requests).toHaveLength(3));
  expect(requests[1]).toEqual(requests[0]);
  expect(requests[2]?.request_id).not.toBe(requests[0]?.request_id);
  expect(requests[2]?.email).toBe(requests[0]?.email);
});
