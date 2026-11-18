import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountPresentationsProvider, NavigationGuardProvider } from "@helivanta/ui";
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
      <StaffMembershipForm scope={scope} initialSubject="synthetic-first-account" />
    </NavigationGuardProvider>,
  );
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
      <StaffMembershipForm scope={scope} initialSubject="synthetic-first-account" />
    </NavigationGuardProvider>,
  );
  await user.type(screen.getByLabelText("Enrollment reason"), "Verified first enrollment");
  await user.click(screen.getByLabelText("I understand this creates ordinary membership only."));
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.queryByLabelText("Existing account ID")).not.toBeInTheDocument();
  expect(screen.getByLabelText("Enrollment reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same enrollment" }));
  await waitFor(() => expect(commands).toHaveLength(2));
  expect(commands[0]).toEqual(commands[1]);
  expect(commands[0]?.actor).toBe(scope.subject);
  const confirmation = await screen.findByText(/Membership recorded for/);
  expect(confirmation).not.toHaveTextContent("019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb");
  expect(confirmation).not.toHaveTextContent(commands[0]!.url.split("/").at(-1)!);
});

it("shows named enrollment evidence without receipt or membership identifiers", async () => {
  const receipt = {
    id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
    membership_id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
    subject: "synthetic-first-account",
    role_key: "staff_member",
    membership_epoch: 1,
    reason: "Verified first enrollment",
    actor_subject: scope.subject,
    created_at: "2026-10-30T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("account-presentations")) {
        const { subjects } = JSON.parse(String(init?.body));
        return Response.json({
          tenant_id: scope.tenantId,
          actor_subject: scope.subject,
          data: subjects.map((subject: string) => ({
            subject,
            available: true,
            name: subject === scope.subject ? "Hospital owner" : "QA Staff",
            email: "staff@example.test",
            roles: [],
          })),
        });
      }
      return Response.json({
        data: [receipt],
        page: { has_more: false, next_cursor: null },
      });
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <AccountPresentationsProvider scope={scope}>
        <StaffMembershipForm scope={scope} />
      </AccountPresentationsProvider>
    </NavigationGuardProvider>,
  );
  const history = screen.getByRole("region", { name: "Enrollment history" });
  expect(await within(history).findByText("QA Staff")).toBeInTheDocument();
  expect(await within(history).findByText("Hospital owner")).toBeInTheDocument();
  expect(history).toHaveTextContent("Ordinary membership");
  expect(history).toHaveTextContent(receipt.reason);
  expect(history).toHaveTextContent(/Oct.*30.*2026|30.*Oct.*2026/);
  expect(history).not.toHaveTextContent(receipt.id);
  expect(history).not.toHaveTextContent(receipt.membership_id);
});

// #1595: refused enrollment history is a refusal with no way to ask again.
it("shows refused enrollment history as a refusal, with no refresh", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ error: "forbidden", message: "secret detail" }, { status: 403 }),
    ),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <StaffMembershipForm scope={scope} />
    </NavigationGuardProvider>,
  );
  expect(
    await screen.findByText("This wasn't found, or you don't have access to it."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /refresh enrollment history/i })).toBeNull();
});

it("requires a verified Staff setup target instead of exposing a raw account identifier input", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ data: [], page: { has_more: false, next_cursor: null } })),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <StaffMembershipForm scope={scope} />
    </NavigationGuardProvider>,
  );
  expect(screen.queryByLabelText("Existing account ID")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Enroll staff member" })).not.toBeInTheDocument();
  expect(screen.getByText(/Start with verified Staff account setup/)).toBeInTheDocument();
});
