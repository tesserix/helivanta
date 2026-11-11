import { afterEach, expect, it, vi } from "vitest";
import {
  acknowledgeCredentialReminder,
  citableEvidence,
  credentialReminderPolicyFormSchema,
  credentialRemindersPath,
  parseReminderDays,
  saveCredentialReminderPolicy,
  credentialDecisionFormSchema,
  credentialRegisterKey,
  credentialRegisterPath,
  credentialReminderPolicyKey,
  credentialReminderPolicyPath,
  credentialRemindersKey,
  credentialRequest,
  credentialReviewFormSchema,
  credentialsKey,
  credentialsPath,
  decideCredentialReview,
  fetchPharmacyTimeZone,
  pharmacyTimeZoneFormSchema,
  pharmacyTimeZoneKey,
  pharmacyTimeZonePath,
  proposeCredentialReview,
  savePharmacyTimeZone,
  withdrawCredentialReview,
  type CredentialScope,
} from "./credentials";
import type { PharmacyStaffDocument, StaffDocument } from "./documents-fields";

const hospital = "11111111-1111-4111-8111-111111111111";
const staff = "22222222-2222-4222-8222-222222222222";
const credential = "33333333-3333-4333-8333-333333333333";
const review = "44444444-4444-4444-8444-444444444444";
const file = "55555555-5555-4555-8555-555555555555";
const hospitalOwner = { hospitalId: hospital };
const scope = { ...hospitalOwner, staffId: staff };
const stamp = "2026-10-18T00:00:00Z";
const saved = {
  id: review,
  staff_id: staff,
  credential_id: credential,
  base_version: 0,
  status: "pending",
  type: "Synthetic registration",
  number: "SYN-1",
  issuer: "Synthetic Council",
  valid_from: "2026-01-01",
  valid_until: "2027-12-31",
  evidence_document_ids: [file],
  reason: "Checked the synthetic register",
  proposed_by_subject: "synthetic-hr",
  decision_note: "",
  created_at: stamp,
};
const proposal = {
  expected_version: 0,
  number: "SYN-1",
  valid_from: "2026-01-01",
  valid_until: "2027-12-31",
  evidence_document_ids: [file],
  reason: "Checked the synthetic register",
};
afterEach(() => vi.unstubAllGlobals());

it("needs evidence, a reason and an ordered period before asking the API", () => {
  const result = credentialReviewFormSchema.safeParse({
    ...proposal,
    valid_until: "2025-12-31",
    evidence_document_ids: [],
    reason: " ",
  });
  expect(result.success).toBe(false);
  const messages = Object.fromEntries(
    (result.error?.issues ?? []).map((i) => [i.path.join("."), i.message]),
  );
  expect(messages).toEqual({
    evidence_document_ids: "Choose at least one evidence file.",
    reason: "Say what you checked.",
    valid_until: "Expiry must be on or after the valid-from date.",
  });
  expect(credentialReviewFormSchema.safeParse(proposal).success).toBe(true);
});

it("needs a note only to reject", () => {
  expect(credentialDecisionFormSchema.safeParse({ decision: "approve", note: "" }).success).toBe(
    true,
  );
  const rejected = credentialDecisionFormSchema.safeParse({ decision: "reject", note: "  " });
  expect(rejected.error?.issues[0]?.message).toBe("Say why you reject it.");
});

it("offers only this credential's released or scanning evidence", () => {
  const base = {
    record_kind: "credential",
    record_id: credential,
    category: "credential_evidence",
    status: "available",
  };
  const files = [
    { ...base, id: "a" },
    { ...base, id: "b", status: "scan_pending" },
    { ...base, id: "c", status: "revoked" },
    { ...base, id: "d", record_id: review },
    { ...base, id: "e", record_kind: "qualification" },
  ] as unknown as StaffDocument[];
  expect(citableEvidence(files, credential).map((f) => f.id)).toEqual(["a", "b"]);
  const pharmacyFiles = [
    { ...base, id: "p", pharmacy_id: hospital },
    { ...base, id: "q", pharmacy_id: hospital, status: "rejected" },
  ] as unknown as PharmacyStaffDocument[];
  const cited: PharmacyStaffDocument[] = citableEvidence(pharmacyFiles, credential);
  expect(cited.map((f) => f.id)).toEqual(["p"]);
});

