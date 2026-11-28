import { afterEach, expect, it, vi } from "vitest";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import { StaffAcceptance } from "./acceptance";

const navigation = vi.hoisted(() => ({ replace: vi.fn(), push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));
const signin = vi.hoisted(() => ({ signinAfterStaffSetup: vi.fn(async () => {}) }));
vi.mock("@/lib/signup-signin", () => signin);

const invitation = "019a7777-7777-7777-8777-777777777777";
const ticket = "A".repeat(43);
function open(phase = "email", suffix = "") {
  window.history.replaceState(
    null,
    "",
    `/staff/accept#invitation=${invitation}&ticket=${ticket}&phase=${phase}&code=Opaque%2BCode%2F%3D%3D%23${suffix}`,
  );
}
const receipt = (state: string, phase = "email") => ({
  state,
  invitation_id: invitation,
  phase,
  expires_at: "2026-11-13T12:00:00Z",
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  vi.useRealTimers();
  window.history.replaceState(null, "", "/");
});

it("requests a fresh expiring link without exposing or resubmitting the old code", async () => {
  open();
  const bodies: Record<string, string>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      const body = JSON.parse(init.body as string) as Record<string, string>;
      bodies.push(body);
      return new Response(
        JSON.stringify(
          path.endsWith("/resend")
            ? { ...receipt("email_action_required"), invitation_id: body.delivery_id }
            : receipt("expired"),
        ),
        { status: 200 },
      );
    }),
  );
  renderWithProviders(<StaffAcceptance />);
  await userEvent.click(await screen.findByRole("button", { name: "Send a fresh link" }));
  expect(await screen.findByRole("heading", { name: "Check your email" })).toBeVisible();
  expect(bodies).toHaveLength(2);
  expect(bodies[1].delivery_id).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
  expect(bodies[1].code).toBeUndefined();
  expect(bodies[1].password).toBeUndefined();
});

it("offers a Sign in link for a completed account and never redirects on a timer", async () => {
  vi.useFakeTimers();
  open("password");
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ state: "complete" }), { status: 200 })),
  );
  renderWithProviders(<StaffAcceptance />);
  await act(async () => {});
  expect(screen.getByRole("heading", { name: "Your account is ready" })).toBeVisible();
  expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
  expect(screen.queryByText(/Redirecting/)).not.toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
  });
  expect(navigation.replace).not.toHaveBeenCalled();
  expect(navigation.push).not.toHaveBeenCalled();
  expect(screen.getByRole("heading", { name: "Your account is ready" })).toBeVisible();
});

it("removes fragments and refuses ambiguous links before any request", async () => {
  open("email", "&phase=password");
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<StaffAcceptance />);
  expect(await screen.findByRole("heading", { name: "Open your invitation email" })).toBeVisible();
  expect(fetcher).not.toHaveBeenCalled();
  expect(window.location.hash).toBe("");
});

it("shows already verified without repeating proof or sending another email", async () => {
  open();
  const fetcher = vi.fn(
    async () => new Response(JSON.stringify(receipt("already_verified")), { status: 200 }),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<StaffAcceptance />);
  expect(await screen.findByRole("heading", { name: "Your email is verified" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Set password" })).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(window.location.hash).toBe("");
});

// #1777: the email link opens the password form, makes no proof request on
// load (mail scanners), and one submit carries the code and the password.
it("sets the password and confirms the email in one submit, then starts sign-in once", async () => {
  open();
  const calls: { path: string; body: Record<string, string> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      calls.push({ path, body: JSON.parse(init.body as string) });
      return new Response(
        JSON.stringify(
          path.endsWith("/email") ? { state: "complete" } : receipt("email_action_required"),
        ),
        { status: 200 },
      );
    }),
  );
  renderWithProviders(<StaffAcceptance />);
  expect(await screen.findByRole("heading", { name: "Choose your password" })).toBeVisible();
  expect(screen.getByText("This also confirms your email address.")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Verify email" })).not.toBeInTheDocument();
  expect(calls.map((c) => c.path.split("/").pop())).toEqual(["status"]);
  await userEvent.type(screen.getByLabelText("New password"), "private-password");
  await userEvent.type(screen.getByLabelText("Confirm password"), "private-password");
  await userEvent.click(screen.getByRole("button", { name: "Set password" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Opening sign-in…");
  expect(calls[1].path).toMatch(/\/email$/);
  expect(calls[1].body).toMatchObject({
    code: "Opaque+Code/==#",
    ticket,
    password: "private-password",
  });
  await waitFor(() => expect(signin.signinAfterStaffSetup).toHaveBeenCalledTimes(1));
  expect(document.body.textContent).not.toContain("private-password");
  expect(document.body.textContent).not.toContain(ticket);
});

it("keeps the form after a password policy refusal and resubmits the same link", async () => {
  open();
  const bodies: Record<string, string>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      bodies.push(JSON.parse(init.body as string));
      if (!path.endsWith("/email"))
        return new Response(JSON.stringify(receipt("email_action_required")), { status: 200 });
      if (bodies.length === 2)
        return new Response(JSON.stringify({ error: "password_policy", message: "x" }), {
          status: 422,
        });
      return new Response(JSON.stringify({ state: "complete" }), { status: 200 });
    }),
  );
  renderWithProviders(<StaffAcceptance />);
  const submit = async (value: string) => {
    await userEvent.type(await screen.findByLabelText("New password"), value);
    await userEvent.type(screen.getByLabelText("Confirm password"), value);
    await userEvent.click(screen.getByRole("button", { name: "Set password" }));
  };
  await submit("short");
  expect(await screen.findByRole("alert")).toHaveTextContent("password rules");
  expect(screen.getByRole("heading", { name: "Choose your password" })).toBeVisible();
  await submit("a-longer-private-password");
  await waitFor(() => expect(signin.signinAfterStaffSetup).toHaveBeenCalledTimes(1));
  expect(bodies[2]).toMatchObject({
    code: "Opaque+Code/==#",
    password: "a-longer-private-password",
  });
});

it("offers a fresh link when the emailed code is refused", async () => {
  open();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path.endsWith("/email")
        ? new Response(JSON.stringify({ error: "staff_setup_refused", message: "x" }), {
            status: 400,
          })
        : new Response(JSON.stringify(receipt("email_action_required")), { status: 200 }),
    ),
  );
  renderWithProviders(<StaffAcceptance />);
  await userEvent.type(await screen.findByLabelText("New password"), "private-password");
  await userEvent.type(screen.getByLabelText("Confirm password"), "private-password");
  await userEvent.click(screen.getByRole("button", { name: "Set password" }));
  expect(
    await screen.findByRole("heading", { name: "This link has expired or been used" }),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "Send a fresh link" })).toBeVisible();
  expect(signin.signinAfterStaffSetup).not.toHaveBeenCalled();
});

