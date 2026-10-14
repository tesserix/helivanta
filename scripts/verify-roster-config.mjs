import assert from "node:assert/strict";
const base = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
assert(base && cookie && hospital, "Provide an authenticated synthetic verification scope.");
const origin = new URL(base);
assert(
  origin.protocol === "https:" || ["localhost", "127.0.0.1"].includes(origin.hostname),
  "Use HTTPS or loopback.",
);
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}/hr`;
const date = new Date().toISOString().slice(0, 10);
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
const shiftPath = `${prefix}/shifts`,
  configPath = `${prefix}/roster-configs`;
const existingShifts = await list(shiftPath);
const shiftIDs = [
  "c8888888-8888-4888-8888-888888888881",
  "c8888888-8888-4888-8888-888888888882",
  "c8888888-8888-4888-8888-888888888883",
];
const shiftBodies = shiftIDs.map((id, i) => ({
  expected_version: existingShifts.find((s) => s.id === id)?.version ?? 0,
  shift_type: i === 1 ? "night" : "day",
  code: ["SYN-PLAN-DAY", "SYN-PLAN-NIGHT", "SYN-PLAN-OVERLAP"][i],
  name: ["Synthetic Planning Day", "Synthetic Planning Night", "Synthetic Planning Overlap"][i],
  description: "Synthetic policy only; no source staff or patient data.",
  active: true,
  start_time: ["08:00", "22:00", "14:00"][i],
  end_time: ["16:00", "06:00", "20:00"][i],
  break_minutes: 0,
  grace_minutes: 0,
  full_day_ot_minutes: 0,
  half_day_ot_minutes: 0,
  locum: false,
}));
const shifts = [];
for (let i = 0; i < shiftIDs.length; i++) {
  const result = await request(`${shiftPath}/${shiftIDs[i]}`, "PUT", shiftBodies[i]);
  assert.equal(result.status, 200);
  shifts.push(result.body);
}
const ids = {
  area: "d8888888-8888-4888-8888-888888888881",
  group: "d8888888-8888-4888-8888-888888888882",
  template: "d8888888-8888-4888-8888-888888888883",
};
const existing = await list(configPath);
const areaBody = {
  expected_version: existing.find((c) => c.id === ids.area)?.version ?? 0,
  kind: "area",
  code: "SYN-PLAN-AREA",
  name: "Synthetic Planning Area",
  description: "Synthetic configuration acceptance.",
  active: true,
  effective_on: date,
  reviewed: true,
  shift_revision_ids: shifts.map((s) => s.revision_id),
  days: [],
};
const areaRetries = await Promise.all(
  Array.from({ length: 4 }, () => request(`${configPath}/${ids.area}`, "PUT", areaBody)),
);
for (const result of areaRetries) {
  assert.equal(result.status, 200);
  assert.deepEqual(result.body, areaRetries[0].body);
}
let area = areaRetries[0].body;
assert.equal(
  (
    await request(`${configPath}/d8888888-8888-4888-8888-888888888884`, "PUT", {
      ...areaBody,
      expected_version: 0,
      code: areaBody.code.toLowerCase(),
    })
  ).status,
  409,
);
const groupBody = {
  ...areaBody,
  expected_version: existing.find((c) => c.id === ids.group)?.version ?? 0,
  kind: "group",
  code: "SYN-PLAN-GROUP",
  name: "Synthetic Planning Group",
};
let result = await request(`${configPath}/${ids.group}`, "PUT", groupBody);
assert.equal(result.status, 200);
const days = Array.from({ length: 7 }, (_, weekday) => ({
  weekday,
  weekly_off: weekday >= 5,
  allocations:
    weekday < 5
      ? [{ shift_revision_id: shifts[0].revision_id, area_revision_id: area.revision_id }]
      : [],
}));
days[0].allocations.push({
  shift_revision_id: shifts[1].revision_id,
  area_revision_id: area.revision_id,
});
const templateBody = {
  ...areaBody,
  expected_version: existing.find((c) => c.id === ids.template)?.version ?? 0,
  kind: "weekly_template",
  code: "SYN-PLAN-WEEK",
  name: "Synthetic Planning Week",
  shift_revision_ids: [],
  days,
};
result = await request(`${configPath}/${ids.template}`, "PUT", templateBody);
assert.equal(result.status, 200);
const saved = result.body;
assert.equal(saved.days.length, 7);
assert.equal(saved.days[0].allocations.length, 2);
assert.equal(saved.days[6].weekly_off, true);
const changed = {
  ...templateBody,
  expected_version: saved.version,
  name: "Synthetic Planning Week Revised",
};
result = await request(`${configPath}/${ids.template}`, "PUT", changed);
assert.equal(result.status, 200);
const updated = result.body;
assert.equal(updated.version, saved.version + 1);
assert.equal(
  (
    await request(`${configPath}/${ids.template}`, "PUT", {
      ...changed,
      name: "Synthetic stale edit",
    })
  ).status,
  409,
);
assert.deepEqual((await request(`${configPath}/${ids.template}`, "PUT", changed)).body, updated);
const history = await request(`${configPath}/${ids.template}/history`);
assert.equal(history.status, 200);
assert(
  history.body.data.some(
    (r) => r.revision_id === saved.revision_id && r.name === "Synthetic Planning Week",
  ),
);
assert(
  history.body.data.some(
    (r) => r.revision_id === updated.revision_id && r.name === "Synthetic Planning Week Revised",
  ),
);
const overlap = structuredClone(templateBody);
overlap.days[0].allocations[1].shift_revision_id = shifts[2].revision_id;
const offWork = structuredClone(templateBody);
offWork.days[4].allocations.push({
  shift_revision_id: shifts[1].revision_id,
  area_revision_id: area.revision_id,
});
const priorDate = new Date(`${date}T00:00:00Z`);
priorDate.setUTCDate(priorDate.getUTCDate() - 1);
for (const invalid of [
  { ...areaBody, reviewed: false },
  { ...areaBody, tenant_id: "injected" },
  { ...templateBody, effective_on: priorDate.toISOString().slice(0, 10) },
  overlap,
  offWork,
]) {
  const bad = {
    ...invalid,
    expected_version: 0,
    code: "SYN-PLAN-INVALID",
    name: "Synthetic Invalid Configuration",
  };
  assert.equal(
    (await request(`${configPath}/d8888888-8888-4888-8888-888888888885`, "PUT", bad)).status,
    400,
  );
}
assert.equal(
  (await request("workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee/hr/roster-configs"))
    .status,
  404,
);
result = await request(`${shiftPath}/${shiftIDs[0]}`, "PUT", {
  ...shiftBodies[0],
  expected_version: shifts[0].version,
  name: "Synthetic Planning Day Changed",
});
assert.equal(result.status, 200);
const changedShift = result.body;
result = await request(`${configPath}/${ids.template}`, "PUT", {
  ...changed,
  expected_version: updated.version,
  description: "Historical timing and membership preserved.",
});
assert.equal(result.status, 200);
assert.equal(result.body.days[0].allocations[0].shift.name, "Synthetic Planning Day");
const historical = result.body;
result = await request(`${configPath}/${ids.area}`, "PUT", {
  ...areaBody,
  expected_version: area.version,
  active: false,
});
assert.equal(result.status, 200);
area = result.body;
assert.equal(
  (
    await request(`${configPath}/d8888888-8888-4888-8888-888888888886`, "PUT", {
      ...templateBody,
      expected_version: 0,
      code: "SYN-PLAN-RETIRED",
      name: "Synthetic Retired Area Reference",
    })
  ).status,
  400,
);
result = await request(`${shiftPath}/${shiftIDs[0]}`, "PUT", {
  ...shiftBodies[0],
  expected_version: changedShift.version,
});
assert.equal(result.status, 200);
shifts[0] = result.body;
result = await request(`${configPath}/${ids.area}`, "PUT", {
  ...areaBody,
  expected_version: area.version,
  shift_revision_ids: shifts.map((s) => s.revision_id),
});
assert.equal(result.status, 200);
area = result.body;
result = await request(`${configPath}/${ids.group}`, "PUT", {
  ...groupBody,
  expected_version: (await list(configPath)).find((c) => c.id === ids.group).version,
  shift_revision_ids: shifts.map((s) => s.revision_id),
});
assert.equal(result.status, 200);
const restored = structuredClone(templateBody);
restored.expected_version = historical.version;
for (const day of restored.days)
  for (const allocation of day.allocations) {
    allocation.area_revision_id = area.revision_id;
  }
for (const day of restored.days)
  for (const allocation of day.allocations)
    if (allocation.shift_revision_id === saved.days[0].allocations[0].shift_revision_id)
      allocation.shift_revision_id = shifts[0].revision_id;
result = await request(`${configPath}/${ids.template}`, "PUT", restored);
assert.equal(result.status, 200);
const final = await list(configPath);
for (const id of Object.values(ids)) assert.equal(final.filter((r) => r.id === id).length, 1);
console.log(
  JSON.stringify({
    concurrent_area_retries: 4,
    unique_area_group_template: true,
    seven_days: true,
    two_shift_pairs: true,
    overlap_and_off_day_work: 400,
    review_and_scope_validation: 400,
    early_effective_date: 400,
    stale_edit: 409,
    foreign_hospital: 404,
    historical_shift_names_preserved: true,
    retired_area_rejected: true,
    no_store: true,
  }),
);
