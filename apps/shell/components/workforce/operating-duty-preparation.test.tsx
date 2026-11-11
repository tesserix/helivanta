import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { IndependentOperatingDuties } from "./independent-operating-duties";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/pharmacy/staff",
  useSearchParams: () => new URLSearchParams(),
}));
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-manager",
  employerKind: "pharmacy" as const,
  employerId: "019a55c0-1111-7111-8111-111111111112",
  staffId: "019a55c0-1111-7111-8111-111111111113",
};
const linkId = "019a55c0-1111-7111-8111-111111111114";
const outletId = "019a55c0-1111-7111-8111-111111111115";
const page = (data: unknown[]) =>
  Response.json({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("prepares a pharmacy dispatch duty for the exact reviewed account and outlet", async () => {
  let input: unknown;
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
        input = JSON.parse(String(init.body));
        return Response.json({
          owner_tenant_id: scope.tenantId,
          id: url.split("/").at(-1),
          input,
          status: "pending_review",
          version: 1,
          prepared_by: scope.subject,
          created_at: "2026-11-09T00:00:00Z",
          updated_at: "2026-11-09T00:00:00Z",
        });
      }
      if (url.includes("/account-links"))
        return page([
          {
            scope: {
              owner_tenant_id: scope.tenantId,
              employer_kind: "pharmacy",
              employer_id: scope.employerId,
              staff_id: scope.staffId,
            },
            staff_version: 3,
            link: {
              id: linkId,
              staff_id: scope.staffId,
              subject: "TEST-dispatcher",
              status: "active",
              version: 2,
              proposed_by_subject: "TEST-maker",
              proposal_reason: "TEST reviewed account",
              actor_subject: "TEST-reviewer",
              reason: "TEST independent review",
              created_at: "2026-11-09T00:00:00Z",
              updated_at: "2026-11-09T00:00:00Z",
            },
          },
        ]);
      if (url.includes("/outlets"))
        return page([
          {
            id: outletId,
            pharmacy_id: scope.employerId,
            code: "TEST-PUNE",
            name: "TEST Pune outlet",
            country_code: "IN",
          },
        ]);
      if (url.includes("/pharmacy/pharmacies"))
        return page([
          { id: scope.employerId, ownership_type: "independent", name: "TEST pharmacy" },
        ]);
      return page([]);
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties scope={scope} staffVersion={3} staffActive />
    </NavigationGuardProvider>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Assign operating duty" }));
  await user.click(await screen.findByLabelText("Operating role"));
  expect(screen.queryByRole("option", { name: /Warehouse reservation/ })).not.toBeInTheDocument();
  await user.click(
    await screen.findByRole("option", { name: "Outlet reservation and physical dispatch" }),
  );
  await user.click(screen.getByLabelText("Reviewed account"));
  await user.click(await screen.findByRole("option", { name: "TEST-dispatcher" }));
  await user.click(screen.getByLabelText("Pharmacy outlet"));
  await user.click(await screen.findByRole("option", { name: "TEST-PUNE — TEST Pune outlet" }));
  fireEvent.change(screen.getByLabelText("Valid from (UTC)"), {
    target: { value: "2026-11-09T00:00" },
  });
  fireEvent.change(screen.getByLabelText("Expires at (UTC)"), {
    target: { value: "2026-12-09T00:00" },
  });
  await user.type(screen.getByLabelText("Duty reason"), "TEST scoped physical dispatch");
  await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
  await screen.findByText(
    "Operating duty proposed. A separate reviewer must approve it before activation.",
  );
  expect(input).toMatchObject({
    scope: {
      employer_kind: "pharmacy",
      employer_id: scope.employerId,
      location_id: outletId,
      expected_employer_version: 1,
      expected_location_version: 1,
    },
    identity: { staff_id: scope.staffId, staff_version: 3, link_id: linkId, link_version: 2 },
    subject: "TEST-dispatcher",
    template_key: "pharmacy.stock_fulfiller.v1",
  });
});

it.each([
  [false, ["iam.operating.read", "iam.operating.prepare"]],
  [true, ["iam.operating.read"]],
])(
  "does not offer preparation without active Staff and prepare authority (%s)",
  async (active, permissions) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) =>
        url.endsWith("permissions")
          ? Response.json({ tenant_id: scope.tenantId, subject: scope.subject, data: permissions })
          : page([]),
      ),
    );
    renderWithProviders(
      <NavigationGuardProvider>
        <IndependentOperatingDuties scope={scope} staffVersion={3} staffActive={active} />
      </NavigationGuardProvider>,
    );
    await screen.findByText("No operating duties on this page.");
    expect(screen.queryByRole("button", { name: "Assign operating duty" })).not.toBeInTheDocument();
  },
);

