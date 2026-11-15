import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { IndependentOperatingDuties } from "./independent-operating-duties";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/workforce/operating-access",
  useSearchParams: () => new URLSearchParams(),
}));
const scope = {
  administration: "governance" as const,
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-governance-preparer",
  employerKind: "supplier" as const,
  employerId: "019a55c0-1111-7111-8111-111111111112",
  staffId: "019a55c0-1111-7111-8111-111111111113",
};
const account = {
  staff_id: scope.staffId,
  display_name: "Synthetic staff",
  staff_version: 3,
  subject: "TEST-receiver",
  link_id: "019a55c0-1111-7111-8111-111111111114",
  link_version: 2,
};
const location = {
  owner_tenant_id: scope.tenantId,
  employer_kind: scope.employerKind,
  employer_id: scope.employerId,
  location_id: "019a55c0-1111-7111-8111-111111111115",
  code: "SYN-DEPOT",
  name: "Synthetic depot",
  country_code: "IN",
  employer_version: 5,
  location_version: 7,
};
const page = (data: unknown[]) =>
  Response.json({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("prepares through native sources with exact reviewed identity and owner revisions", async () => {
  let input: Record<string, unknown> | undefined;
  const request = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return Response.json({
        tenant_id: scope.tenantId,
        subject: scope.subject,
        data: ["iam.governance.operating.manage", "iam.governance.operating.review"],
      });
    if (url.includes("/location-choices")) return page([location]);
    if (init?.method === "PUT") {
      input = JSON.parse(String(init.body));
      return Response.json({
        owner_tenant_id: scope.tenantId,
        id: url.split("/").at(-1),
        input,
        status: "pending_review",
        version: 1,
        prepared_by: scope.subject,
        created_at: "2026-11-14T00:00:00Z",
        updated_at: "2026-11-14T00:00:00Z",
      });
    }
    if (url.includes("/operating-governance-employers/") && url.includes("/duties"))
      return page([]);
    throw new Error(`Unexpected legacy request: ${url}`);
  });
  vi.stubGlobal("fetch", request);
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties
        scope={scope}
        staffVersion={3}
        staffActive
        reviewedAccount={account}
      />
    </NavigationGuardProvider>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Assign operating duty" }));
  await user.click(await screen.findByLabelText("Warehouse or depot"));
  await user.click(await screen.findByRole("option", { name: "SYN-DEPOT — Synthetic depot" }));
  fireEvent.change(screen.getByLabelText("Valid from (UTC)"), {
    target: { value: "2030-11-14T00:00" },
  });
  fireEvent.change(screen.getByLabelText("Expires at (UTC)"), {
    target: { value: "2030-11-15T00:00" },
  });
  await user.type(screen.getByLabelText("Duty reason"), "Synthetic independent receiving");
  await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
  await screen.findByText(
    "Operating duty proposed. A separate reviewer must approve it before activation.",
  );
  expect(input).toMatchObject({
    scope: {
      employer_kind: "supplier",
      employer_id: scope.employerId,
      location_id: location.location_id,
      expected_employer_version: 5,
      expected_location_version: 7,
    },
    identity: {
      staff_id: scope.staffId,
      staff_version: 3,
      link_id: account.link_id,
      link_version: 2,
    },
    subject: account.subject,
  });
  expect(
    request.mock.calls.some(
      ([url]) =>
        url.includes("/account-links") ||
        url.includes("/supply/parties/") ||
        url.includes("/pharmacy/pharmacies"),
    ),
  ).toBe(false);
});

it("reviews a native duty and recovers its exact decision without repeating the command", async () => {
  const row = {
    owner_tenant_id: scope.tenantId,
    id: "019a55c0-1111-7111-8111-111111111119",
    input: {
      scope: {
        employer_kind: scope.employerKind,
        employer_id: scope.employerId,
        location_id: location.location_id,
        expected_employer_version: 5,
        expected_location_version: 7,
      },
      identity: {
        staff_id: scope.staffId,
        staff_version: 3,
        link_id: account.link_id,
        link_version: 2,
      },
      subject: account.subject,
      template_key: "supply.inventory_receiver.v1",
      reason: "Synthetic reviewed receiving duty",
      valid_from: "2030-11-14T00:00:00Z",
      expires_at: "2030-11-15T00:00:00Z",
    },
    status: "pending_review",
    version: 1,
    prepared_by: "TEST-another-preparer",
    created_at: "2026-11-14T00:00:00Z",
    updated_at: "2026-11-14T00:00:00Z",
  };
  let command: Record<string, unknown> | undefined,
    path = "",
    writes = 0;
  const request = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return Response.json({
        tenant_id: scope.tenantId,
        subject: scope.subject,
        data: ["iam.governance.operating.manage", "iam.governance.operating.review"],
      });
    if (init?.method === "PUT") {
      command = JSON.parse(String(init.body));
      path = url;
      writes++;
      return Response.json(
        { error: "unavailable", message: "Synthetic lost decision response" },
        { status: 500 },
      );
    }
    if (url.includes("/commands/"))
      return Response.json({
        id: url.split("/").at(-1),
        duty_id: row.id,
        input: command,
        result: { ...row, status: "approved_pending_activation", version: 2 },
        actor_subject: scope.subject,
        created_at: row.created_at,
      });
    return page([row]);
  });
  vi.stubGlobal("fetch", request);
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties
        scope={scope}
        staffVersion={3}
        staffActive
        reviewedAccount={account}
      />
    </NavigationGuardProvider>,
  );
  const user = userEvent.setup();
  await user.type(
    await screen.findByLabelText("Decision reason"),
    "Synthetic independent approval",
  );
  await user.click(screen.getByRole("button", { name: "Approve duty" }));
  await user.click(await screen.findByRole("button", { name: "Recover duty decision" }));
  await screen.findByText("Approval recorded. Waiting for verified activation.");
  expect(writes).toBe(1);
  expect(command).toMatchObject({
    expected_version: 1,
    decision: "approve",
    reason: "Synthetic independent approval",
  });
  expect(
    request.mock.calls.find(([url, init]) => url === path && init?.method !== "PUT")?.[1]?.method ??
      "GET",
  ).toBe("GET");
  expect(path).toContain(
    `/iam/operating-governance-employers/supplier/${scope.employerId}/duties/${row.id}/commands/`,
  );
});

it.each([
  undefined,
  { ...account, staff_version: 2 },
  { ...account, staff_id: "019a55c0-1111-7111-8111-111111111118" },
])(
  "refuses native preparation without the exact qualified Staff basis %j",
  async (reviewedAccount) => {
    const request = vi.fn(async (url: string) =>
      url.endsWith("permissions")
        ? Response.json({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["iam.governance.operating.manage"],
          })
        : page([]),
    );
    vi.stubGlobal("fetch", request);
    renderWithProviders(
      <NavigationGuardProvider>
        <IndependentOperatingDuties
          scope={scope}
          staffVersion={3}
          staffActive
          reviewedAccount={reviewedAccount}
        />
      </NavigationGuardProvider>,
    );
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: "Assign operating duty" }));
    await screen.findByText("Refresh and select a current independently reviewed Staff account.");
    expect(
      screen.queryByRole("button", { name: "Propose operating duty" }),
    ).not.toBeInTheDocument();
    expect(
      request.mock.calls.some(
        ([url]) =>
          url.includes("/account-links") ||
          url.includes("/supply/parties/") ||
          url.includes("/location-choices"),
      ),
    ).toBe(false);
  },
);
