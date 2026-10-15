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
const later = (a, b) => (a > b ? a : b);
const me = await request("iam/me/permissions");
assert.equal(me.status, 200);
assert.equal(me.body.tenant_id, tenant);
assert(me.body.data.includes("workforce.roster.manage"), "Synthetic owner must manage rosters");

// Reserved fixtures maintained by verify-staff-enrollment.mjs and verify-roster-config.mjs.
const staffID = "a7777777-7777-4777-8777-777777777771";
const templateID = "d8888888-8888-4888-8888-888888888883";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}`;
const settingsPath = `${prefix}/hr/roster-settings`;
const assignmentPath = `${prefix}/staff/${staffID}/roster-assignment`;
const template = (await list(`${prefix}/hr/roster-configs?kind=weekly_template`)).find(
  (r) => r.id === templateID,
);
assert(template?.active, "Run verify-roster-config.mjs first to create the synthetic template.");

let current = await request(settingsPath);
if (current.status === 404) {
  current = await request(settingsPath, "PUT", {
    expected_version: 0,
    timezone: "Asia/Kolkata",
    week_start: 0,
    auto_population: "disabled",
    effective_on: new Date().toISOString().slice(0, 10),
    reviewed: true,
  });
}
assert.equal(current.status, 200);
const settings = current.body;
const same = {
  expected_version: settings.version,
  timezone: settings.timezone,
  week_start: settings.week_start,
  auto_population: settings.auto_population,
  effective_on: settings.effective_on,
  reviewed: true,
};
const retries = await Promise.all(
  Array.from({ length: 4 }, () => request(settingsPath, "PUT", same)),
);
for (const r of retries) {
  assert.equal(r.status, 200);
  assert.equal(r.body.revision_id, settings.revision_id);
  assert.equal(r.body.version, settings.version);
}
const changed = { ...same, week_start: (settings.week_start + 1) % 7 };
for (const timezone of ["Local", "Mars/Olympus", "asia/kolkata"]) {
  const r = await request(settingsPath, "PUT", { ...changed, timezone });
  assert.equal(r.status, 400, timezone);
}
assert.equal(
  (await request(settingsPath, "PUT", { ...changed, effective_on: "2000-01-01" })).status,
  400,
);
assert.equal(
  (await request(settingsPath, "PUT", { ...changed, expected_version: settings.version + 1 }))
    .status,
  409,
);
assert.equal((await request(settingsPath, "PUT", { ...changed, reviewed: false })).status, 400);
const settingsHistory = await list(`${settingsPath}/history`);
assert(settingsHistory.some((r) => r.revision_id === settings.revision_id));
assert.equal((await request(settingsPath)).body.version, settings.version);

const existing = (await list(`${prefix}/hr/roster-assignments`)).find(
  (a) => a.staff_id === staffID,
);
const assignment = {
  expected_version: existing?.version ?? 0,
  weekly_template_revision_id: template.revision_id,
  fixed: true,
  active: true,
  effective_on: later(existing?.effective_on ?? template.effective_on, template.effective_on),
  reviewed: true,
};
let saved = await request(assignmentPath, "PUT", assignment);
assert.equal(saved.status, 200, JSON.stringify(saved.body));
const pinned = saved.body;
assert.equal(pinned.weekly_template_revision_id, template.revision_id);
assert.equal(pinned.template_code, template.code);
saved = await request(assignmentPath, "PUT", { ...assignment, expected_version: pinned.version });
assert.equal(saved.body.revision_id, pinned.revision_id);
const unknown = await request(assignmentPath, "PUT", {
  ...assignment,
  expected_version: pinned.version,
  weekly_template_revision_id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
});
assert.equal(unknown.status, 400);
assert.equal(unknown.body.error, "invalid_reference");
assert.equal(
  (
    await request(assignmentPath, "PUT", {
      ...assignment,
      fixed: false,
      expected_version: pinned.version + 1,
    })
  ).status,
  409,
);
const assignmentHistory = await list(`${assignmentPath}/history`);
assert(assignmentHistory.some((r) => r.revision_id === pinned.revision_id));
assert.equal(
  (await request("workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee/hr/roster-settings"))
    .status,
  404,
);
console.log(
  JSON.stringify({
    settings_version: settings.version,
    concurrent_settings_retries: 4,
    invalid_timezones: 400,
    backdated_effective_date: 400,
    unreviewed_change: 400,
    stale_edit: 409,
    assignment_version: pinned.version,
    unknown_template_revision: "invalid_reference",
    foreign_hospital: 404,
    no_store: true,
  }),
);
