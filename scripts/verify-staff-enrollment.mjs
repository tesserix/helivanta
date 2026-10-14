import assert from "node:assert/strict";

const tenant = process.env.HELIVANTA_VERIFY_TENANT;
assert(
  ["11111111-1111-1111-1111-111111111111", "8f8ed375-596b-4633-b90b-27fd3e6f299c"].includes(tenant),
  "Verification is restricted to designated synthetic tenants",
);
const api = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
assert(api && cookie && hospital, "API, session and synthetic hospital are required");
async function request(path, method = "GET", body) {
  const response = await fetch(api + path, {
    method,
    headers: { "Content-Type": "application/json", Cookie: cookie },
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(20000),
  });
  if (path.startsWith("workforce/"))
    assert.equal(response.headers.get("cache-control"), "no-store");
  return { status: response.status, body: await response.json() };
}
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
assert(
  me.body.data.includes("workforce.enrollment.manage"),
  "Synthetic owner must have enrollment permission",
);
const id = "a7777777-7777-4777-8777-777777777771";
const path = `workforce/hospitals/${hospital}/staff/${id}/enrollment`;
const input = {
  expected_version: 0,
  staff: {
    employee_code: "SYN-ENROLL-101",
    display_name: "Avery Enrollment Synthetic",
    job_category: "nurse",
    employment_status: "active",
  },
  profile: {
    given_name: "Avery",
    family_name: "Synthetic",
    preferred_name: "Avery",
    date_of_birth: "1995-03-15",
    gender: "prefer_not_to_say",
    work_email: "avery.work@example.invalid",
    personal_email: "avery.personal@example.invalid",
    mobile_phone: "+61400000001",
    work_phone: "+61400000002",
    blood_group: "AB-",
    preferred_language: "English",
    engagement_type: "employee",
    department: "Emergency",
    designation: "Registered nurse",
    start_date: "2024-02-01",
  },
  addresses: [
    {
      id: "a7777777-7777-4777-8777-777777777772",
      use: "current",
      line1: "1 Synthetic Avenue",
      city: "Synthetic City",
      country: "AU",
    },
  ],
  emergency_contacts: [
    {
      id: "a7777777-7777-4777-8777-777777777773",
      name: "Morgan Contact Synthetic",
      relationship: "partner",
      phone: "+61400000003",
      priority: 1,
    },
  ],
  credentials: [
    {
      id: "a7777777-7777-4777-8777-777777777774",
      type: "Nursing registration",
      number: "SYNTHETIC-RN-101",
      issuer: "Synthetic Registration Council",
      country: "AU",
      valid_from: "2024-01-01",
      valid_until: "2028-12-31",
    },
  ],
  qualifications: [
    {
      id: "a7777777-7777-4777-8777-777777777775",
      qualification: "Synthetic Nursing Degree",
      institution: "Synthetic Training College",
      completion_year: 2020,
    },
  ],
  experiences: [
    {
      id: "a7777777-7777-4777-8777-777777777776",
      employer: "Synthetic Prior Clinic",
      role: "Nurse",
      start_date: "2020-02-01",
      end_date: "2024-01-31",
    },
  ],
};
const results = await Promise.all(Array.from({ length: 8 }, () => request(path, "PUT", input)));
for (const result of results) {
  assert.equal(result.status, 200);
  assert.deepEqual(result.body, results[0].body);
}
const saved = results[0].body;
assert.equal(saved.profile.personal_email, input.profile.personal_email);
assert.equal(saved.profile.blood_group, "AB-");
assert.equal(saved.profile.date_of_birth, "1995-03-15");
assert.equal(saved.credentials[0].valid_until, "2028-12-31");
assert.equal(saved.emergency_contacts[0].name, input.emergency_contacts[0].name);
assert.equal(saved.qualifications[0].completion_year, 2020);
assert.equal(saved.experiences[0].start_date, "2020-02-01");
const details = await request(path);
assert.deepEqual(details.body, saved);
const directory = await request(path.replace("/enrollment", ""));
assert.equal(directory.status, 200);
for (const forbidden of ["personal_email", "blood_group", "emergency_contacts", "SYNTHETIC-RN"])
  assert(!JSON.stringify(directory.body).includes(forbidden));
const invalid = await request(path, "PUT", {
  ...input,
  profile: { ...input.profile, date_of_birth: "2024-02-30" },
});
assert.equal(invalid.status, 400);
const foreignHospital = await request(
  path.replace(hospital, "f7777777-7777-4777-8777-777777777777"),
);
assert.equal(foreignHospital.status, 404);
const changed = {
  ...input,
  expected_version: saved.version,
  profile: { ...input.profile, department: "Synthetic Recovery" },
};
const updated = await request(path, "PUT", changed);
assert.equal(updated.status, 200);
assert.equal(updated.body.version, saved.version + 1);
const stale = await request(path, "PUT", { ...input, expected_version: saved.version });
assert.equal(stale.status, 409);
const restored = await request(path, "PUT", { ...input, expected_version: updated.body.version });
assert.equal(restored.status, 200);
assert.equal(restored.body.version, saved.version + 2);
const repeat = await request(path, "PUT", input);
assert.deepEqual(repeat.body, restored.body);
console.log(
  JSON.stringify({
    synthetic_tenant: tenant,
    staff_id: id,
    concurrent_retries: 8,
    unique_staff: 1,
    retry_version_unchanged: true,
    private_directory_fields_omitted: true,
    invalid_date: invalid.status,
    foreign_hospital: foreignHospital.status,
    stale_edit: stale.status,
    qualification_and_credentials_persisted: true,
  }),
);
