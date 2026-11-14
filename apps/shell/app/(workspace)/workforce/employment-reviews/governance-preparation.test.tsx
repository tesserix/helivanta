import "@/test/select";
import { screen, within } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { GovernanceEmploymentPreparation } from "./governance-preparation";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-owner",
  hospitalId: "22222222-2222-4222-8222-222222222222",
};
const staffId = "33333333-3333-4333-8333-333333333333";
const target = {
  id: staffId,
  hospital_id: scope.hospitalId,
  employee_code: "SYN-STAFF",
  display_name: "Synthetic applicant",
  employment_status: "inactive",
  staff_version: 3,
  created_at: "2026-11-01T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("selects an applicant and saves hospital terms with exact governance replay", async () => {
  const writes: Record<string, unknown>[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("employment-preparation-targets/search")) return Response.json(page([target]));
    if (url.endsWith("employment-preparation-source"))
      return Response.json({
        hospital_id: scope.hospitalId,
        staff_id: staffId,
        staff_version: 3,
        employment_epoch: 2,
      });
    if (init?.method === "PUT") {
      const input = JSON.parse(String(init.body));
      writes.push(input);
      if (writes.length === 1)
        return Response.json(
          { error: "unavailable", message: "Uncertain result" },
          { status: 503 },
        );
      const { command_id, expected_version, ...terms } = input;
      return Response.json({
        ...terms,
        id: url.split("/").at(-1),
        revision_id: command_id,
        version: expected_version + 1,
        status: "draft",
        hospital_id: scope.hospitalId,
        staff_id: staffId,
        proposed_by_subject: scope.subject,
        actor_subject: scope.subject,
        created_at: "2026-11-01T00:00:00Z",
        updated_at: "2026-11-01T00:00:00Z",
      });
    }
    if (
      new URL(url, "https://synthetic.invalid").pathname.endsWith("employment-preparation-periods")
    )
      return Response.json(page([]));
    throw new Error("Unexpected legacy or direct request: " + url);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentPreparation scope={scope} onBusy={() => {}} onAccessLost={() => {}} />
    </NavigationGuardProvider>,
  );
  expect(fetcher).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Find Staff" }));
  await user.click(await screen.findByRole("button", { name: "Select Synthetic applicant" }));
  await user.click(await screen.findByRole("button", { name: "Prepare employment terms" }));
  const form = screen.getByRole("form", { name: "Employment terms draft" });
  await user.click(within(form).getByLabelText("Duration"));
  await user.click(screen.getByRole("option", { name: "Ongoing" }));
  await user.type(within(form).getByLabelText("Effective start"), "2026-11-01T09:00");
  await user.type(
    within(form).getByLabelText("Appointment or contract reference"),
    "SYN-APPOINTMENT",
  );
  await user.type(within(form).getByLabelText("Reason"), "Synthetic appointment");
  await user.click(within(form).getByRole("button", { name: "Save draft" }));
  await screen.findByText(
    "Could not confirm the save. Retry the same draft to recover its original result.",
  );
  expect(writes).toHaveLength(1);
  await user.click(within(form).getByRole("button", { name: "Retry same draft" }));
  await screen.findByText("Employment terms draft saved.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(writes[0]).toMatchObject({ staff_version: 3, employment_epoch: 2, expected_version: 0 });
  for (const [url, init] of fetcher.mock.calls) {
    expect(url).not.toContain("hr_intake");
    expect(url).not.toContain("person-review-targets");
    expect(url).not.toContain("employment-sponsors");
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  }
});
it("does not fall back to Staff or person browsing after source denial", async () => {
  const lost = vi.fn();
  const fetcher = vi.fn(async () =>
    Response.json({ error: "forbidden", message: "Denied" }, { status: 403 }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentPreparation scope={scope} onBusy={() => {}} onAccessLost={lost} />
    </NavigationGuardProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Find Staff" }));
  await screen.findByRole("alert");
  expect(lost).toHaveBeenCalled();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("requires a fresh selection if Staff changed before the source check", async () => {
  const fetcher = vi.fn(async (url: string) =>
    Response.json(
      url.includes("targets/search")
        ? page([target])
        : url.endsWith("employment-preparation-source")
          ? {
              hospital_id: scope.hospitalId,
              staff_id: staffId,
              staff_version: 4,
              employment_epoch: 2,
            }
          : page([]),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentPreparation scope={scope} onBusy={() => {}} onAccessLost={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Find Staff" }));
  await user.click(await screen.findByRole("button", { name: "Select Synthetic applicant" }));
  await screen.findByText(
    "Staff details changed. Choose the Staff record again before preparing terms.",
  );
  expect(
    screen.queryByRole("button", { name: "Prepare employment terms" }),
  ).not.toBeInTheDocument();
  expect(fetcher.mock.calls.every(([url]) => String(url).includes("employment-preparation"))).toBe(
    true,
  );
});

it("rechecks a selected terms draft and refuses a concurrent withdrawal", async () => {
  const period = "019a0000-0000-7000-8000-000000000011";
  const terms = {
    id: period,
    revision_id: "019a0000-0000-7000-8000-000000000012",
    version: 1,
    status: "draft",
    hospital_id: scope.hospitalId,
    staff_id: staffId,
    staff_version: 3,
    employment_epoch: 2,
    engagement_kind: "employee",
    duration: "ongoing",
    starts_at: "2026-11-01T00:00:00Z",
    contracting_party: "",
    sponsor_id: "",
    reason: "Synthetic appointment",
    evidence_reference: "SYN-APPOINTMENT",
    proposed_by_subject: scope.subject,
    actor_subject: scope.subject,
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
  };
  const fetcher = vi.fn(async (url: string) =>
    Response.json(
      url.includes("targets/search")
        ? page([target])
        : url.endsWith("employment-preparation-source")
          ? {
              hospital_id: scope.hospitalId,
              staff_id: staffId,
              staff_version: 3,
              employment_epoch: 2,
            }
          : new URL(url, "https://synthetic.invalid").pathname.endsWith(period)
            ? { ...terms, version: 2, status: "withdrawn" }
            : page([terms]),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentPreparation scope={scope} onBusy={() => {}} onAccessLost={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Find Staff" }));
  await user.click(await screen.findByRole("button", { name: "Select Synthetic applicant" }));
  await user.click(await screen.findByRole("button", { name: "Edit SYN-APPOINTMENT" }));
  await screen.findByText(
    "These terms are no longer a draft. Close this form and refresh the terms.",
  );
  expect(screen.queryByRole("form", { name: "Employment terms draft" })).not.toBeInTheDocument();
});

it("opens selected employment files using the current Staff version and epoch", async () => {
  const fetcher = vi.fn(async (url: string) =>
    Response.json(
      url.includes("targets/search")
        ? page([target])
        : url.endsWith("employment-preparation-source")
          ? {
              hospital_id: scope.hospitalId,
              staff_id: staffId,
              staff_version: 3,
              employment_epoch: 2,
            }
          : page([]),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentPreparation scope={scope} onBusy={() => {}} onAccessLost={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Find Staff" }));
  await user.click(await screen.findByRole("button", { name: "Select Synthetic applicant" }));
  await user.click(await screen.findByRole("button", { name: "Prepare employment files" }));
  await screen.findByText("No employment files prepared.");
  expect(
    fetcher.mock.calls.some(([url]) =>
      url.includes(
        `/staff/${staffId}/employment-preparation-documents?staff_version=3&employment_epoch=2`,
      ),
    ),
  ).toBe(true);
});

it("opens evidence from the selected canonical terms revision", async () => {
  const terms = {
    id: "019a0000-0000-7000-8000-000000000011",
    revision_id: "019a0000-0000-7000-8000-000000000012",
    version: 1,
    status: "draft",
    hospital_id: scope.hospitalId,
    staff_id: staffId,
    staff_version: 3,
    employment_epoch: 2,
    engagement_kind: "employee",
    duration: "ongoing",
    starts_at: "2026-11-01T00:00:00Z",
    contracting_party: "",
    sponsor_id: "",
    reason: "Synthetic terms",
    evidence_reference: "SYN-TERMS",
    proposed_by_subject: scope.subject,
    actor_subject: scope.subject,
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
  };
  const fetcher = vi.fn(async (url: string) =>
    Response.json(
      url.includes("targets/search")
        ? page([target])
        : url.endsWith("employment-preparation-source")
          ? {
              hospital_id: scope.hospitalId,
              staff_id: staffId,
              staff_version: 3,
              employment_epoch: 2,
            }
          : new URL(url, "https://synthetic.invalid").pathname.endsWith(terms.id)
            ? terms
            : url.includes("/evidence?")
              ? page([])
              : page([terms]),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentPreparation scope={scope} onBusy={() => {}} onAccessLost={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Find Staff" }));
  await user.click(await screen.findByRole("button", { name: "Select Synthetic applicant" }));
  await user.click(await screen.findByRole("button", { name: "Terms history for SYN-TERMS" }));
  await screen.findByText("Terms revision 1 · draft");
  await user.click(await screen.findByRole("button", { name: "Review evidence for SYN-TERMS" }));
  await screen.findByText("No evidence bound to this terms revision.");
  expect(
    fetcher.mock.calls.some(([url]) =>
      url.includes(
        `/employment-preparation-periods/${terms.id}/revisions/1/evidence?staff_version=3&employment_epoch=2`,
      ),
    ),
  ).toBe(true);
});
