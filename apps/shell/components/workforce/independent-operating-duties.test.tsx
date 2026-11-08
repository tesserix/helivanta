import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/supply/staff",
  useSearchParams: () => new URLSearchParams(),
}));
import { IndependentOperatingDuties } from "./independent-operating-duties";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-reviewer",
  employerKind: "supplier" as const,
  employerId: "019a55c0-1111-7111-8111-111111111112",
  staffId: "019a55c0-1111-7111-8111-111111111113",
};
const row = {
  owner_tenant_id: scope.tenantId,
  id: "019a55c0-1111-7111-8111-111111111114",
  input: {
    scope: {
      employer_kind: scope.employerKind,
      employer_id: scope.employerId,
      location_id: "019a55c0-1111-7111-8111-111111111116",
      expected_employer_version: 1,
      expected_location_version: 1,
    },
    identity: {
      staff_id: scope.staffId,
      staff_version: 1,
      link_id: "019a55c0-1111-7111-8111-111111111117",
      link_version: 2,
    },
    subject: "TEST-receiver",
    template_key: "supply.inventory_receiver.v1",
    reason: "TEST private reviewed duty",
    valid_from: "2026-11-07T00:00:00Z",
    expires_at: "2027-01-01T00:00:00Z",
  },
  status: "pending_review",
  version: 1,
  prepared_by: "TEST-maker",
  created_at: "2026-11-07T00:00:00Z",
  updated_at: "2026-11-07T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("recovers the exact decision after response loss without another PUT", async () => {
  let receipt: unknown;
  let writes = 0;
  let savedPath = "";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["iam.operating.read", "iam.operating.review"],
        });
      if (init?.method === "PUT") {
        writes++;
        savedPath = url;
        receipt = {
          id: url.split("/").at(-1),
          duty_id: row.id,
          input: JSON.parse(String(init.body)),
          result: { ...row, status: "approved_pending_activation", version: 2 },
          actor_subject: scope.subject,
          created_at: row.created_at,
        };
        throw new TypeError("TEST response lost after commit");
      }
      if (url === savedPath) return Response.json(receipt);
      return Response.json({ data: [row], page: { has_more: false, next_cursor: null } });
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties scope={scope} staffVersion={1} staffActive />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("TEST private reviewed duty")).toBeInTheDocument();
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Decision reason"), "TEST independent review");
  await user.click(screen.getByRole("button", { name: "Approve duty" }));
  await user.click(await screen.findByRole("button", { name: "Recover duty decision" }));
  await screen.findByText(/Approval recorded.*activation/i);
  expect(writes).toBe(1);
});
it("removes private duties when current read authority is refused", async () => {
  let refused = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("permissions")
        ? Response.json({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["iam.operating.read"],
          })
        : refused
          ? Response.json({ error: "not_found", message: "TEST revoked" }, { status: 404 })
          : Response.json({ data: [row], page: { has_more: false, next_cursor: null } }),
    ),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties scope={scope} staffVersion={1} staffActive />
    </NavigationGuardProvider>,
  );
  await screen.findByText("TEST private reviewed duty");
  refused = true;
  await userEvent.setup().click(screen.getByRole("button", { name: "Refresh duties" }));
  await waitFor(() =>
    expect(screen.queryByText("TEST private reviewed duty")).not.toBeInTheDocument(),
  );
  await screen.findByText("This wasn't found, or you don't have access to it.");
});

it("prepares the exact current warehouse and reviewed account and recovers a lost proposal", async () => {
  let proposal: Record<string, unknown> | undefined;
  let path = "";
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["iam.operating.read", "iam.operating.prepare"],
        });
      if (init?.method === "PUT") {
        writes++;
        path = url;
        proposal = {
          ...row,
          id: url.split("/").at(-1),
          input: JSON.parse(String(init.body)),
          prepared_by: scope.subject,
        };
        throw new TypeError("TEST lost proposal response after commit");
      }
      if (url === `${path}/proposal-receipt`) return Response.json(proposal);
      if (url.includes("/account-links"))
        return Response.json({
          data: [
            {
              scope: {
                owner_tenant_id: scope.tenantId,
                employer_kind: "supplier",
                employer_id: scope.employerId,
                staff_id: scope.staffId,
              },
              staff_version: 1,
              link: {
                id: row.input.identity.link_id,
                staff_id: scope.staffId,
                subject: "TEST-receiver",
                status: "active",
                version: 2,
                proposed_by_subject: "TEST-maker",
                proposal_reason: "TEST identity",
                actor_subject: "TEST-reviewer",
                reason: "TEST reviewed",
                created_at: row.created_at,
                updated_at: row.created_at,
              },
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("/premises"))
        return Response.json({
          data: [
            {
              id: row.input.scope.location_id,
              party_id: scope.employerId,
              version: 7,
              code: "TEST-PUNE",
              name: "TEST Pune warehouse",
              status: "active",
              kind: "warehouse",
              country_code: "IN",
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url === `/api/v1/supply/parties/${scope.employerId}`)
        return Response.json({
          id: scope.employerId,
          version: 4,
          record_kind: "owned_organisation",
        });
      return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties scope={scope} staffVersion={1} staffActive />
    </NavigationGuardProvider>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Assign receiving duty" }));
  await user.click(await screen.findByLabelText("Reviewed account"));
  await user.click(await screen.findByRole("option", { name: "TEST-receiver" }));
  await user.click(screen.getByLabelText("Warehouse or depot"));
  await user.click(await screen.findByRole("option", { name: "TEST-PUNE — TEST Pune warehouse" }));
  const { fireEvent } = await import("@testing-library/react");
  fireEvent.change(screen.getByLabelText("Valid from (UTC)"), {
    target: { value: "2026-11-07T00:00" },
  });
  fireEvent.change(screen.getByLabelText("Expires at (UTC)"), {
    target: { value: "2027-01-01T00:00" },
  });
  await user.type(screen.getByLabelText("Duty reason"), "TEST exact receiving source");
  await user.click(screen.getByRole("button", { name: "Propose receiving duty" }));
  await user.click(await screen.findByRole("button", { name: "Recover duty proposal" }));
  await screen.findByText(
    "Receiving duty proposed. A separate reviewer must approve it before activation.",
  );
  expect(writes).toBe(1);
  expect(proposal?.input).toMatchObject({
    scope: { expected_employer_version: 4, expected_location_version: 7 },
    identity: row.input.identity,
    subject: "TEST-receiver",
  });
});

it("allows refresh after an explicit stale-decision rejection instead of freezing recovery", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["iam.operating.read", "iam.operating.review"],
        });
      if (init?.method === "PUT")
        return Response.json(
          { error: "operating_conflict", message: "TEST changed basis; refresh first" },
          { status: 409 },
        );
      return Response.json({ data: [row], page: { has_more: false, next_cursor: null } });
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties scope={scope} staffVersion={1} staffActive />
    </NavigationGuardProvider>,
  );
  const user = userEvent.setup();
  await screen.findByText("TEST private reviewed duty");
  await user.type(screen.getByLabelText("Decision reason"), "TEST stale review");
  await user.click(screen.getByRole("button", { name: "Approve duty" }));
  await screen.findByText("TEST changed basis; refresh first");
  expect(screen.queryByRole("button", { name: "Recover duty decision" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Refresh duties" })).toBeEnabled();
});
