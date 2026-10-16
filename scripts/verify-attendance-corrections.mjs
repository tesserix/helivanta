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
const expect = async (path, method, body, status, as = cookie) => {
  const r = await request(path, method, body, as);
  assert.equal(r.status, status, `${method} ${path}: ${JSON.stringify(r.body)}`);
  return r.body;
};
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
for (const p of ["workforce.attendance.record", "workforce.attendance.correct"])
  assert(me.body.data.includes(p), `Synthetic filer must hold ${p}`);
if (approverCookie) {
  const approver = await request("iam/me/permissions", "GET", undefined, approverCookie);
  assert.equal(approver.status, 200);
  assert.equal(approver.body.tenant_id, tenant);
  assert(approver.body.data.includes("workforce.attendance.approve"), "Approver must hold approve");
}

// Reserved fixture; roster settings come from verify-roster-config and the policy from verify-attendance.
const staff = "a7777777-7777-4777-8777-777777777793";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const roster = await expect(`${prefix}/hr/roster-settings`, "GET", undefined, 200);
const settings = await expect(`${prefix}/hr/attendance-settings`, "GET", undefined, 200);
const person = await expect(
  `${prefix}/staff/${staff}`,
  "PUT",
  {
    employee_code: "SYN-ATTENDANCE-105",
    display_name: "Ira Correction Synthetic",
    job_category: "nurse",
    employment_status: "active",
  },
  200,
);
assert.match(person.employee_code, /^SYN-/);

const localDate = (at) =>
  new Intl.DateTimeFormat("en-CA", { timeZone: roster.timezone }).format(new Date(at));
const minute = (offset) =>
  new Date(Math.floor((Date.now() + offset * 60000) / 60000) * 60000).toISOString();
// Every time used sits between 150 and 58 minutes ago, which must fall on one local date.
const at = minute(-120);
const day = localDate(at);
assert(
  localDate(minute(-150)) === day && localDate(minute(-58)) === day,
  "Run outside the first three hours of the local day.",
);
const punches = `${prefix}/hr/attendance/punches`;
const corrections = `${prefix}/hr/attendance/corrections`;
const ledger = `${punches}?staff_id=${staff}&from=${day}&to=${day}`;

// A failed earlier run may leave pending requests or live punches that would be duplicates; clear them first.
for (const left of await list(`${corrections}?status=pending&staff_id=${staff}`))
  await expect(
    `${corrections}/${left.id}/cancellation`,
    "POST",
    { expected_version: left.version, note: "Synthetic cleanup" },
    200,
  );
for (const left of await list(ledger))
  if (!left.voided)
    await expect(`${punches}/${left.id}/void`, "POST", { reason: "Synthetic cleanup" }, 200);

const original = await expect(
  `${punches}/${randomUUID()}`,
  "PUT",
  { staff_id: staff, direction: "in", punched_at: at, reason: "Synthetic late punch" },
  200,
);

// Filing: shape, replay, reuse and the one-open-request-per-punch rule.
const retime = {
  staff_id: staff,
  kind: "replace",
  target_punch_id: original.id,
  direction: "in",
  punched_at: minute(-150),
  reason: "Synthetic clock running fast",
};
const retimeID = randomUUID();
const filed = await expect(`${corrections}/${retimeID}`, "PUT", retime, 200);
assert.equal(filed.status, "pending");
assert.equal(filed.version, 1);
assert.equal(filed.target_punched_at, original.punched_at, "before values are returned");
assert.equal(filed.punched_at, retime.punched_at);
assert.equal(filed.applied_punch_id, undefined);
const replay = await expect(`${corrections}/${retimeID}`, "PUT", retime, 200);
assert.equal(replay.created_at, filed.created_at, "replay returns the stored request");
assert.equal(
  (await expect(`${corrections}/${retimeID}`, "PUT", { ...retime, reason: "Synthetic x" }, 409))
    .error,
  "request_reused",
);
assert.equal(
  (
    await expect(
      `${corrections}/${randomUUID()}`,
      "PUT",
      { staff_id: staff, kind: "void", target_punch_id: original.id, reason: "Synthetic second" },
      409,
    )
  ).error,
  "correction_exists",
);
await expect(`${corrections}/${randomUUID()}`, "PUT", { ...retime, kind: "add" }, 400);
await expect(`${corrections}/${randomUUID()}`, "PUT", { ...retime, kind: "void" }, 400);
await expect(`${corrections}/${randomUUID()}`, "PUT", { ...retime, reason: " " }, 400);
await expect(
  `${corrections}/${randomUUID()}`,
  "PUT",
  { ...retime, punched_at: new Date(Date.parse(retime.punched_at) + 30000).toISOString() },
  400,
);
const old = new Date(Date.now() - (settings.backdate_days + 2) * 86400000);
old.setUTCSeconds(0, 0);
assert.equal(
  (
    await expect(
      `${corrections}/${randomUUID()}`,
      "PUT",
      {
        staff_id: staff,
        kind: "add",
        direction: "out",
        punched_at: old.toISOString(),
        reason: "Synthetic too old",
      },
      409,
    )
  ).error,
  "outside_backdate_window",
);
const add = {
  staff_id: staff,
  kind: "add",
  direction: "out",
  punched_at: minute(-60),
  reason: "Synthetic missed out punch",
};
const addID = randomUUID();
await expect(`${corrections}/${addID}`, "PUT", add, 200);
assert.equal(
  (await expect(`${corrections}/${randomUUID()}`, "PUT", { ...add, punched_at: minute(-58) }, 409))
    .reason,
  "duplicate_correction",
);
const pending = await list(`${corrections}?status=pending&staff_id=${staff}&from=${day}&to=${day}`);
assert.deepEqual(pending.map((c) => c.id).sort(), [retimeID, addID].sort());
const untouched = (await list(ledger)).filter((p) => !p.voided);
assert.deepEqual(
  untouched.map((p) => p.id),
  [original.id],
  "pending requests change nothing",
);

