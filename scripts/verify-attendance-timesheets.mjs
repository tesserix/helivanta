import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
const tenant = process.env.HELIVANTA_VERIFY_TENANT;
assert(
  ["11111111-1111-1111-1111-111111111111", "8f8ed375-596b-4633-b90b-27fd3e6f299c"].includes(tenant),
  "Verification is restricted to designated synthetic tenants",
);
const base = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
// A past day reserved for this check; periods are never deleted, so every run reuses it.
const day = process.env.HELIVANTA_VERIFY_PERIOD_DAY;
assert(base && cookie && hospital, "Provide an authenticated synthetic verification scope.");
assert(/^\d{4}-\d{2}-\d{2}$/.test(day ?? ""), "Provide HELIVANTA_VERIFY_PERIOD_DAY as YYYY-MM-DD.");
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
    signal: AbortSignal.timeout(60000),
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
const expect = async (path, method, body, status) => {
  const r = await request(path, method, body);
  assert.equal(r.status, status, `${method} ${path}: ${JSON.stringify(r.body)}`);
  return r.body;
};
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
for (const p of ["workforce.attendance.record", "workforce.attendance.finalise"])
  assert(me.body.data.includes(p), `Synthetic verifier must hold ${p}`);

// Reserved fixtures: the staff member and the period id are fixed so reruns replay.
const staff = "a7777777-7777-4777-8777-777777777794";
const periodID = "a7777777-7777-4777-8777-777777777795";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const roster = await expect(`${prefix}/hr/roster-settings`, "GET", undefined, 200);
const settings = await expect(`${prefix}/hr/attendance-settings`, "GET", undefined, 200);
const today = new Intl.DateTimeFormat("en-CA", { timeZone: roster.timezone }).format(new Date());
const age = (Date.parse(`${today}T00:00:00Z`) - Date.parse(`${day}T00:00:00Z`)) / 86400000;
assert(
  age >= 1 && age <= settings.backdate_days,
  `Reserve an ended day within the ${settings.backdate_days}-day back-dating window.`,
);
const person = await expect(
  `${prefix}/staff/${staff}`,
  "PUT",
  {
    employee_code: "SYN-ATTENDANCE-106",
    display_name: "Tara Timesheet Synthetic",
    job_category: "nurse",
    employment_status: "active",
  },
  200,
);
assert.match(person.employee_code, /^SYN-/);
const periodPath = `${prefix}/hr/attendance/periods/${periodID}`;
const punches = `${prefix}/hr/attendance/punches`;
const step = (name, version, note) =>
  expect(`${periodPath}/${name}`, "POST", { expected_version: version, note }, 200);

// First run creates the period; later runs replay it, and a run that stopped while reopened finalises it.
let period = await expect(
  periodPath,
  "PUT",
  { from: day, to: day, note: "Synthetic verification" },
  200,
);
if (period.status === "reopened")
  period = await step("finalisation", period.version, "Synthetic resume");
assert.equal(period.status, "finalised");
assert.equal(
  (await expect(periodPath, "PUT", { from: day, to: day, note: "Synthetic other" }, 409)).error,
  "request_reused",
);
const overlap = await expect(
  `${prefix}/hr/attendance/periods/${randomUUID()}`,
  "PUT",
  { from: day, to: day, note: "" },
  409,
);
assert.equal(overlap.reason, "period_overlap");
assert.equal(overlap.period_id, periodID);

// Noon on the reserved day, read in the hospital timezone.
const noon = new Date(`${day}T12:00:00Z`);
const offset =
  new Date(noon.toLocaleString("en-US", { timeZone: roster.timezone })).getTime() -
  new Date(noon.toLocaleString("en-US", { timeZone: "UTC" })).getTime();
const at = new Date(noon.getTime() - offset).toISOString().replace(".000Z", "Z");
const punch = {
  staff_id: staff,
  direction: "in",
  punched_at: at,
  reason: "Synthetic timesheet check",
};
assert.equal(
  (await expect(`${punches}/${randomUUID()}`, "PUT", punch, 409)).error,
  "period_locked",
);

// Reopen, add and void a punch, and finalise again: the new version reconciles and the day is locked again.
period = await step("reopening", period.version, "Synthetic reopen");
assert.equal(period.status, "reopened");
const recorded = await expect(`${punches}/${randomUUID()}`, "PUT", punch, 200);
await expect(
  `${punches}/${recorded.id}/void`,
  "POST",
  { reason: "Synthetic timesheet cleanup" },
  200,
);
const before = period.timesheet.number;
period = await step("finalisation", period.version, "Synthetic refinalise");
assert.equal(period.status, "finalised");
assert.equal(period.timesheet.number, before + 1);
assert.equal(
  (await expect(`${punches}/${randomUUID()}`, "PUT", punch, 409)).error,
  "period_locked",
);
const sheet = `${periodPath}/timesheets/${period.timesheet.id}`;
const people = await list(`${sheet}/people`);
const days = await list(`${sheet}/days`);
assert.equal(people.length, period.timesheet.people);
assert.equal(
  people.reduce((n, p) => n + p.payable_minutes, 0),
  period.timesheet.payable_minutes,
  "people reconcile to the timesheet",
);
for (const p of people) {
  const own = days.filter((d) => d.staff_id === p.staff_id);
  assert.equal(
    own.reduce((n, d) => n + d.payable_minutes, 0),
    p.payable_minutes,
    "days reconcile to people",
  );
}
assert(days.every((d) => d.date === day));
const history = await list(`${periodPath}/history`);
assert.equal(history[0].version, period.version);
assert.equal(history[0].note, "Synthetic refinalise");

// The punch report lists the voided punch with its actors and never a reason.
const report = await list(
  `${prefix}/hr/attendance/punch-report?from=${day}&to=${day}&staff_id=${staff}`,
);
const row = report.find((r) => r.id === recorded.id);
assert(row, "the report lists the punch");
assert.equal(row.voided, true);
assert.equal(row.job_category, "nurse");
assert(!("reason" in row) && !("void_reason" in row), "reasons stay out of the report");
console.log(
  `Timesheet verification passed: period ${periodID} version ${period.timesheet.number}.`,
);
