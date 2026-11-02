import { NavigationGuardProvider } from "@helivanta/ui";
import "@/test/select";
import { screen, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { StaffPostingDraftPanel } from "./posting-drafts-panel";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-manager",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const department = {
  id: "019a0000-0000-7000-8000-000000000003",
  revision_id: "019a0000-0000-7000-8000-000000000004",
  hospital_id: scope.hospitalId,
  version: 1,
  code: "PHARMA",
  name: "Synthetic Pharmacy",
  kind: "department",
  status: "active",
  created_at: "2026-11-02T00:00:00Z",
  effective_at: "2026-11-02T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("requires a canonical department and dates and retains the exact uncertain command", async () => {
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push({ url, body });
        if (writes.length === 1)
          return Response.json(
            { error: "unavailable", message: "Synthetic uncertain outcome" },
            { status: 503 },
          );
        const { expected_version, ...intent } = body;
        delete intent.reason;
        return Response.json({
          ...intent,
          id: url.split("/").at(-3),
          revision_id: "019a0000-0000-7000-8000-000000000005",
          version: expected_version + 1,
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          status: "draft",
          grants_access: false,
          created_at: "2026-11-02T00:00:00Z",
          recorded_at: "2026-11-02T00:00:00Z",
        });
      }
      if (url.endsWith("posting-draft-source"))
        return Response.json({
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          staff_version: 1,
          employment_epoch: 1,
        });
      return Response.json({
        data: url.includes("/organization/") ? [department] : [],
        page: { has_more: false, next_cursor: null },
      });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffPostingDraftPanel
        scope={scope}
        staffLabel="Synthetic Applicant"
        canManage
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare posting draft" }));
  const form = screen.getByRole("form", { name: "Posting preparation" });
  await user.click(within(form).getByRole("button", { name: "Save posting draft" }));
  expect(writes).toHaveLength(0);
  await user.click(await within(form).findByLabelText("Department or sub-unit"));
  await user.click(await screen.findByRole("option", { name: "PHARMA · Synthetic Pharmacy" }));
  await user.type(within(form).getByLabelText("Start (UTC)"), "2027-01-01T00:00");
  await user.type(
    within(form).getByLabelText("Reason"),
    "Synthetic reviewed placement preparation",
  );
  await user.click(within(form).getByRole("button", { name: "Save posting draft" }));
  await screen.findByRole("alert");
  expect(writes).toHaveLength(1);
  await user.click(within(form).getByRole("button", { name: "Retry exact posting command" }));
  await screen.findByText("Posting draft saved. Operating authority is unchanged.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(writes[0].body).toMatchObject({
    department_id: department.id,
    department_revision_id: department.revision_id,
    starts_at: "2027-01-01T00:00:00Z",
    staff_version: 1,
    employment_epoch: 1,
  });
});

it("keeps a hospital reader's preparation workspace read only", async () => {
  const row = {
    id: "019a0000-0000-7000-8000-000000000001",
    revision_id: "019a0000-0000-7000-8000-000000000005",
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    version: 1,
    status: "draft",
    grants_access: false,
    staff_version: 1,
    employment_epoch: 1,
    department_id: department.id,
    department_revision_id: department.revision_id,
    starts_at: "2027-01-01T00:00:00Z",
    ends_at: "2027-06-30T17:00:00Z",
    created_at: "2026-11-02T00:00:00Z",
    recorded_at: "2026-11-02T00:00:00Z",
  };
  const fetcher = vi.fn(async (url: string) =>
    url.endsWith("posting-draft-source")
      ? Response.json({
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          staff_version: 1,
          employment_epoch: 1,
        })
      : Response.json({ data: [row], page: { has_more: false, next_cursor: null } }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffPostingDraftPanel
        scope={scope}
        staffLabel="Synthetic Applicant"
        canManage={false}
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "View posting history" }));
  expect(
    await screen.findByRole("heading", { name: "Immutable posting history" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("region", { name: "Posting revision history" })).toHaveTextContent(
    "to 2027-06-30T17:00:00Z",
  );
  expect(screen.queryByRole("button", { name: "Prepare posting draft" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit draft" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.every(([url]) => !url.includes("/organization/"))).toBe(true);
});

it("withholds preparation when the current Staff source is unavailable", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("posting-draft-source")
        ? Response.json(
            { error: "posting_source_unavailable", message: "Synthetic source outage" },
            { status: 503 },
          )
        : Response.json({ data: [], page: { has_more: false, next_cursor: null } }),
    ),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <StaffPostingDraftPanel
        scope={scope}
        staffLabel="Synthetic Applicant"
        canManage
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("Current Staff source is unavailable");
  expect(screen.queryByRole("button", { name: "Prepare posting draft" })).not.toBeInTheDocument();
});