let approval = "not_run_without_approver_session";
if (approverCookie) {
  const decide = (id, version, decision, note, status) =>
    expect(
      `${corrections}/${id}/decision`,
      "POST",
      { expected_version: version, decision, note },
      status,
      approverCookie,
    );
  await decide(addID, 1, "reject", "", 400);
  assert.equal((await decide(addID, 1, "reject", "Synthetic rejected", 200)).status, "rejected");
  assert.equal((await decide(addID, 1, "approve", "", 409)).error, "request_state");
  assert.equal((await decide(retimeID, 2, "approve", "", 409)).error, "stale_version");
  const approved = await decide(retimeID, 1, "approve", "Synthetic checked", 200);
  assert.equal(approved.status, "approved");
  assert(approved.applied_punch_id);
  await decide(retimeID, 1, "approve", "Synthetic checked", 200);
  const after = await list(ledger);
  const source = after.find((p) => p.id === original.id);
  assert.equal(source.voided, true);
  assert.equal(source.void_correction_id, retimeID);
  assert.equal(source.punched_at, original.punched_at, "the original punch is kept as recorded");
  const corrected = after.find((p) => p.id === approved.applied_punch_id);
  assert.equal(corrected.source, "correction");
  assert.equal(corrected.correction_id, retimeID);
  assert.equal(corrected.punched_at, retime.punched_at);
  assert.deepEqual(
    after.filter((p) => !p.voided).map((p) => p.id),
    [approved.applied_punch_id],
    "the rejected add wrote nothing",
  );
  const sheet = (await list(`${prefix}/hr/attendance?date=${day}`)).find(
    (d) => d.staff_id === staff,
  );
  assert(
    sheet.shifts.some((s) => s.punch_ids.includes(corrected.id)) ||
      sheet.stray_punch_ids.includes(corrected.id),
    "the corrected punch counts",
  );
  const history = await list(`${corrections}/${retimeID}/history`);
  assert.deepEqual(
    history.map((e) => e.status),
    ["approved", "pending"],
  );
  await expect(`${punches}/${corrected.id}/void`, "POST", { reason: "Synthetic cleanup" }, 200);
  approval = "approved_and_applied";
} else {
  for (const id of [retimeID, addID])
    assert.equal(
      (
        await expect(
          `${corrections}/${id}/cancellation`,
          "POST",
          { expected_version: 1, note: "Synthetic cancelled" },
          200,
        )
      ).status,
      "cancelled",
    );
  await expect(`${punches}/${original.id}/void`, "POST", { reason: "Synthetic cleanup" }, 200);
}

const foreign = "workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
await expect(`${foreign}/hr/attendance/corrections`, "GET", undefined, 404);
await expect(`${foreign}/hr/attendance/corrections/${retimeID}`, "GET", undefined, 404);

console.log(
  JSON.stringify({
    policy_revision: settings.revision_id,
    timezone: roster.timezone,
    local_date: day,
    replay_same_request: true,
    id_reuse: "request_reused",
    one_open_request_per_punch: "correction_exists",
    duplicate: "duplicate_correction",
    shape: 400,
    backdate: "outside_backdate_window",
    pending_changes_nothing: true,
    approval,
    foreign_hospital: 404,
    no_store: true,
  }),
);
