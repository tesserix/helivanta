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
    signal: AbortSignal.timeout(30000),
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
for (const p of ["workforce.attendance.record", "workforce.attendance.configure"])
  assert(me.body.data.includes(p), `Synthetic owner must hold ${p}`);

// Reserved fixture; roster settings come from verify-roster-config.
const staff = "a7777777-7777-4777-8777-777777777792";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const roster = await expect(`${prefix}/hr/roster-settings`, "GET", undefined, 200);
const person = await expect(
  `${prefix}/staff/${staff}`,
  "PUT",
  {
    employee_code: "SYN-ATTENDANCE-104",
    display_name: "Rowan Attendance Synthetic",
    job_category: "nurse",
    employment_status: "active",
  },
  200,
);
assert.match(person.employee_code, /^SYN-/);

const localDate = (at) =>
  new Intl.DateTimeFormat("en-CA", { timeZone: roster.timezone }).format(new Date(at));
const now = Date.now();
const today = localDate(now);
const settingsPath = `${prefix}/hr/attendance-settings`;
let policy = await request(settingsPath);
if (policy.status === 404) {
  const configure = {
    expected_version: 0,
    early_arrival_minutes: 60,
    late_departure_minutes: 120,
    early_departure_grace_minutes: 0,
    short_duty_minutes: 30,
    duplicate_window_minutes: 5,
    backdate_days: 7,
    effective_on: today,
    reviewed: true,
  };
  await expect(settingsPath, "PUT", { ...configure, duplicate_window_minutes: 0 }, 400);
  await expect(settingsPath, "PUT", { ...configure, reviewed: false }, 400);
  policy = { status: 200, body: await expect(settingsPath, "PUT", configure, 200) };
  const replay = await expect(settingsPath, "PUT", configure, 200);
  assert.equal(replay.version, policy.body.version, "an identical policy returns the revision");
}
assert.equal(policy.status, 200, JSON.stringify(policy.body));
const settings = policy.body;
assert(settings.effective_on <= today, "The current attendance policy is not yet in force.");
assert((await list(`${settingsPath}/history`)).some((r) => r.revision_id === settings.revision_id));

// Punches sit an hour ago, whole minutes, so they are inside the back-dating window and not ahead.
const at = new Date(Math.floor((now - 60 * 60000) / 60000) * 60000).toISOString();
const day = localDate(at);
const punches = `${prefix}/hr/attendance/punches`;
const ledger = `${punches}?staff_id=${staff}&from=${day}&to=${day}`;
// A failed earlier run may leave live punches that would be duplicates; void them first.
for (const left of await list(ledger))
  if (!left.voided)
    await expect(`${punches}/${left.id}/void`, "POST", { reason: "Synthetic cleanup" }, 200);

const punch = {
  staff_id: staff,
  direction: "in",
  punched_at: at,
  reason: "Synthetic verification punch",
};
const id = randomUUID();
const recorded = await expect(`${punches}/${id}`, "PUT", punch, 200);
assert.equal(recorded.local_date, day);
assert.equal(recorded.timezone, roster.timezone);
assert.equal(recorded.policy_revision_id, settings.revision_id);
assert.equal(recorded.source, "manual");
assert.equal(recorded.reason, punch.reason);
assert.equal(recorded.voided, false);
const replayed = await expect(`${punches}/${id}`, "PUT", punch, 200);
assert.equal(replayed.created_at, recorded.created_at, "replay returns the stored punch");
assert.equal(
  (await expect(`${punches}/${id}`, "PUT", { ...punch, reason: "Synthetic changed" }, 409)).error,
  "request_reused",
);
const duplicate = await expect(`${punches}/${randomUUID()}`, "PUT", punch, 409);
assert.equal(duplicate.reason, "duplicate_punch");
assert.equal(duplicate.punch_id, id);
await expect(`${punches}/${randomUUID()}`, "PUT", { ...punch, reason: " " }, 400);
await expect(
  `${punches}/${randomUUID()}`,
  "PUT",
  { ...punch, punched_at: new Date(Date.parse(at) + 30000).toISOString() },
  400,
);
await expect(
  `${punches}/${randomUUID()}`,
  "PUT",
  { ...punch, punched_at: new Date(now + 60 * 60000).toISOString() },
  400,
);
const old = new Date(now - (settings.backdate_days + 2) * 86400000);
old.setUTCSeconds(0, 0);
assert.equal(
  (
    await expect(
      `${punches}/${randomUUID()}`,
      "PUT",
      { ...punch, punched_at: old.toISOString() },
      409,
    )
  ).error,
  "outside_backdate_window",
);
await expect(`${punches}?staff_id=${staff}&from=2026-01-01&to=2026-03-01`, "GET", undefined, 400);
assert.deepEqual(
  (await list(ledger)).filter((p) => !p.voided).map((p) => p.id),
  [id],
);

const sheet = (await list(`${prefix}/hr/attendance?date=${day}`)).find((d) => d.staff_id === staff);
assert(sheet, "a person with a punch appears on the day sheet");
assert(
  sheet.shifts.some((s) => s.punch_ids.includes(id)) || sheet.stray_punch_ids.includes(id),
  "the punch is matched to a shift or reported as stray",
);
const totals = await expect(`${prefix}/hr/attendance-totals?date=${day}`, "GET", undefined, 200);
assert.equal(totals.date, day);
assert(totals.people >= 1);
const range = await expect(
  `${prefix}/staff/${staff}/attendance?from=${day}&to=${day}`,
  "GET",
  undefined,
  200,
);
assert.equal(range.days.length, 1);
assert.equal(range.days[0].date, day);

const voidPath = `${punches}/${id}/void`;
await expect(voidPath, "POST", { reason: "" }, 400);
const voided = await expect(voidPath, "POST", { reason: "Synthetic verification void" }, 200);
assert.equal(voided.voided, true);
assert.equal(voided.void_reason, "Synthetic verification void");
await expect(voidPath, "POST", { reason: "Synthetic verification void" }, 200);
assert.equal(
  (await expect(voidPath, "POST", { reason: "Synthetic other" }, 409)).error,
  "punch_voided",
);
const after = (await list(`${prefix}/hr/attendance?date=${day}`)).find((d) => d.staff_id === staff);
assert(
  !after ||
    (after.shifts.every((s) => !s.punch_ids.includes(id)) && !after.stray_punch_ids.includes(id)),
  "a voided punch no longer counts",
);
const again = await expect(`${punches}/${randomUUID()}`, "PUT", punch, 200);
assert.equal(again.voided, false, "a voided punch no longer blocks a corrected one");
await expect(`${punches}/${again.id}/void`, "POST", { reason: "Synthetic cleanup" }, 200);

const foreign = "workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
await expect(`${foreign}/hr/attendance?date=${day}`, "GET", undefined, 404);
await expect(`${foreign}/hr/attendance/punches/${id}`, "GET", undefined, 404);
await expect(`${foreign}/hr/attendance-settings`, "GET", undefined, 404);

console.log(
  JSON.stringify({
    policy_revision: settings.revision_id,
    timezone: roster.timezone,
    local_date: day,
    replay_same_punch: true,
    id_reuse: "request_reused",
    duplicate: "duplicate_punch",
    blank_reason: 400,
    sub_minute: 400,
    future: 400,
    backdate: "outside_backdate_window",
    range_limit: 400,
    sheet_matched: true,
    void_replay: true,
    void_conflict: "punch_voided",
    void_frees_direction: true,
    foreign_hospital: 404,
    no_store: true,
  }),
);
