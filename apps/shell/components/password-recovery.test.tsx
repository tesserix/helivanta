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
  expect(await screen.findByText("Your email is verified. Choose a new password.")).toBeVisible();
  await user.click(await screen.findByRole("button", { name: "Choose a new password" }));
  await user.type(await screen.findByLabelText("New password"), "Synthetic-secret-123!");
  await user.type(screen.getByLabelText("Confirm new password"), "Synthetic-secret-123!");
  await user.click(screen.getByRole("button", { name: "Set new password" }));
  expect(
    await screen.findByText(
      "We couldn't confirm whether that worked. Select Check again before you continue. Your password won't be sent again automatically.",
    ),
  ).toBeVisible();
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Check again" }));
  await screen.findByRole("button", { name: "Cancel and start again" });
  expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  expect(calls.filter((c) => c.url.endsWith("/password"))).toHaveLength(1);
  expect(calls.findIndex((c) => c.url.endsWith("/prepare"))).toBeLessThan(
    calls.findIndex((c) => c.url.endsWith("/password")),
  );
  await user.click(screen.getByRole("button", { name: "Cancel and start again" }));
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
  expect(
    await screen.findByText(
      "You started a new password but it wasn't saved. Cancel it to start again.",
    ),
  ).toBeVisible();
  await user.click(await screen.findByRole("button", { name: "Cancel and start again" }));
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
  expect(
    screen.getByText(
      "You can ask for up to three codes an hour and ten a day. If the email doesn't arrive, check your spam folder.",
    ),
  ).toBeVisible();
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
  await user.click(screen.getByRole("button", { name: "Check again" }));
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
  await screen.findByRole("button", { name: "Cancel and start again" });
  expect(screen.getAllByRole("button", { name: "Check again" })).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Cancel and start again" }));
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
  expect(
    await screen.findByText(
      "Your new password is still being saved. Select Check again, or try signing in with it. If this doesn't clear, contact your organisation's administrator.",
    ),
  ).toBeVisible();
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
  expect(
    await screen.findByText(
      "We couldn't confirm the code was sent. Try sending it again before asking for a new code.",
    ),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  await user.click(await screen.findByRole("button", { name: "Send a new code" }));
  await waitFor(() => expect(requests).toHaveLength(3));
  expect(requests[1]).toEqual(requests[0]);
  expect(requests[2]?.request_id).not.toBe(requests[0]?.request_id);
  expect(requests[2]?.email).toBe(requests[0]?.email);
});

// #1855: the forms validate with zod only and tie each error to its input.
it("refuses an invalid email inline, tied to the input, without a request", async () => {
  const fetch = vi.fn(async () =>
    Response.json({ error: "password_recovery_expired" }, { status: 404 }),
  );
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  const email = await screen.findByLabelText("Email");
  expect(
    screen.getByText(
      "Enter the email for your account in this organisation. We'll send a code to check it's yours.",
    ),
  ).toBeVisible();
  expect(email).toHaveAttribute("type", "text");
  expect(email).toHaveAttribute("inputmode", "email");
  expect(email).toHaveAttribute("autocomplete", "email");
  expect(email.closest("form")).toHaveAttribute("novalidate");
  const reads = fetch.mock.calls.length;
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  expect(await screen.findByText("Enter a valid email address.")).toHaveAttribute(
    "id",
    "recovery-email-error",
  );
  expect(email).toHaveAttribute("aria-invalid", "true");
  expect(email).toHaveAttribute("aria-describedby", "recovery-email-error");
  await user.type(email, "not-an-email");
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  expect(email).toHaveAttribute("aria-invalid", "true");
  expect(fetch.mock.calls.slice(reads)).toEqual([]);
});

it("refuses an empty or mismatched new password inline without submitting it", async () => {
  let state = "ready";
  const fetch = vi.fn(async (url: string) => {
    if (url.endsWith("/prepare")) state = "prepared";
    return Response.json({ state });
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.click(await screen.findByRole("button", { name: "Choose a new password" }));
  const password = await screen.findByLabelText("New password");
  const confirm = screen.getByLabelText("Confirm new password");
  expect(screen.getByText("Your organisation’s password policy applies.")).toBeVisible();
  expect(password.closest("form")).toHaveAttribute("novalidate");
  for (const input of [password, confirm]) expect(input).not.toHaveAttribute("maxlength");
  await user.click(screen.getByRole("button", { name: "Set new password" }));
  expect(await screen.findByText("Enter a new password using up to 200 characters.")).toBeVisible();
  expect(password).toHaveAttribute("aria-invalid", "true");
  expect(password).toHaveAttribute("aria-describedby", "recovery-password-error");
  await user.type(password, "Synthetic-secret-123!");
  await user.type(confirm, "Different-secret-456!");
  await user.click(screen.getByRole("button", { name: "Set new password" }));
  expect(await screen.findByText("The passwords must match.")).toBeVisible();
  expect(confirm).toHaveAttribute("aria-invalid", "true");
  expect(confirm).toHaveAttribute("aria-describedby", "recovery-confirm-error");
  expect(fetch.mock.calls.filter(([url]) => url.endsWith("/password"))).toEqual([]);
});

// #1856: the plain-language copy for states the flows above do not reach.
it("offers another password after the policy refuses one", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ state: "refused" })),
  );
  renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  expect(
    await screen.findByText(
      "That password doesn’t meet your organisation’s password policy. Choose another one.",
    ),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "Choose a new password" })).toBeVisible();
});

