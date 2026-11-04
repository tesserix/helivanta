import { apiFetch } from "./client";
import * as postings from "./staff-posting-proposals";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const scope = {
  access: "scoped" as const,
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-submitter",
  hospitalId: "22222222-2222-4222-8222-222222222222",
};
const choice = {
  id: "019a0000-0000-7000-8000-000000000001",
  revision_id: "019a0000-0000-7000-8000-000000000002",
  hospital_id: scope.hospitalId,
  staff: {
    id: "33333333-3333-4333-8333-333333333333",
    employee_code: "SYN-SUP-1",
    display_name: "Synthetic canonical supervisor",
  },
  kind: "hospital_management",
  department_id: "019a0000-0000-7000-8000-000000000003",
  department_revision_id: "019a0000-0000-7000-8000-000000000004",
  starts_at: "2026-11-01T00:00:00Z",
  ends_at: null,
  eligibility_until: "2028-11-01T00:00:00Z",
  created_at: "2026-11-04T00:00:00Z",
};
afterEach(() => vi.resetAllMocks());
it("reads canonical supervisor hints with the original account and explicit purpose", async () => {
  const page = { data: [choice], page: { has_more: false, next_cursor: null } };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await postings.listStaffPostingSupervisorChoices(scope, "submission")).toEqual(page);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-supervisor-choices?purpose=submission&limit=8`,
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
});

it("withholds canonical Staff labels from a different hospital response", async () => {
  vi.mocked(apiFetch).mockResolvedValue({
    data: [{ ...choice, hospital_id: scope.tenantId }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(postings.listStaffPostingSupervisorChoices(scope, "review")).rejects.toThrow(
    "hospital",
  );
});

it.each([
  { data: [choice, choice], page: { has_more: false, next_cursor: null } },
  { data: [], page: { has_more: true, next_cursor: "next" } },
  { data: [choice], page: { has_more: true, next_cursor: null } },
  { data: [choice], page: { has_more: false, next_cursor: "next" } },
  { data: [choice], page: { has_more: true, next_cursor: "previous" } },
  {
    data: [
      choice,
      { ...choice, id: "019a0000-0000-7000-8000-000000000005", created_at: "2026-11-04T00:00:01Z" },
    ],
    page: { has_more: false, next_cursor: null },
  },
])("withholds an unverifiable supervisor continuation", async (page) => {
  vi.mocked(apiFetch).mockResolvedValue(page);
  await expect(
    postings.listStaffPostingSupervisorChoices(scope, "submission", "previous"),
  ).rejects.toThrow();
});

it.each([
  { id: scope.hospitalId },
  { revision_id: scope.hospitalId },
  { staff: { ...choice.staff, id: "00000000-0000-0000-0000-000000000000" } },
  { department_id: scope.hospitalId },
  { department_revision_id: scope.hospitalId },
  { starts_at: "0001-01-01T00:00:00Z" },
  { ends_at: choice.starts_at },
  { eligibility_until: choice.starts_at },
  { created_at: "0001-01-01T00:00:00Z" },
  { kind: "department_member" },
  { reason: "Private ancestor rationale" },
])("withholds an inconsistent reviewed supervisor reference", async (change) => {
  vi.mocked(apiFetch).mockResolvedValue({
    data: [{ ...choice, ...change }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(postings.listStaffPostingSupervisorChoices(scope, "review")).rejects.toThrow();
});

it("preserves an expired historical reference as a hint rather than asserting fresh eligibility", async () => {
  const historical = {
    ...choice,
    created_at: "2026-11-01T00:00:01Z",
    eligibility_until: "2026-11-02T00:00:00Z",
  };
  vi.mocked(apiFetch).mockResolvedValue({
    data: [historical],
    page: { has_more: false, next_cursor: null },
  });
  expect((await postings.listStaffPostingSupervisorChoices(scope, "submission")).data).toEqual([
    historical,
  ]);
});