it("builds the register path only for a valid window", () => {
  expect(credentialRegisterPath(hospitalOwner, null)).toBe(
    `/workforce/hospitals/${hospital}/hr/credentials`,
  );
  expect(credentialRegisterPath(hospitalOwner, 30)).toBe(
    `/workforce/hospitals/${hospital}/hr/credentials?expiring_within=30`,
  );
  expect(() => credentialRegisterPath(hospitalOwner, 366)).toThrow();
});

it("proposes to the review id and refuses a receipt for another credential", async () => {
  const calls: { url: string; method?: string; body?: unknown }[] = [];
  let body: unknown = saved;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, method: init.method, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  await expect(proposeCredentialReview(scope, credential, review, proposal)).resolves.toEqual(
    saved,
  );
  expect(calls[0]).toEqual({
    url: `/api/v1/workforce/hospitals/${hospital}/staff/${staff}/credentials/${credential}/reviews/${review}`,
    method: "PUT",
    body: proposal,
  });
  body = { ...saved, credential_id: file };
  await expect(
    decideCredentialReview(scope, credential, review, {
      expected_version: 0,
      decision: "approve",
      note: "",
    }),
  ).rejects.toThrow("The saved review did not match this credential.");
  expect(calls[1]?.url).toMatch(/\/reviews\/[0-9a-f-]+\/decision$/);
});

it("accepts 1 to 6 distinct whole reminder days from 0 to 365", () => {
  const message = (days: string) =>
    credentialReminderPolicyFormSchema.safeParse({ days }).error?.issues[0]?.message;
  expect(message("90, 30, 0")).toBeUndefined();
  expect(parseReminderDays("90, 30, 0")).toEqual([90, 30, 0]);
  expect(message(" ")).toBe("Enter at least one day.");
  expect(message("30, 1.5")).toBe("Use whole days from 0 to 365, separated by commas.");
  expect(message("366")).toBe("Use whole days from 0 to 365, separated by commas.");
  expect(message("-1")).toBe("Use whole days from 0 to 365, separated by commas.");
  expect(message("30,,0")).toBe("Use whole days from 0 to 365, separated by commas.");
  expect(message("1,2,3,4,5,6,7")).toBe("Use up to 6 reminder days.");
  expect(message("30, 30")).toBe("List each day once.");
});

it("filters the reminder inbox by a known status only", () => {
  expect(credentialRemindersPath(hospitalOwner, "open")).toBe(
    `/workforce/hospitals/${hospital}/hr/credential-reminders?status=open`,
  );
  expect(() => credentialRemindersPath(hospitalOwner, "closed" as "open")).toThrow();
});

it("saves the policy against a version and refuses a receipt for another reminder", async () => {
  const calls: { url: string; method?: string; body?: string }[] = [];
  let body: unknown = { version: 2, thresholds: [30, 0], created_by_subject: "synthetic-hr" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, method: init.method, body: init.body as string | undefined });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  await expect(saveCredentialReminderPolicy(hospitalOwner, 1, [30, 0])).resolves.toMatchObject({
    version: 2,
  });
  expect(calls[0]).toEqual({
    url: `/api/v1/workforce/hospitals/${hospital}/hr/credential-reminder-policy`,
    method: "PUT",
    body: JSON.stringify({ expected_version: 1, thresholds: [30, 0] }),
  });
  body = {
    id: file,
    staff_id: staff,
    employee_code: "SYN-001",
    display_name: "Synthetic Nurse",
    credential_id: credential,
    type: "Synthetic registration",
    valid_until: "2026-12-31",
    validity_version: 1,
    threshold_days: 30,
    due_on: "2026-12-01",
    status: "acknowledged",
    created_at: stamp,
  };
  await expect(acknowledgeCredentialReminder(hospitalOwner, review)).rejects.toThrow(
    "The acknowledged reminder did not match.",
  );
  expect(calls[1]).toMatchObject({
    url: `/api/v1/workforce/hospitals/${hospital}/hr/credential-reminders/${review}/acknowledgement`,
    method: "POST",
  });
  await expect(acknowledgeCredentialReminder(hospitalOwner, file)).resolves.toMatchObject({
    status: "acknowledged",
  });
});