it("asks to check again when preparing a password cannot be confirmed", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("/prepare")
        ? Response.json({ error: "password_recovery_unavailable" }, { status: 503 })
        : Response.json({ state: "ready" }),
    ),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.click(await screen.findByRole("button", { name: "Choose a new password" }));
  expect(
    await screen.findByText(
      "We couldn't confirm whether that worked. Select Check again before you continue.",
    ),
  ).toBeVisible();
});

it("says the new password may already be saving when cancelling fails", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("/cancel")
        ? Response.json({ error: "password_recovery_unavailable" }, { status: 503 })
        : Response.json({ state: "prepared" }),
    ),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.click(await screen.findByRole("button", { name: "Cancel and start again" }));
  expect(
    await screen.findByText(
      "We couldn't cancel it. Select Check again: your new password may already be saving.",
    ),
  ).toBeVisible();
});

it("waits for the verified status after the code is accepted", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/verify")) return Response.json({ state: "verified" });
      if (init?.method === "POST") return Response.json({ state: "check_email" }, { status: 202 });
      return Response.json({ error: "password_recovery_expired" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.type(await screen.findByLabelText("Email"), "person@example.test");
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  await screen.findByRole("group", { name: "Recovery email code" });
  await user.keyboard("12345678");
  expect(
    await screen.findByText("Your email is verified. Select Check again to continue."),
  ).toBeVisible();
  expect(
    screen.getByText(
      "We can't see your verified code yet. If you've already entered it, select Check again.",
    ),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "Check again" })).toBeVisible();
});

it("offers to send the same code again when a resend cannot be confirmed", async () => {
  let posts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method !== "POST")
        return Response.json({ error: "password_recovery_expired" }, { status: 404 });
      posts += 1;
      return posts === 1
        ? Response.json({ state: "check_email" }, { status: 202 })
        : Response.json({ error: "password_recovery_unavailable" }, { status: 503 });
    }),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.type(await screen.findByLabelText("Email"), "person@example.test");
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  await user.click(await screen.findByRole("button", { name: "Send a new code" }));
  expect(await screen.findByRole("button", { name: "Try sending again" })).toBeVisible();
});

it("explains the code limit when too many codes were requested", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) =>
      init?.method === "POST"
        ? Response.json({ error: "password_recovery_rate_limited" }, { status: 429 })
        : Response.json({ error: "password_recovery_expired" }, { status: 404 }),
    ),
  );
  const { user } = renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  await user.type(await screen.findByLabelText("Email"), "person@example.test");
  await user.click(screen.getByRole("button", { name: "Send recovery code" }));
  expect(
    await screen.findByText(
      "You've asked for too many codes. Wait a while, then try again. You can ask for up to three codes an hour and ten a day.",
    ),
  ).toBeVisible();
});

it("says nothing changed when the recovery status cannot be read", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ error: "password_recovery_unavailable" }, { status: 503 })),
  );
  renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  expect(
    await screen.findByText("We couldn't check your recovery. Your password hasn't been changed."),
  ).toBeVisible();
  expect(
    screen.getByText(
      "We can't see where your recovery is up to. Select Check again before you continue.",
    ),
  ).toBeVisible();
});

it("sends a reader without a sign-in attempt back to start sign-in", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ error: "password_recovery_expired" }, { status: 404 })),
  );
  renderWithProviders(<PasswordRecovery />);
  expect(
    await screen.findByText(
      "Start sign-in and choose your organisation first, then select Forgot password?",
    ),
  ).toBeVisible();
  expect(screen.getByRole("link", { name: "Start sign-in" })).toHaveAttribute("href", "/login");
});

it("points a locked account at its administrator", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ state: "locked" })),
  );
  renderWithProviders(<PasswordRecovery authRequestId="V2_test" />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This account is locked. Contact your organisation's administrator. If you own the workspace, contact Helivanta support.",
  );
});
