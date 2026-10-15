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
const staffID = process.env.HELIVANTA_VERIFY_STAFF;
assert(
  base && cookie && hospital && staffID,
  "Provide an authenticated synthetic verification scope.",
);
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
const expect = async (path, method, body, status, as) => {
  const r = await request(path, method, body, as);
  assert.equal(r.status, status, `${method} ${path}: ${JSON.stringify(r.body)}`);
  return r.body;
};
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
for (const p of ["workforce.leave.configure", "workforce.leave.request"])
  assert(me.body.data.includes(p), `Synthetic owner must hold ${p}`);
if (approverCookie) {
  const approver = await request("iam/me/permissions", "GET", undefined, approverCookie);
  assert.equal(approver.status, 200);
  assert.equal(approver.body.tenant_id, tenant);
  assert(approver.body.data.includes("workforce.leave.approve"), "Approver must hold approve");
}

const scope = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const staff = await expect(`${scope}/staff/${encodeURIComponent(staffID)}`, "GET", undefined, 200);
assert.match(staff.employee_code, /^SYN-/i, "Verification staff must carry a synthetic code");

// verify-leave-setup.mjs and verify-leave-ledger.mjs own the type, structure, year and credits.
const earnedID = "f1111111-1111-4111-8111-111111111111";
const today = new Date(Date.now() - 12 * 3600_000).toISOString().slice(0, 10);
const year = (await list(`${scope}/hr/leave-years`)).find(
  (y) => !y.closed && y.start_on <= today && today <= y.end_on,
);
assert(year, "Run scripts/verify-leave-ledger.mjs before this script.");

// Each calendar year the leave year touches needs one active calendar; reserved ids fill gaps.
const configs = `${scope}/hr/leave-configs`;
const calendars = await list(`${configs}?kind=holiday_calendar`);
const holidays = new Set();
for (const [i, y] of [Number(year.start_on.slice(0, 4)), Number(year.end_on.slice(0, 4))]
  .filter((y, i, all) => all.indexOf(y) === i)
  .entries()) {
  let calendar = calendars.find((c) => c.active && c.calendar_year === y);
  if (!calendar) {
    const id = `f1111111-1111-4111-8111-11111111113${i + 1}`;
    calendar = await expect(
      `${configs}/${id}`,
      "PUT",
      {
        expected_version: calendars.find((c) => c.id === id)?.version ?? 0,
        kind: "holiday_calendar",
        code: `SYN-VERIFY-${y}`,
        name: `Synthetic Verify ${y}`,
        description: "Synthetic verification fixture",
        active: true,
        reviewed: true,
        effective_on: today,
        calendar_year: y,
        holidays: [{ holiday_date: `${y}-12-25`, name: "Synthetic Year End", category: "regular" }],
        entries: [],
      },
      200,
    );
  }
  for (const h of calendar.holidays) if (h.category !== "optional") holidays.add(h.holiday_date);
}

const staffPath = `${scope}/staff/${staffID}/leave-requests`;
const live = (await list(`${scope}/hr/leave-requests?staff_id=${staffID}`)).filter(
  (r) => r.status === "pending" || r.status === "approved",
);
// Withdraw pending requests an interrupted earlier run left behind.
for (const r of live.filter((r) => r.status === "pending" && r.reason.startsWith("Synthetic")))
  await expect(
    `${staffPath}/${r.id}/cancellation`,
    "POST",
    { expected_version: r.version, note: "" },
    200,
  );
const kept = live.filter((r) => r.status === "approved" || !r.reason.startsWith("Synthetic"));
const busy = (day) => kept.some((r) => r.start_on <= day && day <= r.end_on);
const shift = (day, n) => {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
};
// A working day with free neighbours, so clubbing with other live leave cannot interfere.
let day = shift(today, 1);
while (day <= year.end_on && (holidays.has(day) || [-1, 0, 1].some((n) => busy(shift(day, n)))))
  day = shift(day, 1);
assert(day <= year.end_on, "No free working day remains in the current leave year.");
const holiday = [...holidays].find((h) => h >= year.start_on && h <= year.end_on);
assert(holiday, "The leave year carries at least one counted holiday.");

