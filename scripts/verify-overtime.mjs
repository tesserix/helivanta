import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
const tenant = process.env.HELIVANTA_VERIFY_TENANT;
assert(
  ["11111111-1111-1111-1111-111111111111", "8f8ed375-596b-4633-b90b-27fd3e6f299c"].includes(tenant),
  "Verification is restricted to designated synthetic tenants",
);
const base = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const approverCookie = process.env.HELIVANTA_VERIFY_APPROVER_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
assert(base && cookie && hospital, "Provide an authenticated synthetic verification scope.");
assert(approverCookie !== cookie, "The approver must be a different session from the filer.");
const origin = new URL(base);
assert(
  origin.protocol === "https:" || ["localhost", "127.0.0.1"].includes(origin.hostname),
  "Use HTTPS or loopback.",
);
async function request(path, method = "GET", body, as = cookie) {
  const response = await fetch(new URL(path, origin), {
    method,
    headers: { Cookie: as, "Content-Type": "application/json" },
    ...(body ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(30000),
  });
  assert.equal(response.headers.get("cache-control"), "no-store");
  return { status: response.status, body: await response.json() };
}
async function list(path, as = cookie) {
  const data = [];
  let cursor;
  for (let page = 0; page < 100; page++) {
    const response = await request(
      `${path}${path.includes("?") ? "&" : "?"}limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
      "GET",
      undefined,
      as,
    );
    assert.equal(response.status, 200, JSON.stringify(response.body));
    data.push(...response.body.data);
    if (!response.body.page.has_more) return data;
    cursor = response.body.page.next_cursor;
    assert(cursor);
  }
  throw new Error("Synthetic verification page limit exceeded.");
}
const expect = async (path, method, body, status, as) => {
  const r = await request(path, method, body, as);
  assert.equal(r.status, status, `${method} ${path}: ${JSON.stringify(r.body)}`);
  return r.body;
};
const addDays = (date, days) => {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
};
const clock = (minutes) =>
  `${String(Math.floor((minutes % 1440) / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
for (const p of ["workforce.overtime.request", "workforce.overtime.configure"])
  assert(me.body.data.includes(p), `Synthetic owner must hold ${p}`);
if (approverCookie) {
  const approver = await request("iam/me/permissions", "GET", undefined, approverCookie);
  assert.equal(approver.status, 200);
  assert.equal(approver.body.tenant_id, tenant);
  assert(approver.body.data.includes("workforce.overtime.approve"), "Approver must hold approve");
}

// Reserved fixture; roster settings come from verify-roster-config.
const staff = "a7777777-7777-4777-8777-777777777791";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
await expect(`${prefix}/hr/roster-settings`, "GET", undefined, 200);
const person = await expect(
  `${prefix}/staff/${staff}`,
  "PUT",
  {
    employee_code: "SYN-OVERTIME-103",
    display_name: "Avery Overtime Synthetic",
    job_category: "nurse",
    employment_status: "active",
  },
  200,
);
assert.match(person.employee_code, /^SYN-/);

const today = new Date().toISOString().slice(0, 10);
const settingsPath = `${prefix}/hr/overtime-settings`;
let policy = await request(settingsPath);
if (policy.status === 404) {
  const configure = {
    expected_version: 0,
    daily_limit_minutes: 240,
    weekly_limit_minutes: 720,
    monthly_limit_minutes: 2400,
    filing_window_days: 7,
    effective_on: today,
    reviewed: true,
  };
  await expect(settingsPath, "PUT", { ...configure, weekly_limit_minutes: 200 }, 400);
  await expect(settingsPath, "PUT", { ...configure, reviewed: false }, 400);
  policy = { status: 200, body: await expect(settingsPath, "PUT", configure, 200) };
  const replay = await expect(settingsPath, "PUT", configure, 200);
  assert.equal(replay.version, policy.body.version, "an identical policy returns the revision");
}
assert.equal(policy.status, 200, JSON.stringify(policy.body));
const settings = policy.body;
assert((await list(`${settingsPath}/history`)).some((r) => r.revision_id === settings.revision_id));
const workDate = [addDays(today, 14), settings.effective_on].sort().at(-1);
assert(workDate <= addDays(today, 92), "The current policy takes effect too far ahead to verify.");

const overtime = `${prefix}/hr/overtime`;
// A failed earlier run may leave pending overtime that would overlap; withdraw it first.
for (const left of await list(`${overtime}?status=pending&staff_id=${staff}`))
  await expect(
    `${overtime}/${left.id}/cancellation`,
    "POST",
    { expected_version: left.version, note: "Synthetic cleanup" },
    200,
  );
assert.equal(
  (await list(`${overtime}?status=approved&staff_id=${staff}`)).length,
  0,
  "Revoke approved synthetic overtime from an earlier run with an approver session first.",
);

const body = (start, minutes, extra = {}) => ({
  staff_id: staff,
  work_date: workDate,
  start_time: clock(start),
  end_time: clock(start + minutes),
  reason: "Synthetic verification overtime",
  ...extra,
});
const first = body(19 * 60, 60);
const id = randomUUID();
const filed = await expect(`${overtime}/${id}`, "PUT", first, 200);
assert.equal(filed.status, "pending");
assert.equal(filed.requested_minutes, 60);
assert.equal(filed.payable_minutes, 0, "pending overtime is never payable");
assert.equal(filed.reason, first.reason);
const replayed = await expect(`${overtime}/${id}`, "PUT", first, 200);
assert.equal(replayed.version, filed.version, "replay returns the stored request");
assert.equal(
  (await expect(`${overtime}/${id}`, "PUT", { ...first, reason: "Synthetic changed" }, 409)).error,
  "request_reused",
);
assert.equal(
  (await expect(`${overtime}/${randomUUID()}`, "PUT", body(19 * 60 + 30, 60), 409)).error,
  "overtime_overlap",
);
const over = await expect(
  `${overtime}/${randomUUID()}`,
  "PUT",
  body(20 * 60, settings.daily_limit_minutes - 60 + 15),
  409,
);
assert.equal(over.reason, "limit_exceeded", "pending minutes count towards the limit");
assert.equal(
  (
    await expect(
      `${overtime}/${randomUUID()}`,
      "PUT",
      body(19 * 60, 60, { work_date: addDays(today, 93) }),
      409,
    )
  ).error,
  "outside_filing_window",
);
await expect(`${overtime}/${randomUUID()}`, "PUT", body(19 * 60, 10), 400);
await expect(
  `${overtime}/${id}/decision`,
  "POST",
  { expected_version: 1, decision: "approve", approved_minutes: 60, note: "" },
  403,
);
const pending = await list(
  `${overtime}?status=pending&staff_id=${staff}&from=${workDate}&to=${workDate}`,
);
assert.deepEqual(
  pending.map((r) => r.id),
  [id],
);
const history = await list(`${overtime}/${id}/history`);
assert.deepEqual(
  history.map((e) => e.status),
  ["pending"],
);
const month = workDate.slice(0, 7);
const counted = (await list(`${prefix}/hr/overtime-summary?month=${month}`)).find(
  (r) => r.staff_id === staff,
);
assert.equal(counted?.pending_minutes, 60);
assert.equal(counted?.payable_minutes, 0);
const cancel = { expected_version: 1, note: "" };
assert.equal(
  (await expect(`${overtime}/${id}/cancellation`, "POST", cancel, 200)).status,
  "cancelled",
);
await expect(`${overtime}/${id}/cancellation`, "POST", cancel, 200);

let approval = "not_run_without_approver_session";
if (approverCookie) {
  const second = randomUUID();
  const ask = body(19 * 60, 90);
  await expect(`${overtime}/${second}`, "PUT", ask, 200);
  await expect(
    `${overtime}/${second}/revocation`,
    "POST",
    { expected_version: 1, note: "Synthetic early" },
    409,
    approverCookie,
  );
  await expect(
    `${overtime}/${second}/decision`,
    "POST",
    { expected_version: 1, decision: "approve", approved_minutes: 60, note: "" },
    400,
    approverCookie,
  );
  const decide = {
    expected_version: 1,
    decision: "approve",
    approved_minutes: 60,
    note: "Synthetic partial approval",
  };
  const approved = await expect(
    `${overtime}/${second}/decision`,
    "POST",
    decide,
    200,
    approverCookie,
  );
  assert.equal(approved.status, "approved");
  assert.equal(approved.approved_minutes, 60);
  assert.equal(approved.payable_minutes, 60, "approved minutes, not requested, are payable");
  assert.equal(approved.policy_revision_id, settings.revision_id);
  await expect(`${overtime}/${second}/decision`, "POST", decide, 200, approverCookie);
  const paid = (await list(`${prefix}/hr/overtime-summary?month=${month}`)).find(
    (r) => r.staff_id === staff,
  );
  assert.equal(paid.payable_minutes, 60);
  assert.equal(paid.approved_requested_minutes, 90);
  const revoke = { expected_version: approved.version, note: "" };
  await expect(`${overtime}/${second}/revocation`, "POST", revoke, 400, approverCookie);
  revoke.note = "Synthetic reversal";
  const revoked = await expect(
    `${overtime}/${second}/revocation`,
    "POST",
    revoke,
    200,
    approverCookie,
  );
  assert.equal(revoked.status, "revoked");
  assert.equal(revoked.payable_minutes, 0, "revoked overtime is never payable");
  await expect(`${overtime}/${second}/revocation`, "POST", revoke, 200, approverCookie);
  const after = (await list(`${prefix}/hr/overtime-summary?month=${month}`)).find(
    (r) => r.staff_id === staff,
  );
  assert.equal(after.payable_minutes, 0);
  approval = "partial_approval_paid_then_revoked";
}

const foreign = "workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
await expect(`${foreign}/hr/overtime`, "GET", undefined, 404);
await expect(`${foreign}/hr/overtime/${id}`, "GET", undefined, 404);
await expect(`${foreign}/hr/overtime-settings`, "GET", undefined, 404);

console.log(
  JSON.stringify({
    policy_revision: settings.revision_id,
    work_date: workDate,
    replay_same_version: true,
    id_reuse: "request_reused",
    overlap: "overtime_overlap",
    pending_counts_to_limit: over.period,
    filing_window: "outside_filing_window",
    short_span: 400,
    self_decision: 403,
    summary_pending: true,
    withdraw_replay: true,
    revoke_pending: approverCookie ? 409 : "not_run",
    partial_without_note: approverCookie ? 400 : "not_run",
    approval,
    foreign_hospital: 404,
    no_store: true,
  }),
);
