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
const addDays = (date, days) => {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
};
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
for (const p of ["workforce.roster.manage", "workforce.roster.correct", "workforce.duty.request"])
  assert(me.body.data.includes(p), `Synthetic owner must hold ${p}`);
if (approverCookie) {
  const approver = await request("iam/me/permissions", "GET", undefined, approverCookie);
  assert.equal(approver.status, 200);
  assert.equal(approver.body.tenant_id, tenant);
  assert(approver.body.data.includes("workforce.duty.approve"), "Approver must hold approve");
}

// Reserved fixtures from verify-staff-enrollment, verify-roster-config and verify-roster-policy.
const giver = "a7777777-7777-4777-8777-777777777771";
const taker = "a7777777-7777-4777-8777-777777777781";
const areaID = "d8888888-8888-4888-8888-888888888881";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const settings = await expect(`${prefix}/hr/roster-settings`, "GET", undefined, 200);
const counterpart = await expect(
  `${prefix}/staff/${taker}`,
  "PUT",
  {
    employee_code: "SYN-EXCHANGE-102",
    display_name: "Jordan Exchange Synthetic",
    job_category: "nurse",
    employment_status: "active",
  },
  200,
);
assert.match(counterpart.employee_code, /^SYN-/);
const area = (await list(`${prefix}/hr/roster-configs?kind=area`)).find((c) => c.id === areaID);
assert(area?.active, "Run verify-roster-config.mjs first to create the synthetic area.");
const day = (await list(`${prefix}/hr/shifts`)).find(
  (s) => s.code === "SYN-PLAN-DAY" && area.shift_revision_ids.includes(s.revision_id),
)?.revision_id;
assert(day, "Area must pin the current SYN-PLAN-DAY revision; rerun verify-roster-config.mjs.");

// A one-week roster after every live roster, so the exchanged duties are weeks in the future.
const rostersPath = `${prefix}/hr/rosters`;
const live = (await list(rostersPath)).filter((r) => r.status !== "discarded");
let start = [addDays(new Date().toISOString().slice(0, 10), 28), area.effective_on]
  .concat(live.map((r) => addDays(r.end_date, 1)))
  .sort()
  .at(-1);
while ((new Date(`${start}T00:00:00Z`).getUTCDay() + 6) % 7 !== settings.week_start)
  start = addDays(start, 1);
const rosterID = randomUUID();
const rosterPath = `${rostersPath}/${rosterID}`;
await expect(
  rosterPath,
  "PUT",
  {
    name: `Synthetic exchange ${start}`,
    start_date: start,
    end_date: addDays(start, 6),
    reviewed: true,
  },
  200,
);
const write = (action, version, extra = {}) =>
  request(`${rosterPath}/${action}`, "POST", {
    change_id: randomUUID(),
    expected_version: version,
    reviewed: true,
    ...extra,
  });
const duty = (staff, date) => ({
  id: randomUUID(),
  staff_id: staff,
  roster_date: date,
  shift_revision_id: day,
  area_revision_id: area.revision_id,
});
const given = duty(giver, start);
const returned = duty(taker, addDays(start, 1));
for (const [i, a] of [given, returned].entries()) {
  const r = await write("changes", i + 1, { mode: "employee", add: [a], remove: [] });
  assert.equal(r.status, 200, JSON.stringify(r.body));
}
let r = await write("freeze", 3);
assert.equal(r.status, 200, JSON.stringify(r.body));
const holders = async () =>
  Object.fromEntries(
    (await list(`${rosterPath}/allocations`)).map((a) => [
      a.roster_date,
      [a.staff_id, a.exchange_id],
    ]),
  );
const scoped = await list(`${rosterPath}/allocations?staff_id=${taker}`);
assert.deepEqual(
  scoped.map((a) => a.id),
  [returned.id],
  "the staff_id filter returns only that person's duties",
);

