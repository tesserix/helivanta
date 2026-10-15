import assert from "node:assert/strict";
const tenant = process.env.HELIVANTA_VERIFY_TENANT;
assert(
  ["11111111-1111-1111-1111-111111111111", "8f8ed375-596b-4633-b90b-27fd3e6f299c"].includes(tenant),
  "Verification is restricted to designated synthetic tenants",
);
const base = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
const staffID = process.env.HELIVANTA_VERIFY_STAFF;
assert(
  base && cookie && hospital && staffID,
  "Provide an authenticated synthetic verification scope.",
);
const origin = new URL(base);
assert(
  origin.protocol === "https:" || ["localhost", "127.0.0.1"].includes(origin.hostname),
  "Use HTTPS or loopback.",
);
async function request(path) {
  const response = await fetch(new URL(path, origin), {
    headers: { Cookie: cookie },
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
    assert.equal(response.status, 200, JSON.stringify(response.body));
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
const profiles = me.body.data.includes("workforce.enrollment.manage");

const scope = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const staff = await request(`${scope}/staff/${encodeURIComponent(staffID)}`);
assert.equal(staff.status, 200);
assert.match(staff.body.employee_code, /^SYN-/i, "Verification staff must carry a synthetic code");

// verify-leave-requests.mjs leaves approved and reversed synthetic requests in the current leave year.
const today = new Date(Date.now() - 12 * 3600_000).toISOString().slice(0, 10);
const year = (await list(`${scope}/hr/leave-years`)).find(
  (y) => !y.closed && y.start_on <= today && today <= y.end_on,
);
assert(year, "Run scripts/verify-leave-requests.mjs before this script.");
const period = `from=${year.start_on}&to=${year.end_on}`;
const requests = await list(`${scope}/hr/leave-reports/status?${period}&staff_id=${staffID}`);
const all = await list(
  `${scope}/hr/leave-requests?staff_id=${staffID}&leave_year=${year.leave_year}`,
);
assert.deepEqual(
  requests.map((r) => r.id).sort(),
  all.map((r) => r.id).sort(),
  "the status report lists every request of the year",
);
for (const r of requests) {
  assert(!("reason" in r), "reports never carry reasons");
  assert.equal("department" in r, profiles, "profile columns follow enrollment access");
  if (!["approved", "rejected"].includes(r.status)) assert(!("decided_by_subject" in r));
}

const monthly = await list(`${scope}/hr/leave-reports/monthly?${period}&staff_id=${staffID}`);
assert(monthly.length <= 1);
const reported = new Map();
for (const m of monthly[0]?.months ?? [])
  for (const t of m.types)
    reported.set(t.leave_type_id, (reported.get(t.leave_type_id) ?? 0) + t.half_days);
const ledger = await list(`${scope}/staff/${staffID}/leave-ledger?leave_year=${year.leave_year}`);
const consumed = new Map();
for (const e of ledger.filter((e) => e.kind === "consumption" || e.kind === "reversal"))
  consumed.set(e.leave_type_id, (consumed.get(e.leave_type_id) ?? 0) - e.half_days);
for (const [type, halfDays] of consumed)
  assert.equal(reported.get(type) ?? 0, halfDays, `report and ledger agree for ${type}`);

const expect = async (path, status) => {
  const r = await request(path);
  assert.equal(r.status, status, `${path}: ${JSON.stringify(r.body)}`);
};
await expect(`${scope}/hr/leave-reports/monthly?from=2026-10-02&to=2026-10-01`, 400);
await expect(`${scope}/hr/leave-reports/status?from=2026-01-01&to=2027-01-02`, 400);
await expect(`${scope}/hr/leave-reports/status?${period}&status=draft`, 400);
if (!profiles)
  await expect(
    `${scope}/hr/leave-reports/monthly?${period}&department_id=eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee`,
    403,
  );
const foreign = "workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
await expect(`${foreign}/hr/leave-reports/monthly?${period}`, 404);

console.log(
  JSON.stringify({
    leave_year: year.leave_year,
    status_rows: requests.length,
    monthly_months: monthly[0]?.months.length ?? 0,
    ledger_reconciled: true,
    profile_columns: profiles,
    invalid_period: 400,
    foreign_hospital: 404,
    no_store: true,
  }),
);
