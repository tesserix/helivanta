import { apiFetch } from "./client";
import { StaffPostingDraftInputContract } from "./staff-postings-contract";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-manager",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const draft = "019a0000-0000-7000-8000-000000000001";
const command = "019a0000-0000-7000-8000-000000000002";
const input = {
  expected_version: 0,
  staff_version: 1,
  employment_epoch: 1,
  department_id: "019a0000-0000-7000-8000-000000000003",
  department_revision_id: "019a0000-0000-7000-8000-000000000004",
  starts_at: "2027-01-01T00:00:00Z",
  reason: "Synthetic placement preparation",
};
const publicReceipt = {
  staff_version: input.staff_version,
  employment_epoch: input.employment_epoch,
  department_id: input.department_id,
  department_revision_id: input.department_revision_id,
  starts_at: input.starts_at,
  id: draft,
  revision_id: "019a0000-0000-7000-8000-000000000005",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 1,
  status: "draft",
  grants_access: false,
  created_at: "2026-11-02T00:00:00Z",
  recorded_at: "2026-11-02T00:00:00Z",
};

afterEach(() => vi.resetAllMocks());

it("rejects malformed draft intent before transmission", () => {
  expect(StaffPostingDraftInputContract.safeParse(input).success).toBe(true);
  for (const change of [
    { expected_version: 9007199254740990 },
    { department_id: scope.hospitalId },
    { department_revision_id: scope.hospitalId },
    { starts_at: "0001-01-01T00:00:00.0000001Z" },
    { ends_at: input.starts_at },
    { reason: "Synthetic\nmultiline" },
  ]) {
    expect(StaffPostingDraftInputContract.safeParse({ ...input, ...change }).success).toBe(false);
  }
});

it("pins source reads and rejects a different Staff source", async () => {
  const { getStaffPostingDraftSource } = await import("./staff-postings");
  const source = {
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    staff_version: 3,
    employment_epoch: 2,
  };
  vi.mocked(apiFetch).mockResolvedValue(source);
  expect(await getStaffPostingDraftSource(scope)).toEqual(source);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/posting-draft-source`,
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...source, staff_id: scope.hospitalId });
  await expect(getStaffPostingDraftSource(scope)).rejects.toThrow("source");
});

it("retains the exact command and verifies scope, intent and non-operating receipt", async () => {
  const { saveStaffPostingDraft } = await import("./staff-postings");
  vi.mocked(apiFetch).mockResolvedValue(publicReceipt);
  expect(await saveStaffPostingDraft(scope, draft, command, input)).toEqual(publicReceipt);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/posting-drafts/${draft}/commands/${command}`,
    expect.objectContaining({ method: "PUT", body: JSON.stringify(input) }),
  );
  for (const change of [
    { id: command },
    { staff_id: scope.hospitalId },
    { version: 2 },
    { grants_access: true },
    { department_revision_id: command },
    { starts_at: "2027-02-01T00:00:00Z" },
    { ends_at: "2027-06-01T00:00:00Z" },
    { employment_epoch: 2 },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...publicReceipt, ...change });
    await expect(saveStaffPostingDraft(scope, draft, command, input)).rejects.toThrow("receipt");
  }
});

it("recovers the exact retained command without replacing it with the latest revision", async () => {
  const { getStaffPostingDraftReceipt } = await import("./staff-postings");
  vi.mocked(apiFetch).mockResolvedValue(publicReceipt);
  expect(await getStaffPostingDraftReceipt(scope, draft, command, input)).toEqual(publicReceipt);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/${draft}/commands/${command}`),
    expect.objectContaining({ cache: "no-store" }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...publicReceipt, id: command });
  await expect(getStaffPostingDraftReceipt(scope, draft, command, input)).rejects.toThrow(
    "receipt",
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...publicReceipt, version: 2 });
  await expect(getStaffPostingDraftReceipt(scope, draft, command, input)).rejects.toThrow(
    "receipt",
  );
});

it("checks paged history and refuses duplicate revisions and a repeated cursor", async () => {
  const { listStaffPostingDraftHistory } = await import("./staff-postings");
  const page = { data: [publicReceipt], page: { has_more: false, next_cursor: null } };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await listStaffPostingDraftHistory(scope, draft)).toEqual(page);
  vi.mocked(apiFetch).mockResolvedValue({ ...page, data: [publicReceipt, publicReceipt] });
  await expect(listStaffPostingDraftHistory(scope, draft)).rejects.toThrow("page");
  vi.mocked(apiFetch).mockResolvedValue({
    ...page,
    page: { has_more: true, next_cursor: "same-cursor" },
  });
  await expect(listStaffPostingDraftHistory(scope, draft, "same-cursor")).rejects.toThrow("page");
});

it("lists only drafts for the exact Staff workspace", async () => {
  const { listStaffPostingDrafts } = await import("./staff-postings");
  const page = { data: [publicReceipt], page: { has_more: false, next_cursor: null } };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await listStaffPostingDrafts(scope)).toEqual(page);
  vi.mocked(apiFetch).mockResolvedValue({
    ...page,
    data: [{ ...publicReceipt, staff_id: scope.hospitalId }],
  });
  await expect(listStaffPostingDrafts(scope)).rejects.toThrow("receipt");
});

it("isolates scoped posting paths and caches without legacy fallback after denial", async () => {
  const { getStaffPostingDraftSource, saveStaffPostingDraft, staffPostingsKey } =
    await import("./staff-postings");
  const scoped = { ...scope, access: "scoped" as const };
  expect(staffPostingsKey(scoped)).not.toEqual(staffPostingsKey(scope));
  vi.mocked(apiFetch).mockResolvedValue({
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    staff_version: 1,
    employment_epoch: 1,
  });
  await getStaffPostingDraftSource(scoped);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-draft-source`,
    expect.anything(),
  );
  vi.mocked(apiFetch).mockResolvedValue(publicReceipt);
  await saveStaffPostingDraft(scoped, draft, command, input);
  expect(apiFetch).toHaveBeenLastCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-drafts/${draft}/commands/${command}`,
    expect.objectContaining({ method: "PUT" }),
  );
  vi.mocked(apiFetch).mockClear().mockRejectedValue(new Error("access denied"));
  await expect(getStaffPostingDraftSource(scoped)).rejects.toThrow("access denied");
  expect(apiFetch).toHaveBeenCalledTimes(1);
});
