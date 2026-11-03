import { apiFetch } from "./client";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-reader" };
const hospitalId = "22222222-2222-4222-8222-222222222222";
const target = {
  id: "33333333-3333-4333-8333-333333333333",
  employee_code: "SYN-1",
  display_name: "Synthetic Staff",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.resetAllMocks());
it("pins bounded scoped hospital discovery and retains empty-page continuation", async () => {
  const { listStaffPostingScopes } = await import("./staff-posting-workspace");
  const continuation = { data: [], page: { has_more: true, next_cursor: "opaque-continuation" } };
  vi.mocked(apiFetch).mockResolvedValue(continuation);
  expect(await listStaffPostingScopes(scope)).toEqual(continuation);
  expect(apiFetch).toHaveBeenCalledWith(
    "/workforce/posting-scopes?limit=20",
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  vi.mocked(apiFetch).mockResolvedValue(
    page([{ hospital_id: hospitalId, name: "Synthetic hospital", can_prepare: false }]),
  );
  expect((await listStaffPostingScopes(scope)).data[0].can_prepare).toBe(false);
});
it("refuses private fields, duplicate targets and invalid continuation", async () => {
  const { listStaffPostingTargets } = await import("./staff-posting-workspace");
  vi.mocked(apiFetch).mockResolvedValue(page([target]));
  expect((await listStaffPostingTargets({ ...scope, hospitalId })).data).toEqual([target]);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${hospitalId}/posting-targets?limit=50`,
    expect.anything(),
  );
  for (const bad of [
    page([{ ...target, given_name: "Private" }]),
    page([target, target]),
    page([{ ...target, id: "00000000-0000-0000-0000-000000000000" }]),
    { data: [target], page: { has_more: false, next_cursor: "invalid" } },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue(bad);
    await expect(listStaffPostingTargets({ ...scope, hospitalId })).rejects.toThrow();
  }
});
