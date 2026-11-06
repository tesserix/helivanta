import { apiFetch } from "./client";
import { listStaffDepartmentTeam } from "./staff-posting-proposals";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const scope = {
  access: "scoped" as const,
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-team-reviewer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
};
const id = (n: number) => `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const item = {
  posting_id: id(1),
  revision_id: id(2),
  hospital_id: scope.hospitalId,
  staff: { id: id(3), employee_code: "SYN-01", display_name: "Synthetic Supervisor" },
  version: 2,
  department_id: id(4),
  department_revision_id: id(5),
  starts_at: "2026-11-01T00:00:00Z",
  ends_at: null,
  responsibility: "department_supervisor",
  supervisor_staff_id: id(6),
  supervisor_posting_id: id(7),
  state: "current",
  checked_at: "2026-11-06T00:00:00Z",
  source_until: "2026-11-07T00:00:00Z",
  created_at: "2026-11-01T00:00:00Z",
};
const page = { data: [item], page: { has_more: false, next_cursor: null } };
beforeEach(() => vi.mocked(apiFetch).mockReset());
it("loads minimized current team observations through the scoped Global API", async () => {
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await listStaffDepartmentTeam(scope)).toEqual(page);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/department-team?limit=8`,
    expect.objectContaining({ cache: "no-store" }),
  );
});
it.each([
  { ...item, hospital_id: id(9) },
  { ...item, source_until: null },
  { ...item, source_until: item.checked_at },
  { ...item, starts_at: "2026-11-08T00:00:00Z" },
  { ...item, ends_at: item.checked_at },
  { ...item, state: "not_current", source_until: item.source_until },
  { ...item, reason: "Private HR rationale" },
])("rejects foreign, stale or over-disclosing team observations", async (value) => {
  vi.mocked(apiFetch).mockResolvedValue({ ...page, data: [value] });
  await expect(listStaffDepartmentTeam(scope)).rejects.toThrow();
});
it("retains an explicit non-current observation without declaring the team empty", async () => {
  const observed = { ...page, data: [{ ...item, state: "not_current", source_until: null }] };
  vi.mocked(apiFetch).mockResolvedValue(observed);
  expect(await listStaffDepartmentTeam(scope)).toEqual(observed);
});
it("rejects duplicate posting identities and contradictory pagination", async () => {
  for (const response of [
    { ...page, data: [item, item] },
    { ...page, page: { has_more: true, next_cursor: null } },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue(response);
    await expect(listStaffDepartmentTeam(scope)).rejects.toThrow();
  }
});
