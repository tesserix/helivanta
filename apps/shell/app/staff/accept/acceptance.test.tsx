import { afterEach, expect, it, vi } from "vitest";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@helivanta/api/testing";
import { StaffAcceptance } from "./acceptance";

const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));

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

it("redirects a completed account after seven seconds and offers an immediate login link", async () => {
  vi.useFakeTimers();
  open("password");
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ state: "complete" }), { status: 200 })),
  );
  renderWithProviders(<StaffAcceptance />);
  await act(async () => {});
  expect(screen.getByRole("heading", { name: "Your account is ready" })).toBeVisible();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(6999);
  });
  expect(navigation.replace).not.toHaveBeenCalled();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(navigation.replace).toHaveBeenCalledWith("/login");
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
  expect(screen.queryByRole("button", { name: "Verify email" })).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(window.location.hash).toBe("");
});

it("verifies only after a deliberate click and preserves opaque native codes", async () => {
  open();
  const calls: { path: string; body: Record<string, string> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit) => {
      calls.push({ path, body: JSON.parse(init.body as string) });
      return new Response(
        JSON.stringify(
          receipt(path.endsWith("/email") ? "already_verified" : "email_action_required"),
        ),
        { status: 200 },
      );
    }),
  );
  renderWithProviders(<StaffAcceptance />);
  const button = await screen.findByRole("button", { name: "Verify email" });
  expect(calls).toHaveLength(1);
  await userEvent.click(button);
  expect(await screen.findByRole("heading", { name: "Your email is verified" })).toBeVisible();
  expect(calls[1].body.code).toBe("Opaque+Code/==#");
  expect(calls[1].body.ticket).toBe(ticket);
  expect(document.body.textContent).not.toContain(ticket);
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
  expect(fetcher).toHaveBeenCalledTimes(1);
  await userEvent.clear(screen.getByLabelText("Confirm password"));
  await userEvent.type(screen.getByLabelText("Confirm password"), "private-password");
  await userEvent.click(screen.getByRole("button", { name: "Set password" }));
  expect(await screen.findByRole("heading", { name: "Your account is ready" })).toBeVisible();
  expect(screen.getByRole("link", { name: "Go to login" })).toHaveAttribute("href", "/login");
  expect(document.body.textContent).not.toContain("private-password");
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
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