function sourceFixture(
  options: {
    kind?: "supplier" | "pharmacy";
    staffVersion?: number;
    linkStatus?: string;
    foreignOutlet?: boolean;
    country?: string;
    ownership?: string;
    lostReply?: boolean;
    paginated?: boolean;
  } = {},
) {
  const currentScope = { ...scope, employerKind: options.kind ?? "pharmacy" };
  const writes: Array<{ path: string; input: Record<string, unknown>; headers: Headers }> = [];
  let receipt: unknown;
  let refused = false;
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
        const input = JSON.parse(String(init.body));
        writes.push({ path: url, input, headers: new Headers(init.headers) });
        receipt = {
          owner_tenant_id: scope.tenantId,
          id: url.split("/").at(-1),
          input,
          status: "pending_review",
          version: 1,
          prepared_by: scope.subject,
          created_at: "2026-11-09T00:00:00Z",
          updated_at: "2026-11-09T00:00:00Z",
        };
        if (options.lostReply) throw new TypeError("TEST reply lost after commit");
        return Response.json(receipt);
      }
      if (url.endsWith("/proposal-receipt")) return Response.json(receipt);
      if (url.includes("/account-links"))
        return page([
          {
            scope: {
              owner_tenant_id: scope.tenantId,
              employer_kind: currentScope.employerKind,
              employer_id: scope.employerId,
              staff_id: scope.staffId,
            },
            staff_version: options.staffVersion ?? 3,
            link: {
              id: linkId,
              staff_id: scope.staffId,
              subject: "TEST-dispatcher",
              status: options.linkStatus ?? "active",
              version: 2,
              proposed_by_subject: "TEST-maker",
              proposal_reason: "TEST account",
              actor_subject: "TEST-reviewer",
              reason: "TEST review",
              created_at: "2026-11-09T00:00:00Z",
              updated_at: "2026-11-09T00:00:00Z",
            },
          },
        ]);
      if (url.includes("/outlets")) {
        if (refused)
          return Response.json(
            { error: "forbidden", message: "TEST source revoked" },
            { status: 403 },
          );
        if (options.paginated && !url.includes("cursor="))
          return Response.json({ data: [], page: { has_more: true, next_cursor: "TEST-next" } });
        return page([
          {
            id: outletId,
            pharmacy_id: options.foreignOutlet ? outletId : scope.employerId,
            code: "TEST-PUNE",
            name: "TEST Pune outlet",
            country_code: options.country ?? "IN",
          },
        ]);
      }
      if (url.includes("/pharmacy/pharmacies"))
        return page([{ id: scope.employerId, ownership_type: options.ownership ?? "independent" }]);
      if (url.includes("/premises"))
        return page([
          {
            id: outletId,
            party_id: scope.employerId,
            code: "TEST-PUNE",
            name: "TEST Pune warehouse",
            country_code: "IN",
            status: "active",
            kind: "warehouse",
            version: 7,
          },
        ]);
      if (url === `/api/v1/supply/parties/${scope.employerId}`)
        return Response.json({
          id: scope.employerId,
          record_kind: "owned_organisation",
          version: 4,
        });
      return page([]);
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <IndependentOperatingDuties scope={currentScope} staffVersion={3} staffActive />
    </NavigationGuardProvider>,
  );
  return {
    writes,
    refuse: () => {
      refused = true;
    },
  };
}
async function openPreparation() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Assign operating duty" }));
  await screen.findByLabelText("Operating role");
  return user;
}
async function fillPreparation(
  user: ReturnType<typeof userEvent.setup>,
  kind: "supplier" | "pharmacy" = "pharmacy",
  end = "2026-12-09T00:00",
) {
  await user.click(screen.getByLabelText("Reviewed account"));
  await user.click(await screen.findByRole("option", { name: "TEST-dispatcher" }));
  await user.click(
    screen.getByLabelText(kind === "pharmacy" ? "Pharmacy outlet" : "Warehouse or depot"),
  );
  await user.click(
    await screen.findByRole("option", {
      name:
        kind === "pharmacy" ? "TEST-PUNE — TEST Pune outlet" : "TEST-PUNE — TEST Pune warehouse",
    }),
  );
  fireEvent.change(screen.getByLabelText("Valid from (UTC)"), {
    target: { value: "2026-11-09T00:00" },
  });
  fireEvent.change(screen.getByLabelText("Expires at (UTC)"), { target: { value: end } });
  await user.type(screen.getByLabelText("Duty reason"), "TEST source duty");
}
it.each([{ staffVersion: 2 }, { linkStatus: "pending" }])(
  "does not offer an account without current independent review (%j)",
  async (options) => {
    const { writes } = sourceFixture(options);
    const user = await openPreparation();
    await user.click(screen.getByLabelText("Reviewed account"));
    expect(screen.queryByRole("option", { name: "TEST-dispatcher" })).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  },
);
it.each([{ foreignOutlet: true }, { country: "US" }])(
  "does not offer a foreign pharmacy outlet (%j)",
  async (options) => {
    const { writes } = sourceFixture(options);
    const user = await openPreparation();
    await user.click(screen.getByLabelText("Pharmacy outlet"));
    expect(screen.queryByRole("option", { name: /TEST-PUNE/ })).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  },
);
it("refuses a hospital-owned organization in independent duty preparation", async () => {
  const { writes } = sourceFixture({ ownership: "hospital_owned" });
  const user = await openPreparation();
  await fillPreparation(user);
  await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
  await screen.findByText(
    "Choose a current reviewed account, operating role and owned source location.",
  );
  expect(writes).toHaveLength(0);
});
it.each(["2026-11-08T00:00", "2028-11-09T00:00"])(
  "rejects an inverted or excessive role validity (%s)",
  async (end) => {
    const { writes } = sourceFixture();
    const user = await openPreparation();
    await fillPreparation(user, "pharmacy", end);
    await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
    await screen.findByText(/within the role’s maximum validity/);
    expect(writes).toHaveLength(0);
  },
);
it("recovers a lost pharmacy proposal without repeating the PUT and binds expected identity headers", async () => {
  const { writes } = sourceFixture({ lostReply: true });
  const user = await openPreparation();
  await fillPreparation(user);
  await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
  await user.click(await screen.findByRole("button", { name: "Recover duty proposal" }));
  await screen.findByText(/Operating duty proposed/);
  expect(writes).toHaveLength(1);
  expect(writes[0]?.headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(writes[0]?.headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it("follows an empty outlet page that has a continuation", async () => {
  sourceFixture({ paginated: true });
  const user = await openPreparation();
  await user.click(await screen.findByRole("button", { name: "More locations" }));
  await fillPreparation(user);
  await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
  await screen.findByText(/Operating duty proposed/);
});
it("prepares a warehouse dispatch role instead of always selecting receiving", async () => {
  const { writes } = sourceFixture({ kind: "supplier" });
  const user = await openPreparation();
  await user.click(screen.getByLabelText("Operating role"));
  await user.click(
    await screen.findByRole("option", { name: "Warehouse reservation and physical dispatch" }),
  );
  await fillPreparation(user, "supplier");
  await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
  await screen.findByText(/Operating duty proposed/);
  expect(writes[0]?.input).toMatchObject({
    template_key: "supply.stock_fulfiller.v1",
    scope: { expected_employer_version: 4, expected_location_version: 7 },
  });
});
it("removes source preparation after a current source read is refused", async () => {
  const source = sourceFixture();
  const user = await openPreparation();
  source.refuse();
  await user.click(screen.getByRole("button", { name: "Refresh preparation sources" }));
  await screen.findAllByText("This wasn't found, or you don't have access to it.");
  expect(screen.queryByRole("button", { name: "Propose operating duty" })).not.toBeInTheDocument();
  expect(source.writes).toHaveLength(0);
});
