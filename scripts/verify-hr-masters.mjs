import assert from "node:assert/strict";

const base = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const tenant = process.env.HELIVANTA_VERIFY_TENANT;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
assert(
  base && cookie && tenant && hospital,
  "Provide the authenticated synthetic verification scope.",
);
const origin = new URL(base);
assert(
  origin.protocol === "https:" || ["localhost", "127.0.0.1"].includes(origin.hostname),
  "Use HTTPS or loopback.",
);
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
async function request(path, method = "GET", body) {
  const response = await fetch(new URL(path, origin), {
    method,
    headers: { Cookie: cookie, "Content-Type": "application/json" },
    ...(body ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(15000),
  });
  assert.equal(
    response.headers.get("cache-control"),
    "no-store",
    "Workforce responses must not be cached.",
  );
  return { status: response.status, body: await response.json() };
}
async function masters(kind) {
  const rows = [];
  let cursor;
  for (let page = 0; page < 100; page++) {
    const result = await request(
      `${prefix}/hr/masters?kind=${kind}&limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
    );
    assert.equal(result.status, 200);
    rows.push(...result.body.data);
    if (!result.body.page.has_more) return rows;
    cursor = result.body.page.next_cursor;
    assert(cursor);
  }
  throw new Error("Verification page limit exceeded.");
}
const id = "b8888888-8888-4888-8888-888888888881";
const path = `${prefix}/hr/masters/${id}`;
const before = (await masters("department")).find((row) => row.id === id);
const baseline = {
  expected_version: before?.version ?? 0,
  kind: "department",
  code: "SYN-VERIFY-HR-DEPT",
  name: "Synthetic HR Verification Department",
  description: "Synthetic acceptance fixture; no source employee data.",
  active: true,
};
const retries = await Promise.all(Array.from({ length: 8 }, () => request(path, "PUT", baseline)));
for (const result of retries) {
  assert.equal(result.status, 200);
  assert.deepEqual(result.body, retries[0].body);
}
const saved = retries[0].body;
assert.equal((await masters("department")).filter((row) => row.id === id).length, 1);
assert.deepEqual((await request(path, "PUT", baseline)).body, saved);
const duplicate = await request(
  `${prefix}/hr/masters/b8888888-8888-4888-8888-888888888882`,
  "PUT",
  { ...baseline, expected_version: 0, code: baseline.code.toLowerCase() },
);
assert.equal(duplicate.status, 409);
const changed = {
  ...baseline,
  expected_version: saved.version,
  name: "Synthetic HR Verification Department Revised",
};
const updated = await request(path, "PUT", changed);
assert.equal(updated.status, 200);
assert.equal(updated.body.version, saved.version + 1);
assert.equal(
  (await request(path, "PUT", { ...changed, name: "Synthetic Stale Edit" })).status,
  409,
);
const history = await request(`${path}/history`);
assert.equal(history.status, 200);
assert(history.body.data.some((row) => row.id === saved.revision_id && row.name === baseline.name));
assert(
  history.body.data.some((row) => row.id === updated.body.revision_id && row.name === changed.name),
);
assert.equal(
  (await request(`workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee/hr/masters`)).status,
  404,
);
assert.equal((await request(path, "PUT", { ...changed, kind: "religion" })).status, 400);

const staffID = "b8888888-8888-4888-8888-888888888883";
const staffPath = `${prefix}/staff/${staffID}/enrollment`;
const oldStaff = await request(staffPath);
assert([200, 404].includes(oldStaff.status));
const enrollment = {
  expected_version: oldStaff.status === 200 ? oldStaff.body.version : 0,
  staff: {
    employee_code: "SYN-VERIFY-HR-REF",
    display_name: "Synthetic HR Reference Staff",
    job_category: "administration",
    employment_status: "active",
  },
  profile: { department_revision_id: updated.body.revision_id, department: "Forged snapshot" },
  addresses: [],
  emergency_contacts: [],
  credentials: [],
  qualifications: [],
  experiences: [],
};
const enrolled = await request(staffPath, "PUT", enrollment);
assert.equal(enrolled.status, 200);
assert.equal(enrolled.body.profile.department, changed.name);
const retired = await request(path, "PUT", {
  ...changed,
  expected_version: updated.body.version,
  active: false,
});
assert.equal(retired.status, 200);
const preserved = await request(staffPath, "PUT", {
  ...enrollment,
  expected_version: enrolled.body.version,
  profile: { preferred_name: "Synthetic historical assignment" },
});
assert.equal(preserved.status, 200);
assert.equal(preserved.body.profile.department_revision_id, updated.body.revision_id);
assert.equal(preserved.body.profile.department, changed.name);
assert.equal(
  (
    await request(`${prefix}/staff/b8888888-8888-4888-8888-888888888884/enrollment`, "PUT", {
      ...enrollment,
      expected_version: 0,
      staff: { ...enrollment.staff, employee_code: "SYN-VERIFY-HR-INACTIVE" },
    })
  ).status,
  400,
);
const restored = await request(path, "PUT", {
  ...baseline,
  expected_version: retired.body.version,
});
assert.equal(restored.status, 200);
console.log(
  JSON.stringify({
    synthetic_tenant: tenant,
    concurrent_retries: 8,
    unique_master: true,
    unchanged_retry_preserved: true,
    normalized_duplicate: 409,
    stale_edit: 409,
    foreign_hospital: 404,
    immutable_history_readable: true,
    assignment_snapshot_derived: true,
    historical_assignment_preserved: true,
    retired_revision_new_assignment: 400,
    no_store: true,
  }),
);
