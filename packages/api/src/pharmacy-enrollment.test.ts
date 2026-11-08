import { afterEach, expect, it, vi } from "vitest";
import { EMPTY_ENROLLMENT, type EnrollmentInput } from "./enrollment";
import { getPharmacyEnrollment, savePharmacyEnrollment } from "./pharmacy-enrollment";

const pharmacy = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const other = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const staffId = "019ccccc-cccc-7ccc-8ccc-cccccccccccc";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "user-admin" };
const staff = {
  id: staffId,
  pharmacy_id: pharmacy,
  employee_code: "SYN-PH-1",
  display_name: "Synthetic pharmacist",
  job_category: "pharmacist",
  employment_status: "active",
  created_at: "2026-11-08T00:00:00Z",
  updated_at: "2026-11-08T00:00:00Z",
} as const;
const profile = {
  given_name: "",
  family_name: "",
  preferred_name: "",
  date_of_birth: "",
  gender: "",
  work_email: "",
  personal_email: "",
  mobile_phone: "",
  work_phone: "",
  preferred_language: "",
  blood_group: "",
  engagement_type: "employee",
  department: "Dispensary",
  designation: "Pharmacist",
  grade: "",
  department_revision_id: "",
  designation_revision_id: "",
  grade_revision_id: "",
  supervisor_id: "",
  start_date: "2024-02-01",
  end_date: "",
  probation_end_date: "",
  confirmation_date: "",
  exit_reason: "",
};
const document = {
  staff,
  version: 2,
  request_context_version: "v1",
  profile,
  addresses: [],
  emergency_contacts: [],
  credentials: [],
  qualifications: [],
  experiences: [],
};
const input: EnrollmentInput = {
  ...EMPTY_ENROLLMENT,
  expected_version: 2,
  staff: {
    employee_code: staff.employee_code,
    display_name: staff.display_name,
    job_category: staff.job_category,
    employment_status: staff.employment_status,
  },
  profile: { ...EMPTY_ENROLLMENT.profile, designation: "Pharmacist" },
};
const respond = (body: unknown) => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(body));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
};
afterEach(() => vi.unstubAllGlobals());

it("reads one pharmacy enrollment through the versioned request context under the expected account", async () => {
  const fetcher = respond(document);
  expect(await getPharmacyEnrollment(scope, pharmacy.toUpperCase(), staffId.toUpperCase())).toEqual(
    document,
  );
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/workforce/pharmacies/${pharmacy}/staff/${staffId}/enrollment?request_context=v1`,
  );
  const init = fetcher.mock.calls[0]?.[1];
  expect(init).toMatchObject({ cache: "no-store" });
  expect(init.method).toBeUndefined();
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("refuses an enrollment that is not this pharmacy's, carries hospital keys or a master projection", async () => {
  const refusals: Record<string, unknown> = {
    "another pharmacy's Staff": { ...document, staff: { ...staff, pharmacy_id: other } },
    "another Staff record": { ...document, staff: { ...staff, id: other } },
    "a hospital Staff shape": { ...document, staff: { ...staff, hospital_id: pharmacy } },
    "a hospital key": { ...document, hospital_id: pharmacy },
    "a profile masters marker": { ...document, profile_masters_version: "v1" },
    "a qualification masters marker": { ...document, qualification_masters_version: "v1" },
    "a contact masters marker": { ...document, contact_masters_version: "v1" },
    "an unversioned view": { ...document, request_context_version: undefined },
    "a later view version": { ...document, request_context_version: "v2" },
    "an unsaved version": { ...document, version: 0 },
  };
  for (const [reason, body] of Object.entries(refusals)) {
    respond(body);
    await expect(getPharmacyEnrollment(scope, pharmacy, staffId), reason).rejects.toThrow(
      "Could not verify this Staff enrollment in this pharmacy.",
    );
  }
  await expect(getPharmacyEnrollment(scope, "not-a-pharmacy", staffId)).rejects.toThrow();
  await expect(getPharmacyEnrollment(scope, pharmacy, "not-a-staff-id")).rejects.toThrow();
});

it("saves the enrollment with its version and request context, and verifies what was stored", async () => {
  const saved = { ...document, version: 3 };
  const fetcher = respond(saved);
  expect(await savePharmacyEnrollment(scope, pharmacy, staffId, input)).toEqual(saved);
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/workforce/pharmacies/${pharmacy}/staff/${staffId}/enrollment?request_context=v1`,
  );
  const init = fetcher.mock.calls[0]?.[1];
  expect(init).toMatchObject({ cache: "no-store", method: "PUT" });
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(JSON.parse(init.body)).toEqual({ ...input, request_context_version: "v1" });

  const draftKept =
    "Could not verify the saved enrollment. Your draft has been kept. Reload to check the current record.";
  for (const [reason, body] of Object.entries({
    "an older version": { ...document, version: 1 },
    "another pharmacy": { ...saved, staff: { ...staff, pharmacy_id: other } },
    "a different Staff name": { ...saved, staff: { ...staff, display_name: "Someone else" } },
    "a different employment status": {
      ...saved,
      staff: { ...staff, employment_status: "inactive" },
    },
    "a master projection": { ...saved, profile_masters_version: "v1" },
  })) {
    respond(body);
    await expect(savePharmacyEnrollment(scope, pharmacy, staffId, input), reason).rejects.toThrow(
      draftKept,
    );
  }
});
