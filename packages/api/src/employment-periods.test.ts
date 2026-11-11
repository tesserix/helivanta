import { apiFetch } from "./client";
import {
  saveEmploymentPeriodDraft,
  withdrawEmploymentPeriod,
  employmentPeriodsKey,
  employmentSourcePath,
  employmentSponsorsPath,
  ownedEmploymentSource,
  ownedEmploymentTerms,
} from "./employment-periods";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-admin",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const id = "019a0000-0000-7000-8000-000000000001";
const input = {
  command_id: "019a0000-0000-7000-8000-000000000002",
  expected_version: 0,
  staff_version: 1,
  employment_epoch: 1,
  engagement_kind: "employee" as const,
  duration: "ongoing" as const,
  starts_at: "2026-11-01T00:00:00Z",
  contracting_party: "",
  sponsor_id: "",
  reason: "Synthetic onboarding",
  evidence_reference: "SYN-OFFER",
};
afterEach(() => vi.resetAllMocks());
it("pins the Staff workspace and verifies the owner's receipt", async () => {
  const { command_id: commandId, expected_version: expectedVersion, ...terms } = input;
  const receipt = {
    ...terms,
    id,
    revision_id: commandId,
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    version: expectedVersion + 1,
    status: "draft",
    proposed_by_subject: scope.subject,
    actor_subject: scope.subject,
    created_at: "2026-10-29T00:00:00Z",
    updated_at: "2026-10-29T00:00:00Z",
  };
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await saveEmploymentPeriodDraft(scope, id, input)).toMatchObject({
    id,
    version: 1,
    status: "draft",
  });
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/staff/${scope.staffId}/employment-periods/${id}`),
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  await saveEmploymentPeriodDraft({ ...scope, hrIntake: true }, id, input);
  expect(vi.mocked(apiFetch).mock.lastCall?.[0]).toBe(
    `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-periods/${id}?hr_intake=v1`,
  );
  expect(employmentPeriodsKey({ ...scope, hrIntake: true })).not.toEqual(
    employmentPeriodsKey(scope),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, staff_id: scope.hospitalId });
  await expect(saveEmploymentPeriodDraft(scope, id, input)).rejects.toThrow("receipt");
});
it("rejects incomplete contractor terms before transmitting", async () => {
  await expect(
    saveEmploymentPeriodDraft(scope, id, { ...input, engagement_kind: "agency_worker" }),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("checks the terminal withdrawal receipt and pins its command to the workspace", async () => {
  const command = {
    command_id: input.command_id,
    expected_version: 1,
    reason: "Synthetic cancellation",
  };
  const terms = {
    staff_version: input.staff_version,
    employment_epoch: input.employment_epoch,
    engagement_kind: input.engagement_kind,
    duration: input.duration,
    starts_at: input.starts_at,
    contracting_party: input.contracting_party,
    sponsor_id: input.sponsor_id,
    evidence_reference: input.evidence_reference,
  };
  const receipt = {
    ...terms,
    id,
    revision_id: command.command_id,
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    version: 2,
    status: "withdrawn",
    reason: command.reason,
    proposed_by_subject: scope.subject,
    actor_subject: scope.subject,
    created_at: "2026-10-29T00:00:00Z",
    updated_at: "2026-10-29T00:00:00Z",
  };
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await withdrawEmploymentPeriod(scope, id, command)).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/${id}/withdraw`),
    expect.objectContaining({
      method: "POST",
      cache: "no-store",
      body: JSON.stringify(command),
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  await withdrawEmploymentPeriod({ ...scope, hrIntake: true }, id, command);
  expect(vi.mocked(apiFetch).mock.lastCall?.[0]).toBe(
    `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-periods/${id}/withdraw?hr_intake=v1`,
  );
  for (const change of [
    { status: "draft" },
    { staff_id: scope.hospitalId },
    { actor_subject: "other" },
    { revision_id: id },
    { version: 3 },
    { reason: "Changed reason" },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...receipt, ...change });
    await expect(withdrawEmploymentPeriod(scope, id, command)).rejects.toThrow("receipt");
  }
  vi.mocked(apiFetch).mockClear();
  await expect(withdrawEmploymentPeriod(scope, id, { ...command, reason: " " })).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});
