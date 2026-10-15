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
for (const p of [
  "workforce.leave.configure",
  "workforce.leave.allocate",
  "workforce.leave.correct",
])
  assert(me.body.data.includes(p), `Synthetic owner must hold ${p}`);

const scope = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const staff = await expect(`${scope}/staff/${encodeURIComponent(staffID)}`, "GET", undefined, 200);
assert.match(staff.employee_code, /^SYN-/i, "Verification staff must carry a synthetic code");

// verify-leave-setup.mjs owns these fixtures; run it first.
const structureID = "f1111111-1111-4111-8111-111111111114";
const earnedID = "f1111111-1111-4111-8111-111111111111";
const structure = (await list(`${scope}/hr/leave-configs?kind=structure`)).find(
  (r) => r.id === structureID,
);
assert(structure, "Run scripts/verify-leave-setup.mjs before this script.");

await expect(`${scope}/hr/leave-settings`, "PUT", { year_start_month: 1, reviewed: false }, 400);
const current = await request(`${scope}/hr/leave-settings`);
assert([200, 404].includes(current.status), JSON.stringify(current.body));
const settings =
  current.status === 404
    ? await expect(
        `${scope}/hr/leave-settings`,
        "PUT",
        { year_start_month: 1, reviewed: true },
        200,
      )
    : current.body;
const otherMonth = (settings.year_start_month % 12) + 1;
await expect(
  `${scope}/hr/leave-settings`,
  "PUT",
  { year_start_month: otherMonth, reviewed: true },
  409,
);

// Today anywhere on Earth, matching the server's guard.
const today = new Date(Date.now() - 12 * 3600_000).toISOString().slice(0, 10);
const thisYear = Number(today.slice(0, 4));
const leaveYear =
  today.slice(5, 7) >= String(settings.year_start_month).padStart(2, "0") ? thisYear : thisYear - 1;
await expect(`${scope}/hr/leave-years/${thisYear + 2}`, "PUT", undefined, 409);
const year = await expect(`${scope}/hr/leave-years/${leaveYear}`, "PUT", undefined, 200);
const reopened = await expect(`${scope}/hr/leave-years/${leaveYear}`, "PUT", undefined, 200);
assert.equal(reopened.id, year.id, "reopening returns the same year");

const assignments = await list(`${scope}/hr/leave-assignments`);
const assigned = assignments.find((a) => a.staff_id === staffID);
if (!assigned || assigned.structure_revision_id !== structure.revision_id || !assigned.active) {
  const saved = await expect(
    `${scope}/staff/${staffID}/leave-assignment`,
    "PUT",
    {
      expected_version: assigned?.version ?? 0,
      structure_revision_id: structure.revision_id,
      active: true,
      effective_on:
        assigned && assigned.effective_on > year.start_on ? assigned.effective_on : year.start_on,
      reviewed: true,
    },
    200,
  );
  assert.equal(saved.structure_revision_id, structure.revision_id);
}

// Post every started month; reposting a month replays its run instead of crediting twice.
const started = (step) => {
  const d = new Date(`${year.start_on}T00:00:00Z`);
  d.setUTCMonth(d.getUTCMonth() + step - 1);
  return d.toISOString().slice(0, 10) <= today;
};
let posted = (await list(`${scope}/hr/leave-years`)).find((y) => y.id === year.id).posted_months;
if (posted < 11 && started(posted + 2))
  await expect(`${scope}/hr/leave-years/${leaveYear}/postings`, "POST", { month: posted + 2 }, 409);
const runs = [];
while (posted < 12 && started(posted + 1)) {
  posted += 1;
  runs.push(
    await expect(`${scope}/hr/leave-years/${leaveYear}/postings`, "POST", { month: posted }, 200),
  );
}
assert(posted >= 1, "At least the first month of the current leave year has started.");
const replays = await Promise.all(
  Array.from({ length: 4 }, () =>
    request(`${scope}/hr/leave-years/${leaveYear}/postings`, "POST", { month: posted }),
  ),
);
const replayIDs = new Set(replays.map((r) => (assert.equal(r.status, 200), r.body.id)));
assert.equal(replayIDs.size, 1, "concurrent reposts return one run");
if (posted < 12)
  await expect(`${scope}/hr/leave-years/${leaveYear}/postings`, "POST", { month: posted + 1 }, 409);
await expect(`${scope}/hr/leave-years/${leaveYear}/close`, "POST", undefined, 409);

const ledger = await list(`${scope}/staff/${staffID}/leave-ledger?leave_year=${leaveYear}`);
const sum = ledger.filter((e) => e.leave_type_id === earnedID).reduce((t, e) => t + e.half_days, 0);
const balances = await expect(
  `${scope}/staff/${staffID}/leave-balances?leave_year=${leaveYear}`,
  "GET",
  undefined,
  200,
);
const earned = balances.balances.find((b) => b.leave_type_id === earnedID);
assert(earned, "the tracked synthetic type has a balance");
assert.equal(earned.half_days, sum, "balance is the ledger sum");

// Reserved correction id: reruns replay it; reuse with other values conflicts.
const correctionID = "f1111111-1111-4111-8111-111111111121";
const correction = {
  leave_type_id: earnedID,
  leave_year: leaveYear,
  half_days: 1,
  effective_on: year.start_on,
  reason: "Synthetic verification correction",
};
const first = await expect(
  `${scope}/staff/${staffID}/leave-corrections/${correctionID}`,
  "PUT",
  correction,
  200,
);
const again = await expect(
  `${scope}/staff/${staffID}/leave-corrections/${correctionID}`,
  "PUT",
  correction,
  200,
);
assert.equal(again.id, first.id);
await expect(
  `${scope}/staff/${staffID}/leave-corrections/${correctionID}`,
  "PUT",
  { ...correction, half_days: 2 },
  409,
);
await expect(
  `${scope}/staff/${staffID}/leave-corrections/f1111111-1111-4111-8111-111111111122`,
  "PUT",
  { ...correction, half_days: 0 },
  400,
);
await expect(
  `${scope}/staff/${staffID}/leave-corrections/f1111111-1111-4111-8111-111111111122`,
  "PUT",
  { ...correction, reason: "short" },
  400,
);
const foreign = "workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
for (const path of [
  "hr/leave-settings",
  "hr/leave-years",
  `hr/leave-balances?leave_year=${leaveYear}`,
])
  await expect(`${foreign}/${path}`, "GET", undefined, 404);

console.log(
  JSON.stringify({
    leave_year: leaveYear,
    year_start_month: settings.year_start_month,
    months_posted: posted,
    new_runs: runs.length,
    concurrent_reposts: 4,
    balance_equals_ledger_sum: true,
    unreviewed_settings: 400,
    settings_change: 409,
    year_beyond_next: 409,
    out_of_order_or_unstarted_month: 409,
    close_before_year_end: 409,
    correction_replay: true,
    correction_id_reuse: 409,
    zero_or_unexplained_correction: 400,
    foreign_hospital: 404,
    no_store: true,
  }),
);
