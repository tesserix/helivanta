import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { StaffMembershipForm } from "./membership-form";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-admin" };
afterEach(() => vi.unstubAllGlobals());
it("requires an enrollment reason and ordinary-membership acknowledgement", async () => {
  const requests: RequestInit[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PUT") requests.push(init);
      return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffMembershipForm scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.type(screen.getByLabelText("Existing account ID"), "synthetic-first-account");
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(await screen.findByLabelText("Enrollment reason")).toHaveAttribute("aria-invalid", "true");
  expect(requests).toHaveLength(0);
  await user.type(
    screen.getByLabelText("Enrollment reason"),
    "Verified account with no previous membership",
  );
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(requests).toHaveLength(0);
  expect(screen.getByText("Acknowledge the membership consequence.")).toBeInTheDocument();
});
it("freezes and retries the same command after an uncertain result", async () => {
  const commands: Array<{ url: string; body: string; actor: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method !== "PUT")
        return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
      commands.push({
        url,
        body: String(init.body),
        actor: new Headers(init.headers).get("X-Helivanta-Expected-Subject"),
      });
      if (commands.length === 1)
        return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 502 });
      return Response.json({
        id: url.split("/").at(-1),
        ...JSON.parse(String(init.body)),
        membership_id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
        role_key: "staff_member",
        membership_epoch: 1,
        actor_subject: scope.subject,
        created_at: "2026-10-30T00:00:00Z",
      });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffMembershipForm scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.type(screen.getByLabelText("Existing account ID"), "synthetic-first-account");
  await user.type(screen.getByLabelText("Enrollment reason"), "Verified first enrollment");
  await user.click(screen.getByLabelText("I understand this creates ordinary membership only."));
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.getByLabelText("Existing account ID")).toBeDisabled();
  expect(screen.getByLabelText("Enrollment reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same enrollment" }));
  await waitFor(() => expect(commands).toHaveLength(2));
  expect(commands[0]).toEqual(commands[1]);
  expect(commands[0]?.actor).toBe(scope.subject);
  expect(
    await screen.findByText(/Membership recorded for synthetic-first-account/),
  ).toBeInTheDocument();
});
