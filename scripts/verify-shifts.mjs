import assert from "node:assert/strict";

const base = process.env.HELIVANTA_VERIFY_API;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
assert(base && cookie && hospital, "Provide the authenticated synthetic verification scope.");
const origin = new URL(base);
assert(
  origin.protocol === "https:" || ["localhost", "127.0.0.1"].includes(origin.hostname),
  "Use HTTPS or loopback.",
);
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}/hr/shifts`;
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
async function list() {
  const rows = [];
  let cursor;
  for (let page = 0; page < 100; page++) {
    const result = await request(
      `${prefix}?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
    );
    assert.equal(result.status, 200);
    rows.push(...result.body.data);
    if (!result.body.page.has_more) return rows;
    cursor = result.body.page.next_cursor;
    assert(cursor);
  }
  throw new Error("Verification page limit exceeded.");
}
const id = "c9999999-9999-4999-8999-999999999991";
const path = `${prefix}/${id}`;
const before = (await list()).find((row) => row.id === id);
const baseline = {
  expected_version: before?.version ?? 0,
  shift_type: "night",
  code: "SYN-VERIFY-NIGHT",
  name: "Synthetic Verification Night",
  description: "Synthetic shift policy; no source personnel or salary data.",
  active: true,
  start_time: "22:00",
  end_time: "06:00",
  break_minutes: 30,
  grace_minutes: 10,
  full_day_ot_minutes: 120,
  half_day_ot_minutes: 60,
  locum: true,
};
const retries = await Promise.all(Array.from({ length: 4 }, () => request(path, "PUT", baseline)));
for (const result of retries) {
  assert.equal(result.status, 200);
  assert.deepEqual(result.body, retries[0].body);
}
const saved = retries[0].body;
assert.equal(saved.duration_minutes, 480);
assert.equal((await list()).filter((row) => row.id === id).length, 1);
assert.equal(
  (
    await request(`${prefix}/c9999999-9999-4999-8999-999999999992`, "PUT", {
      ...baseline,
      expected_version: 0,
      code: baseline.code.toLowerCase(),
    })
  ).status,
  409,
);
const changed = { ...baseline, expected_version: saved.version, end_time: "07:00" };
const updated = await request(path, "PUT", changed);
assert.equal(updated.status, 200);
assert.equal(updated.body.version, saved.version + 1);
assert.equal(updated.body.duration_minutes, 540);
assert.equal((await request(path, "PUT", { ...changed, end_time: "08:00" })).status, 409);
assert.deepEqual((await request(path, "PUT", changed)).body, updated.body);
const history = await request(`${path}/history`);
assert.equal(history.status, 200);
assert(history.body.data.some((row) => row.id === saved.revision_id && row.end_time === "06:00"));
assert(
  history.body.data.some((row) => row.id === updated.body.revision_id && row.end_time === "07:00"),
);
for (const invalid of [
  { start_time: "25:00" },
  { end_time: "22:00" },
  { break_minutes: 480 },
  { half_day_ot_minutes: 130 },
  { name: "Synthetic\0Invalid" },
  { tenant_id: "injected" },
]) {
  assert.equal(
    (
      await request(`${prefix}/c9999999-9999-4999-8999-999999999993`, "PUT", {
        ...baseline,
        expected_version: 0,
        ...invalid,
      })
    ).status,
    400,
  );
}
assert.equal(
  (await request("workforce/hospitals/eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee/hr/shifts")).status,
  404,
);
const retired = await request(path, "PUT", {
  ...changed,
  expected_version: updated.body.version,
  active: false,
});
assert.equal(retired.status, 200);
const restored = await request(path, "PUT", {
  ...baseline,
  expected_version: retired.body.version,
});
assert.equal(restored.status, 200);
console.log(
  JSON.stringify({
    concurrent_retries: 4,
    unique_shift: true,
    overnight_duration: 480,
    immutable_history_readable: true,
    normalized_duplicate: 409,
    stale_edit: 409,
    foreign_hospital: 404,
    retire_reactivate: true,
    no_store: true,
  }),
);
