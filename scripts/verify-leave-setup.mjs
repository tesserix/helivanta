import assert from "node:assert/strict";
const tenant = process.env.HELIVANTA_VERIFY_TENANT;
assert(
  ["11111111-1111-1111-1111-111111111111", "8f8ed375-596b-4633-b90b-27fd3e6f299c"].includes(tenant),
  "Verification is restricted to designated synthetic tenants",
);
const base = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
assert(base && cookie && hospital, "Provide an authenticated synthetic verification scope.");
const origin = new URL(base);
assert(
  origin.protocol === "https:" || ["localhost", "127.0.0.1"].includes(origin.hostname),
  "Use HTTPS or loopback.",
);
async function request(path, method = "GET", body) {
  const response = await fetch(new URL(path, origin), {
    method,
    headers: { Cookie: cookie, "Content-Type": "application/json" },
    ...(body ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(15000),
  });
  assert.equal(response.headers.get("cache-control"), "no-store");
  return { status: response.status, body: await response.json() };
}
async function list(path) {
  const data = [];
  let cursor;
  for (let page = 0; page < 100; page++) {
    const response = await request(
      `${path}${path.includes("?") ? "&" : "?"}limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
    );
    assert.equal(response.status, 200);
    data.push(...response.body.data);
    if (!response.body.page.has_more) return data;
    cursor = response.body.page.next_cursor;
    assert(cursor);
  }
  throw new Error("Synthetic verification page limit exceeded.");
}
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
assert(me.body.data.includes("workforce.leave.configure"), "Synthetic owner must configure leave");

// Reserved synthetic fixtures; reruns replay the same bodies and create no new revisions.
const earnedID = "f1111111-1111-4111-8111-111111111111";
const unpaidID = "f1111111-1111-4111-8111-111111111112";
const calendarID = "f1111111-1111-4111-8111-111111111113";
const structureID = "f1111111-1111-4111-8111-111111111114";
const effective = "2026-10-15";
const path = `workforce/hospitals/${encodeURIComponent(hospital)}/hr/leave-configs`;
const existing = new Map((await list(path)).map((r) => [r.id, r]));
async function ensure(id, body) {
  const saved = await request(`${path}/${id}`, "PUT", {
    ...body,
    expected_version: existing.get(id)?.version ?? 0,
  });
  assert.equal(saved.status, 200, JSON.stringify(saved.body));
  return saved.body;
}
const common = { description: "Synthetic verification fixture", active: true, reviewed: true };
const rules = { paid: true, half_day_allowed: true, short_leave_minutes: 120, clubbable: false };
const leaveType = (code, name, tracked) => ({
  ...common,
  kind: "leave_type",
  code,
  name,
  effective_on: effective,
  rules: { ...rules, balance_tracked: tracked },
  holidays: [],
  entries: [],
});
const earnedBody = leaveType("SYN-VERIFY-EARNED", "Synthetic Verify Earned", true);
const earned = await ensure(earnedID, earnedBody);
const unpaid = await ensure(
  unpaidID,
  leaveType("SYN-VERIFY-UNPAID", "Synthetic Verify Unpaid", false),
);
const calendarBody = {
  ...common,
  kind: "holiday_calendar",
  code: "SYN-VERIFY-2199",
  name: "Synthetic Verify 2199",
  effective_on: effective,
  calendar_year: 2199,
  holidays: [
    { holiday_date: "2199-08-15", name: "Synthetic Midyear", category: "government" },
    { holiday_date: "2199-01-26", name: "Synthetic New Year", category: "regular" },
  ],
  entries: [],
};
const calendar = await ensure(calendarID, calendarBody);
assert.deepEqual(
  calendar.holidays.map((h) => h.holiday_date),
  ["2199-01-26", "2199-08-15"],
);
const entry = (revision, half, carry) => ({
  leave_type_revision_id: revision,
  annual_half_days: half,
  accrual: "monthly",
  carry_forward_half_days: carry,
  proration: "join_month",
  probation_eligible: false,
});
const structureBody = {
  ...common,
  kind: "structure",
  code: "SYN-VERIFY-STRUCTURE",
  name: "Synthetic Verify Structure",
  effective_on: effective,
  holidays: [],
  entries: [entry(earned.revision_id, 24, 10), entry(unpaid.revision_id, 0, 0)],
};
const structure = await ensure(structureID, structureBody);
assert.deepEqual(
  structure.entries.map((e) => [e.code, e.balance_tracked, e.annual_half_days]),
  [
    ["SYN-VERIFY-EARNED", true, 24],
    ["SYN-VERIFY-UNPAID", false, 0],
  ],
);

const replay = { ...structureBody, expected_version: structure.version };
const retries = await Promise.all(
  Array.from({ length: 4 }, () => request(`${path}/${structureID}`, "PUT", replay)),
);
for (const r of retries) {
  assert.equal(r.status, 200);
  assert.equal(r.body.revision_id, structure.revision_id);
}
const rejected = async (id, body, status, error) => {
  const r = await request(`${path}/${id}`, "PUT", body);
  assert.equal(r.status, status, JSON.stringify(r.body));
  if (error) assert.equal(r.body.error, error);
};
const earnedReplay = { ...earnedBody, expected_version: earned.version };
await rejected(
  earnedID,
  { ...earnedReplay, rules: { ...earnedBody.rules, short_leave_minutes: 20 } },
  400,
);
await rejected(earnedID, { ...earnedReplay, name: "Synthetic Unreviewed", reviewed: false }, 400);
await rejected(
  earnedID,
  { ...earnedReplay, name: "Synthetic Stale", expected_version: earned.version + 1 },
  409,
);
await rejected(earnedID, { ...earnedReplay, code: "SYN-VERIFY-RENAMED" }, 409);
await rejected(earnedID, { ...earnedReplay, effective_on: "2026-01-01" }, 400);
await rejected(
  calendarID,
  {
    ...calendarBody,
    expected_version: calendar.version,
    holidays: [{ holiday_date: "2198-12-31", name: "Synthetic Outside", category: "regular" }],
  },
  400,
);
await rejected(structureID, { ...replay, entries: [entry(unpaid.revision_id, 2, 0)] }, 400);
await rejected(
  structureID,
  { ...replay, entries: [entry("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", 2, 0)] },
  400,
  "invalid_reference",
);
await rejected(
  structureID,
  { ...replay, entries: [entry(calendar.revision_id, 2, 0)] },
  400,
  "invalid_reference",
);
const history = await list(`${path}/${structureID}/history`);
assert(history.some((r) => r.revision_id === structure.revision_id && r.config_id === structureID));
const types = await list(`${path}?kind=leave_type`);
assert(types.every((r) => r.kind === "leave_type"));
assert.equal((await request(`${path}?kind=payroll`)).status, 400);
assert.equal(
  (await request("workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee/hr/leave-configs"))
    .status,
  404,
);
console.log(
  JSON.stringify({
    leave_type_versions: [earned.version, unpaid.version],
    calendar_version: calendar.version,
    structure_version: structure.version,
    concurrent_structure_retries: 4,
    invalid_short_leave: 400,
    unreviewed_change: 400,
    stale_edit: 409,
    identity_change: 409,
    backdated_effective_date: 400,
    holiday_outside_year: 400,
    untracked_allowance: 400,
    unknown_type_revision: "invalid_reference",
    non_type_reference: "invalid_reference",
    foreign_hospital: 404,
    no_store: true,
  }),
);
