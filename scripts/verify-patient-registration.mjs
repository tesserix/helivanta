import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const base = new URL(process.env.HELIVANTA_VERIFY_API ?? "http://localhost:8080/v1/");
const tenant = process.env.HELIVANTA_VERIFY_TENANT;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
const cookie = process.env.HELIVANTA_VERIFY_COOKIE;
const local = ["localhost", "127.0.0.1", "helivanta.localhost"].includes(base.hostname);
assert(
  local || (base.origin === "https://helivanta.app" && base.pathname === "/api/v1/"),
  "Use the local API or Helivanta production API.",
);
assert(!base.username && !base.password && !base.search && !base.hash, "Unexpected API URL.");
assert(
  tenant ===
    (local ? "11111111-1111-1111-1111-111111111111" : "8f8ed375-596b-4633-b90b-27fd3e6f299c"),
  "Only the designated synthetic tenant is supported.",
);
assert(/^[0-9a-f-]{36}$/.test(hospital ?? ""), "Set the synthetic hospital ID.");
assert(cookie, "Supply an authenticated test session in HELIVANTA_VERIFY_COOKIE.");

async function request(path, body, expected = 200) {
  const response = await fetch(new URL(path, base), {
    method: body === undefined ? "GET" : "POST",
    headers: { "Content-Type": "application/json", Cookie: cookie },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
    redirect: "error",
  });
  assert.equal(response.status, expected, `${path.split("?")[0]} returned an unexpected status`);
  return response.json();
}

const identity = await request("iam/me/permissions");
assert.equal(identity.tenant_id, tenant, "Session belongs to another tenant.");
const fixtures = JSON.parse(
  await readFile(new URL("./fixtures/patients.synthetic.json", import.meta.url), "utf8"),
);
const path = `patients?hospital_id=${hospital}`;
const created = [];
for (const fixture of fixtures) {
  const initial = await request(path, fixture, 201);
  const retry = await request(path, fixture, 201);
  assert.deepEqual(retry, initial, "Registration retry changed the saved result.");
  assert.equal(initial.patient.hospital_id, hospital);
  assert.equal(initial.patient.family_name, "Synthetic");
  created.push(initial.patient);
}
const concurrent = await Promise.all(
  Array.from({ length: 8 }, () => request(path, fixtures[0], 201)),
);
assert(
  concurrent.every((result) => result.patient.id === created[0].id),
  "Concurrent retries created different patients.",
);
await request(path, { ...fixtures[0], given_name: "ChangedSynthetic" }, 409);
await request(path, { ...fixtures[0], request_id: "0e400004-4444-4444-8444-444444444444" }, 409);
await request("patients", undefined, 404);
await request(
  `patients/${created[0].id}?hospital_id=eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee`,
  undefined,
  404,
);
const page = await request(`patients/search?hospital_id=${hospital}`, { query: "Avery" });
assert.equal(page.data.filter((patient) => patient.id === created[0].id).length, 1);
console.log(
  JSON.stringify({
    origin: base.origin,
    synthetic_patients: created.length,
    unique_mrns: new Set(created.map((patient) => patient.mrn)).size,
    concurrent_retries: concurrent.length,
    idempotency: "passed",
    duplicate_review: "passed",
    missing_scope: "denied",
    ungranted_scope: "denied",
    search: "passed",
  }),
);