const pharmacy = "01900000-0000-7000-8000-000000000001";
const tenant = "66666666-6666-4666-8666-666666666666";
const pharmacyOwner = { pharmacyId: pharmacy, tenantId: tenant, subject: "synthetic-hr" };
const pharmacyScope = { ...pharmacyOwner, staffId: staff };
const expected = {
  "X-Helivanta-Expected-Tenant": tenant,
  "X-Helivanta-Expected-Subject": "synthetic-hr",
};
const headersOf = (init: RequestInit) => Object.fromEntries(new Headers(init.headers).entries());
const lower = (headers: Record<string, string>) =>
  Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]));

it("routes a pharmacy scope to the pharmacy family and keys it apart from a hospital", () => {
  const base = `/workforce/pharmacies/${pharmacy}`;
  expect(credentialsPath(pharmacyScope)).toBe(`${base}/staff/${staff}/credentials`);
  expect(credentialRegisterPath(pharmacyOwner, 30)).toBe(
    `${base}/hr/credentials?expiring_within=30`,
  );
  expect(credentialReminderPolicyPath(pharmacyOwner)).toBe(`${base}/hr/credential-reminder-policy`);
  expect(credentialRemindersPath(pharmacyOwner, "open")).toBe(
    `${base}/hr/credential-reminders?status=open`,
  );
  expect(pharmacyTimeZonePath(pharmacyOwner)).toBe(`${base}/hr/time-zone`);
  const same = { hospitalId: pharmacy, staffId: staff };
  expect(credentialsKey(pharmacyScope)).not.toEqual(credentialsKey(same));
  for (const key of [
    credentialsKey(pharmacyScope),
    credentialRegisterKey(pharmacyOwner),
    credentialReminderPolicyKey(pharmacyOwner),
    credentialRemindersKey(pharmacyOwner),
    pharmacyTimeZoneKey(pharmacyOwner),
  ])
    expect(key).toEqual(expect.arrayContaining(["pharmacy", pharmacy, tenant, "synthetic-hr"]));
  expect(credentialsKey(scope)).toEqual(["workforce", "credentials", "hospital", hospital, staff]);
});

it("refuses a scope naming both families or an unverifiable pharmacy account", () => {
  // @ts-expect-error A scope names exactly one establishment family.
  const mixed: CredentialScope = { ...pharmacyScope, hospitalId: hospital };
  expect(() => credentialsPath(mixed)).toThrow("The credential workspace could not be verified.");
  for (const bad of [
    { ...pharmacyScope, pharmacyId: hospital },
    { ...pharmacyScope, tenantId: "not-a-tenant" },
    { ...pharmacyScope, subject: " synthetic-hr" },
    { ...pharmacyScope, subject: "" },
    { ...pharmacyScope, subject: "synthetic\u0000hr" },
  ])
    expect(() => credentialRequest(bad)).toThrow("The credential workspace could not be verified.");
  expect(credentialRequest(scope)).toEqual({ cache: "no-store" });
  expect(credentialRequest(pharmacyScope)).toEqual({ cache: "no-store", headers: expected });
});

it("sends the expected account on every pharmacy command and none on a hospital", async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  let body: unknown = saved;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const reviews = `/api/v1/workforce/pharmacies/${pharmacy}/staff/${staff}/credentials/${credential}/reviews/${review}`;
  await proposeCredentialReview(pharmacyScope, credential, review, proposal);
  await decideCredentialReview(pharmacyScope, credential, review, {
    expected_version: 0,
    decision: "approve",
    note: "",
  });
  await withdrawCredentialReview(pharmacyScope, credential, review, { note: "Withdrawn" });
  body = { version: 2, thresholds: [30, 0], created_by_subject: "synthetic-hr" };
  await saveCredentialReminderPolicy(pharmacyOwner, 1, [30, 0]);
  body = {
    id: file,
    staff_id: staff,
    employee_code: "SYN-001",
    display_name: "Synthetic Pharmacist",
    credential_id: credential,
    type: "Synthetic registration",
    valid_until: "2026-12-31",
    validity_version: 1,
    threshold_days: 30,
    due_on: "2026-12-01",
    status: "acknowledged",
    created_at: stamp,
  };
  await acknowledgeCredentialReminder(pharmacyOwner, file);
  expect(calls.map((c) => [c.init.method, c.url])).toEqual([
    ["PUT", reviews],
    ["POST", `${reviews}/decision`],
    ["POST", `${reviews}/withdraw`],
    ["PUT", `/api/v1/workforce/pharmacies/${pharmacy}/hr/credential-reminder-policy`],
    [
      "POST",
      `/api/v1/workforce/pharmacies/${pharmacy}/hr/credential-reminders/${file}/acknowledgement`,
    ],
  ]);
  for (const call of calls) expect(headersOf(call.init)).toMatchObject(lower(expected));
  calls.length = 0;
  body = saved;
  await proposeCredentialReview(scope, credential, review, proposal);
  expect(Object.keys(headersOf(calls[0]!.init))).not.toEqual(
    expect.arrayContaining(Object.keys(lower(expected))),
  );
});