const exchanges = `${prefix}/hr/duty-exchanges`;
const swap = {
  kind: "swap",
  requester_allocation_id: given.id,
  counterpart_staff_id: taker,
  counterpart_allocation_id: returned.id,
  counterpart_consent: true,
  reason: "Synthetic verification swap",
};
const id = randomUUID();
const filed = await expect(`${exchanges}/${id}`, "PUT", swap, 200);
assert.equal(filed.exchange.status, "pending");
assert.equal(filed.exchange.duty_from, start);
assert.equal(filed.exchange.duty_to, addDays(start, 1));
const replayed = await expect(`${exchanges}/${id}`, "PUT", swap, 200);
assert.equal(
  replayed.exchange.version,
  filed.exchange.version,
  "replay returns the stored exchange",
);
assert.equal(
  (await expect(`${exchanges}/${id}`, "PUT", { ...swap, reason: "Synthetic changed" }, 409)).error,
  "exchange_reused",
);
assert.equal(
  (await expect(`${exchanges}/${randomUUID()}`, "PUT", swap, 409)).error,
  "allocation_in_exchange",
);
await expect(`${exchanges}/${randomUUID()}`, "PUT", { ...swap, kind: "cover" }, 400);
await expect(`${exchanges}/${randomUUID()}`, "PUT", { ...swap, counterpart_consent: false }, 400);
await expect(
  `${exchanges}/${id}/decision`,
  "POST",
  { expected_version: 1, decision: "approve", note: "" },
  403,
);
r = await write("unfreeze", 4, { reason: "Synthetic pending check" });
assert.equal(r.status, 409, JSON.stringify(r.body));
assert.equal(r.body.error, "pending_exchanges");
const pending = await list(`${exchanges}?status=pending&staff_id=${giver}`);
assert(pending.some((e) => e.id === id && e.reason === swap.reason));
const cancel = { expected_version: 1, note: "" };
const withdrawn = await expect(`${exchanges}/${id}/cancellation`, "POST", cancel, 200);
assert.equal(withdrawn.exchange.status, "cancelled");
await expect(`${exchanges}/${id}/cancellation`, "POST", cancel, 200);
const original = await holders();
assert.deepEqual(original[start], [giver, undefined], "a withdrawn exchange moves nothing");

let approval = "not_run_without_approver_session";
if (approverCookie) {
  const second = randomUUID();
  await expect(`${exchanges}/${second}`, "PUT", swap, 200);
  await expect(
    `${exchanges}/${second}/revocation`,
    "POST",
    { expected_version: 1, note: "Synthetic early" },
    409,
    approverCookie,
  );
  const decide = { expected_version: 1, decision: "approve", note: "" };
  const approved = await expect(
    `${exchanges}/${second}/decision`,
    "POST",
    decide,
    200,
    approverCookie,
  );
  assert.equal(approved.exchange.status, "approved");
  await expect(`${exchanges}/${second}/decision`, "POST", decide, 200, approverCookie);
  const moved = await holders();
  assert.deepEqual(moved[start], [taker, second], "the giver's duty moved to the counterpart");
  assert.deepEqual(moved[addDays(start, 1)], [giver, second], "the returned duty moved back");
  const head = await expect(rosterPath, "GET", undefined, 200);
  assert.equal(head.status, "frozen", "approval never reopens the roster");
  assert.equal(head.version, 5, "approval advances the frozen roster exactly once");
  const revoke = { expected_version: approved.exchange.version, note: "" };
  await expect(`${exchanges}/${second}/revocation`, "POST", revoke, 400, approverCookie);
  revoke.note = "Synthetic reversal";
  const revoked = await expect(
    `${exchanges}/${second}/revocation`,
    "POST",
    revoke,
    200,
    approverCookie,
  );
  assert.equal(revoked.exchange.status, "revoked");
  await expect(`${exchanges}/${second}/revocation`, "POST", revoke, 200, approverCookie);
  const restored = await holders();
  assert.equal(restored[start][0], giver, "revocation hands each duty back");
  assert.equal(restored[addDays(start, 1)][0], taker);
  const actions = (await list(`${rosterPath}/revisions`)).map((v) => v.action);
  assert.equal(actions.filter((a) => a === "exchanged").length, 2, "approve and revoke, once each");
  approval = "approved_moved_and_revoked_once";
}

const head = await expect(rosterPath, "GET", undefined, 200);
r = await write("unfreeze", head.version, { reason: "Synthetic verification complete" });
assert.equal(r.status, 200, JSON.stringify(r.body));
r = await write("discard", head.version + 1, { reason: "Synthetic verification complete" });
assert.equal(r.status, 200, JSON.stringify(r.body));
const foreign = "workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
await expect(`${foreign}/hr/duty-exchanges`, "GET", undefined, 404);
await expect(`${foreign}/hr/duty-exchanges/${id}`, "GET", undefined, 404);

console.log(
  JSON.stringify({
    roster: rosterID,
    duties: [start, addDays(start, 1)],
    staff_id_filter: true,
    replay_same_version: true,
    id_reuse: "exchange_reused",
    held_duty: "allocation_in_exchange",
    cover_with_return_duty: 400,
    missing_consent: 400,
    self_decision: 403,
    unfreeze_with_pending: "pending_exchanges",
    withdraw_replay: true,
    withdrawn_moves_nothing: true,
    revoke_pending: approverCookie ? 409 : "not_run",
    revoke_without_note: approverCookie ? 400 : "not_run",
    approval,
    roster_discarded: true,
    foreign_hospital: 404,
    no_store: true,
  }),
);