it("sends no password request until confirmation matches and clears submitted credentials", async () => {
  open("password");
  const fetcher = vi.fn(
    async (path: string) =>
      new Response(
        JSON.stringify(
          path.endsWith("/password")
            ? { state: "complete" }
            : receipt("password_action_required", "password"),
        ),
        { status: 200 },
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<StaffAcceptance />);
  await screen.findByRole("heading", { name: "Choose your password" });
  await userEvent.type(screen.getByLabelText("New password"), "private-password");
  await userEvent.type(screen.getByLabelText("Confirm password"), "different-password");
  await userEvent.click(screen.getByRole("button", { name: "Set password" }));
  expect(await screen.findByText("Passwords must match.")).toBeVisible();
  const confirmation = screen.getByLabelText("Confirm password");
  expect(confirmation).toHaveAttribute("aria-invalid", "true");
  expect(confirmation).toHaveAttribute("aria-describedby", "staff-confirmation-error");
  expect(document.getElementById("staff-confirmation-error")).toHaveTextContent(
    "Passwords must match.",
  );
  expect(fetcher).toHaveBeenCalledTimes(1);
  await userEvent.clear(screen.getByLabelText("Confirm password"));
  await userEvent.type(screen.getByLabelText("Confirm password"), "private-password");
  await userEvent.click(screen.getByRole("button", { name: "Set password" }));
  await waitFor(() => expect(signin.signinAfterStaffSetup).toHaveBeenCalledTimes(1));
  expect(document.body.textContent).not.toContain("private-password");
  expect(fetcher).toHaveBeenCalledTimes(2);
});

it("explains superseded links without offering verification or password submission", async () => {
  open();
  const fetcher = vi.fn(
    async () => new Response(JSON.stringify(receipt("superseded")), { status: 200 }),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<StaffAcceptance />);
  expect(
    await screen.findByRole("heading", { name: "A newer link replaced this invitation" }),
  ).toBeVisible();
  expect(
    screen.getByText(
      "Open the latest setup email. Older links cannot verify your email or change your password.",
    ),
  ).toBeVisible();
  expect(screen.queryByRole("button", { name: "Verify email" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Set password" })).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

// Presence is a zod rule (#1855): an empty submit shows the inline error on
// the input and sends nothing, with no native validation left on the form.
it("refuses an empty password inline without a request or native validation", async () => {
  open("password");
  const fetcher = vi.fn(
    async () =>
      new Response(JSON.stringify(receipt("password_action_required", "password")), {
        status: 200,
      }),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<StaffAcceptance />);
  await screen.findByRole("heading", { name: "Choose your password" });
  const password = screen.getByLabelText("New password");
  expect(password).not.toHaveAttribute("required");
  expect(screen.getByLabelText("Confirm password")).not.toHaveAttribute("required");
  expect(password.closest("form")).toHaveAttribute("novalidate");
  await userEvent.click(screen.getByRole("button", { name: "Set password" }));
  expect(await screen.findByText("Enter a new password.")).toBeInTheDocument();
  expect(password).toHaveAttribute("aria-invalid", "true");
  expect(password).toHaveAttribute("aria-describedby", "staff-password-error");
  expect(screen.getByText("Confirm your new password.")).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
});