it("saves a pharmacy time zone against a version and refuses another receipt", async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  let body: unknown = {
    version: 1,
    time_zone: "Asia/Kolkata",
    updated_by_subject: "synthetic-hr",
    updated_at: stamp,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  await expect(savePharmacyTimeZone(pharmacyOwner, 0, "Asia/Kolkata")).resolves.toMatchObject({
    version: 1,
    time_zone: "Asia/Kolkata",
  });
  expect(calls[0]?.url).toBe(`/api/v1/workforce/pharmacies/${pharmacy}/hr/time-zone`);
  expect(calls[0]?.init.method).toBe("PUT");
  expect(calls[0]?.init.body).toBe(
    JSON.stringify({ expected_version: 0, time_zone: "Asia/Kolkata" }),
  );
  expect(headersOf(calls[0]!.init)).toMatchObject(lower(expected));
  body = {
    version: 1,
    time_zone: "Europe/London",
    updated_by_subject: "synthetic-hr",
    updated_at: stamp,
  };
  await expect(savePharmacyTimeZone(pharmacyOwner, 0, "Asia/Kolkata")).rejects.toThrow(
    "The saved time zone did not match. Reload before saving again.",
  );
  body = { version: 0, time_zone: "Asia/Kolkata" };
  await expect(savePharmacyTimeZone(pharmacyOwner, 0, "Asia/Kolkata")).rejects.toThrow(
    "The saved time zone did not match. Reload before saving again.",
  );
});

it("accepts only a supported IANA time zone in the form", () => {
  const message = (time_zone: string) =>
    pharmacyTimeZoneFormSchema.safeParse({ time_zone }).error?.issues[0]?.message;
  expect(message("Asia/Kolkata")).toBeUndefined();
  expect(message("UTC")).toBeUndefined();
  for (const bad of ["", "Local", "posix/Asia/Kolkata", "Mars/Olympus_Mons", "asia/kolkata"])
    expect(message(bad)).toBe("Use a supported IANA time zone such as Asia/Kolkata.");
});

it("reads a pharmacy time zone under the expected account and refuses an inconsistent answer", async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  let body: unknown = { version: 0 };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  await expect(fetchPharmacyTimeZone(pharmacyOwner)).resolves.toEqual({ version: 0 });
  expect(calls[0]?.url).toBe(`/api/v1/workforce/pharmacies/${pharmacy}/hr/time-zone`);
  expect(calls[0]?.init.method ?? "GET").toBe("GET");
  expect(calls[0]?.init.cache).toBe("no-store");
  expect(headersOf(calls[0]!.init)).toMatchObject(lower(expected));
  const set = {
    version: 2,
    time_zone: "Asia/Kolkata",
    updated_by_subject: "synthetic-hr",
    updated_at: stamp,
  };
  body = set;
  await expect(fetchPharmacyTimeZone(pharmacyOwner)).resolves.toEqual(set);
  for (const bad of [
    { version: 0, time_zone: "Asia/Kolkata" },
    { version: 1 },
    { version: -1 },
    { ...set, time_zone: "Local" },
    { ...set, hospital_id: hospital },
  ]) {
    body = bad;
    await expect(fetchPharmacyTimeZone(pharmacyOwner)).rejects.toThrow();
  }
  await expect(
    fetchPharmacyTimeZone({ ...pharmacyOwner, subject: " synthetic-hr" }),
  ).rejects.toThrow("The credential workspace could not be verified.");
});