const balance = async () =>
  (
    await expect(
      `${scope}/staff/${staffID}/leave-balances?leave_year=${year.leave_year}`,
      "GET",
      undefined,
      200,
    )
  ).balances.find((b) => b.leave_type_id === earnedID)?.half_days ?? 0;
const before = await balance();
assert(before >= 2, "The synthetic earned balance covers one day; post a month first.");

const body = {
  leave_type_id: earnedID,
  unit: "full_day",
  start_on: day,
  end_on: day,
  reason: "Synthetic verification request",
};
const id = randomUUID();
const filed = await expect(`${staffPath}/${id}`, "PUT", body, 200);
assert.equal(filed.request.status, "pending");
assert.equal(filed.request.half_days, 2);
assert.deepEqual(
  filed.days.map((d) => [d.day, d.half_days]),
  [[day, 2]],
);
const replayed = await expect(`${staffPath}/${id}`, "PUT", body, 200);
assert.equal(replayed.request.version, filed.request.version, "replay returns the stored request");
await expect(`${staffPath}/${id}`, "PUT", { ...body, reason: "Synthetic changed" }, 409);
await expect(
  `${staffPath}/${randomUUID()}`,
  "PUT",
  { ...body, unit: "half_day", session: "first_half" },
  409,
);
await expect(
  `${staffPath}/${randomUUID()}`,
  "PUT",
  { ...body, start_on: holiday, end_on: holiday },
  400,
);
await expect(
  `${staffPath}/${id}/decision`,
  "POST",
  { expected_version: 1, decision: "approve", note: "" },
  403,
);
const listed = await list(`${scope}/hr/leave-requests?staff_id=${staffID}&status=pending`);
assert(listed.some((r) => r.id === id && r.reason === body.reason));
const withdrawn = await expect(
  `${staffPath}/${id}/cancellation`,
  "POST",
  { expected_version: 1, note: "" },
  200,
);
assert.equal(withdrawn.request.status, "cancelled");
await expect(`${staffPath}/${id}/cancellation`, "POST", { expected_version: 1, note: "" }, 200);
assert.equal(await balance(), before, "a withdrawn request never touches the ledger");

let approval = "not_run_without_approver_session";
if (approverCookie) {
  const second = randomUUID();
  await expect(`${staffPath}/${second}`, "PUT", body, 200);
  const decide = { expected_version: 1, decision: "approve", note: "" };
  await expect(
    `${staffPath}/${second}/revocation`,
    "POST",
    { expected_version: 1, note: "" },
    400,
    approverCookie,
  );
  await expect(
    `${staffPath}/${second}/revocation`,
    "POST",
    { expected_version: 1, note: "Synthetic early" },
    409,
    approverCookie,
  );
  const approved = await expect(
    `${staffPath}/${second}/decision`,
    "POST",
    decide,
    200,
    approverCookie,
  );
  assert.equal(approved.request.status, "approved");
  await expect(`${staffPath}/${second}/decision`, "POST", decide, 200, approverCookie);
  assert.equal(await balance(), before - 2, "approval consumes once");
  const ledger = await list(`${scope}/staff/${staffID}/leave-ledger?leave_year=${year.leave_year}`);
  assert.equal(ledger.filter((e) => e.request_id === second && e.kind === "consumption").length, 1);
  const revoke = { expected_version: approved.request.version, note: "Synthetic reversal" };
  await expect(`${staffPath}/${second}/revocation`, "POST", revoke, 200, approverCookie);
  await expect(`${staffPath}/${second}/revocation`, "POST", revoke, 200, approverCookie);
  assert.equal(await balance(), before, "cancelling approved leave reverses it once");
  approval = "approved_consumed_and_reversed_once";
}

const foreign = "workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
await expect(`${foreign}/hr/leave-requests`, "GET", undefined, 404);

console.log(
  JSON.stringify({
    leave_year: year.leave_year,
    day,
    filed_half_days: 2,
    replay_same_version: true,
    id_reuse: 409,
    overlapping_half_day: 409,
    all_holiday_request: 400,
    self_approval: 403,
    revoke_without_note: approverCookie ? 400 : "not_run",
    revoke_pending: approverCookie ? 409 : "not_run",
    withdraw_replay: true,
    withdrawn_ledger_untouched: true,
    approval,
    foreign_hospital: 404,
    no_store: true,
  }),
);