describe("pharmacy employment terms (ADR-0054 slice 4b)", () => {
  const pharmacy = {
    tenantId: scope.tenantId,
    subject: scope.subject,
    pharmacyId: "0190a000-0000-7000-8000-000000000005",
    staffId: scope.staffId,
  };
  const draft = (owner: Record<string, string>) => {
    const terms = Object.fromEntries(
      Object.entries(input).filter(([key]) => key !== "command_id" && key !== "expected_version"),
    );
    return {
      ...terms,
      ...owner,
      id,
      revision_id: input.command_id,
      staff_id: scope.staffId,
      version: 1,
      status: "draft",
      proposed_by_subject: scope.subject,
      actor_subject: scope.subject,
      created_at: "2026-10-29T00:00:00Z",
      updated_at: "2026-10-29T00:00:00Z",
    };
  };
  const pharmacyDraft = () => draft({ pharmacy_id: pharmacy.pharmacyId });
  it("saves under the pharmacy family and accepts only a pharmacy receipt", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce(pharmacyDraft());
    expect(await saveEmploymentPeriodDraft(pharmacy, id, input)).toMatchObject({ id, version: 1 });
    expect(apiFetch).toHaveBeenCalledWith(
      `/workforce/pharmacies/${pharmacy.pharmacyId}/staff/${scope.staffId}/employment-periods/${id}`,
      expect.objectContaining({ method: "PUT", cache: "no-store" }),
    );
    vi.mocked(apiFetch).mockResolvedValueOnce(draft({ hospital_id: pharmacy.pharmacyId }));
    await expect(saveEmploymentPeriodDraft(pharmacy, id, input)).rejects.toThrow(
      /Could not verify/,
    );
  });
  it("refuses another pharmacy's receipt", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({
      ...pharmacyDraft(),
      pharmacy_id: "0190a000-0000-7000-8000-000000000006",
    });
    await expect(saveEmploymentPeriodDraft(pharmacy, id, input)).rejects.toThrow(
      /Could not verify/,
    );
  });
  it("withdraws under the pharmacy family", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({
      ...pharmacyDraft(),
      revision_id: "019a0000-0000-7000-8000-000000000009",
      version: 2,
      status: "withdrawn",
      reason: "Synthetic withdrawal",
    });
    await withdrawEmploymentPeriod(pharmacy, id, {
      command_id: "019a0000-0000-7000-8000-000000000009",
      expected_version: 1,
      reason: "Synthetic withdrawal",
    });
    expect(apiFetch).toHaveBeenCalledWith(
      `/workforce/pharmacies/${pharmacy.pharmacyId}/staff/${scope.staffId}/employment-periods/${id}/withdraw`,
      expect.objectContaining({ method: "POST" }),
    );
  });
  it("keys a pharmacy apart from a hospital with the same identifier", () => {
    expect(employmentPeriodsKey(pharmacy)).not.toEqual(
      employmentPeriodsKey({ ...scope, hospitalId: pharmacy.pharmacyId }),
    );
  });
  it("reads only rows owned by the scoped Staff record", () => {
    expect(ownedEmploymentTerms(pharmacy, [pharmacyDraft()])).toHaveLength(1);
    expect(ownedEmploymentTerms(pharmacy, [{ ...pharmacyDraft(), staff_id: id }])).toBeUndefined();
    const hospitalRow = draft({ hospital_id: pharmacy.pharmacyId });
    expect(ownedEmploymentTerms(pharmacy, [hospitalRow])).toBeUndefined();
    expect(ownedEmploymentTerms(scope, [hospitalRow])).toBeUndefined();
    expect(ownedEmploymentTerms(pharmacy, "not rows")).toBeUndefined();
  });
  it("reads the Staff basis and sponsor directory from the owning establishment", () => {
    expect(employmentSourcePath(pharmacy)).toBe(
      `/workforce/pharmacies/${pharmacy.pharmacyId}/staff/${scope.staffId}/employment-source`,
    );
    expect(employmentSponsorsPath(pharmacy)).toBe(
      `/workforce/pharmacies/${pharmacy.pharmacyId}/staff`,
    );
    expect(employmentSourcePath(scope)).toBe(
      `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-source`,
    );
    expect(employmentSponsorsPath(scope)).toBe(`/workforce/hospitals/${scope.hospitalId}/staff`);
  });
  it("accepts only the scoped owner's Staff basis", () => {
    const basis = { staff_id: scope.staffId, staff_version: 2, employment_epoch: 1 };
    expect(ownedEmploymentSource(pharmacy, { ...basis, pharmacy_id: pharmacy.pharmacyId })).toEqual(
      {
        ...basis,
        pharmacy_id: pharmacy.pharmacyId,
      },
    );
    expect(ownedEmploymentSource(scope, { ...basis, hospital_id: scope.hospitalId })).toEqual({
      ...basis,
      hospital_id: scope.hospitalId,
    });
    for (const other of [
      { ...basis, hospital_id: pharmacy.pharmacyId },
      { ...basis, pharmacy_id: "0190a000-0000-7000-8000-000000000006" },
      { ...basis, pharmacy_id: pharmacy.pharmacyId, staff_id: id },
      undefined,
    ])
      expect(ownedEmploymentSource(pharmacy, other)).toBeUndefined();
    expect(
      ownedEmploymentSource(scope, { ...basis, pharmacy_id: scope.hospitalId }),
    ).toBeUndefined();
  });
  it("never sends a pharmacy request with an HR intake view or an ambiguous owner", async () => {
    for (const invalid of [
      { ...pharmacy, hrIntake: true },
      { ...pharmacy, hrIntake: false },
      { ...pharmacy, hospitalId: scope.hospitalId },
      { ...pharmacy, pharmacyId: scope.hospitalId },
    ])
      await expect(saveEmploymentPeriodDraft(invalid as never, id, input)).rejects.toThrow(
        "The employment workspace could not be verified.",
      );
    expect(apiFetch).not.toHaveBeenCalled();
  });
});
