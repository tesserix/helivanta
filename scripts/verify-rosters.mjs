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
    assert.equal(response.status, 200);
    data.push(...response.body.data);
    if (!response.body.page.has_more) return data;
    cursor = response.body.page.next_cursor;
    assert(cursor);
  }
  throw new Error("Synthetic verification page limit exceeded.");
}
const addDays = (date, days) => {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
};
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
for (const permission of ["workforce.roster.manage", "workforce.roster.correct"]) {
  assert(me.body.data.includes(permission), `Synthetic owner must hold ${permission}`);
}

// Reserved fixtures maintained by verify-staff-enrollment, verify-roster-config and verify-roster-policy.
const staffID = "a7777777-7777-4777-8777-777777777771";
const areaID = "d8888888-8888-4888-8888-888888888881";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const settings = await request(`${prefix}/hr/roster-settings`);
assert.equal(settings.status, 200, "Run verify-roster-policy.mjs first to review settings.");
const area = (await list(`${prefix}/hr/roster-configs?kind=area`)).find((c) => c.id === areaID);
assert(area?.active, "Run verify-roster-config.mjs first to create the synthetic area.");
const shifts = await list(`${prefix}/hr/shifts`);
const shiftRevision = (code) => {
  const shift = shifts.find(
    (s) => s.code === code && area.shift_revision_ids.includes(s.revision_id),
  );
  assert(shift, `Area must pin the current ${code} revision; rerun verify-roster-config.mjs.`);
  return shift.revision_id;
};
const day = shiftRevision("SYN-PLAN-DAY");
const night = shiftRevision("SYN-PLAN-NIGHT");
const overlap = shiftRevision("SYN-PLAN-OVERLAP");

// A one-week period at least four weeks out, after every live roster, on the reviewed week start.
const rostersPath = `${prefix}/hr/rosters`;
const live = (await list(rostersPath)).filter((r) => r.status !== "discarded");
let start = [addDays(new Date().toISOString().slice(0, 10), 28), area.effective_on]
  .concat(live.map((r) => addDays(r.end_date, 1)))
  .sort()
  .at(-1);
while ((new Date(`${start}T00:00:00Z`).getUTCDay() + 6) % 7 !== settings.body.week_start) {
  start = addDays(start, 1);
}
const end = addDays(start, 6);
const rosterID = randomUUID();
const rosterPath = `${rostersPath}/${rosterID}`;
const create = {
  name: `Synthetic verification ${start}`,
  start_date: start,
  end_date: end,
  reviewed: true,
};
const creates = await Promise.all(
  Array.from({ length: 4 }, () => request(rosterPath, "PUT", create)),
);
for (const r of creates) {
  assert.equal(r.status, 200, JSON.stringify(r.body));
  assert.equal(r.body.version, 1);
  assert.equal(r.body.timezone, settings.body.timezone);
}
assert.equal(
  (
    await request(`${rostersPath}/${randomUUID()}`, "PUT", {
      ...create,
      start_date: addDays(start, 1),
      end_date: addDays(end, 1),
    })
  ).status,
  400,
);
const overlapRoster = await request(`${rostersPath}/${randomUUID()}`, "PUT", create);
assert.equal(overlapRoster.status, 409);
assert.equal(overlapRoster.body.error, "roster_overlap");
assert.equal(
  (await request(`${rostersPath}/${randomUUID()}`, "PUT", { ...create, reviewed: false })).status,
  400,
);

const write = (action, version, extra = {}, changeID = randomUUID()) =>
  request(`${rosterPath}/${action}`, "POST", {
    change_id: changeID,
    expected_version: version,
    reviewed: true,
    ...extra,
  });
const allocation = (shift) => ({
  id: randomUUID(),
  staff_id: staffID,
  roster_date: start,
  shift_revision_id: shift,
  area_revision_id: area.revision_id,
});
const changeID = randomUUID();
const primary = allocation(day);
let result = await write("changes", 1, { mode: "employee", add: [primary], remove: [] }, changeID);
assert.equal(result.status, 200, JSON.stringify(result.body));
const replay = await write(
  "changes",
  1,
  { mode: "employee", add: [primary], remove: [] },
  changeID,
);
assert.equal(replay.status, 200);
assert.equal(replay.body.revision.id, result.body.revision.id);
assert.equal(
  (await write("changes", 1, { mode: "employee", add: [allocation(night)], remove: [] })).status,
  409,
);
const clash = allocation(overlap);
result = await write("changes", 2, { mode: "additional", add: [clash], remove: [] });
assert.equal(result.status, 409);
assert.equal(result.body.reason, "allocation_overlap");
assert.deepEqual(result.body.allocation_ids, [clash.id]);
const extra = allocation(night);
result = await write("changes", 2, { mode: "additional", add: [extra], remove: [] });
assert.equal(result.status, 200, JSON.stringify(result.body));

result = await write("populate", 3, { staff_ids: [staffID] });
let version = 3;
if (settings.body.auto_population === "disabled") {
  assert.equal(result.status, 400);
  assert.equal(result.body.error, "population_disabled");
} else {
  assert.equal(result.status, 200, JSON.stringify(result.body));
  assert(
    result.body.revision.skipped_existing >= 1,
    "population must not overwrite the existing date",
  );
  version = 4;
}
const current = await list(`${rosterPath}/allocations`);
assert(current.some((a) => a.id === primary.id && a.starts_at.endsWith("Z")));
assert.equal((await list(`${rosterPath}/allocations?version=1`)).length, 0);

assert.equal((await write("freeze", version)).status, 200);
result = await write("changes", version + 1, {
  mode: "employee",
  add: [allocation(overlap)],
  remove: [],
});
assert.equal(result.status, 409);
assert.equal(result.body.error, "roster_state");
assert.equal((await write("unfreeze", version + 1)).status, 400, "unfreeze needs a reason");
assert.equal(
  (await write("unfreeze", version + 1, { reason: "Synthetic correction check" })).status,
  200,
);
result = await write("discard", version + 2, { reason: "Synthetic verification complete" });
assert.equal(result.status, 200, JSON.stringify(result.body));
assert.equal(result.body.roster.status, "discarded");
assert.equal(result.body.roster.allocations, 0);
assert.equal((await list(`${rosterPath}/allocations?version=${version}`)).length, current.length);
const history = await list(`${rosterPath}/revisions`);
assert.deepEqual(
  history.map((r) => r.action).sort(),
  [
    "changed",
    "changed",
    "created",
    "discarded",
    "frozen",
    "unfrozen",
    ...(version === 4 ? ["populated"] : []),
  ].sort(),
);
assert.equal(
  (await request(`workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee/hr/rosters/${rosterID}`))
    .status,
  404,
);
console.log(
  JSON.stringify({
    roster: rosterID,
    period: `${start}..${end}`,
    timezone: settings.body.timezone,
    concurrent_create_retries: 4,
    misaligned_or_unreviewed: 400,
    roster_overlap: 409,
    change_replay: "same revision",
    stale_change: 409,
    allocation_overlap: clash.id,
    population:
      settings.body.auto_population === "disabled" ? "population_disabled" : "skipped_existing",
    frozen_change: "roster_state",
    revisions: history.length,
    as_of_after_discard: current.length,
    foreign_hospital: 404,
    no_store: true,
  }),
);
