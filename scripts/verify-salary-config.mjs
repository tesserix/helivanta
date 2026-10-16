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
for (const p of ["workforce.payroll.view", "workforce.payroll.configure"])
  assert(me.body.data.includes(p), `Synthetic verifier must hold ${p}`);

// Reserved fixtures: ids, codes and the effective date are fixed so reruns replay identical revisions.
const ids = {
  basic: "a8888888-8888-4888-8888-888888888801",
  hra: "a8888888-8888-4888-8888-888888888802",
  pf: "a8888888-8888-4888-8888-888888888803",
  structure: "a8888888-8888-4888-8888-888888888810",
};
const on = "2026-11-01";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}/hr/payroll`;
const months = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12];
const head = (over) => ({
  expected_version: 0,
  kind: "earning",
  active: true,
  basis: "fixed",
  base_head_ids: [],
  proration: "calendar_days",
  minimum_payable_half_days: 0,
  months,
  rounding_mode: "nearest",
  rounding_unit_paise: 100,
  in_gross: true,
  in_ctc: true,
  arrears_eligible: true,
  taxability: "taxable",
  wage_class: "wages",
  payslip_order: 10,
  show_when_zero: false,
  effective_on: on,
  reviewed: true,
  ...over,
});
const bodies = {
  basic: head({ code: "SYN-VERIFY-BASIC", name: "Synthetic Verify Basic", payslip_label: "Basic" }),
  hra: head({
    code: "SYN-VERIFY-HRA",
    name: "Synthetic Verify HRA",
    payslip_label: "HRA",
    basis: "percent",
    rate_ppm: 400000,
    base_head_ids: [ids.basic],
    proration: "none",
    taxability: "exempt",
    tax_note: "Synthetic exemption basis for verification",
    wage_class: "excluded",
    payslip_order: 20,
  }),
  pf: head({
    kind: "deduction",
    code: "SYN-VERIFY-PF",
    name: "Synthetic Verify PF",
    payslip_label: "PF",
    basis: "percent",
    rate_ppm: 120000,
    base_head_ids: [ids.basic],
    proration: "none",
    max_amount_paise: 180000,
    in_gross: false,
    in_ctc: false,
    taxability: "not_applicable",
    wage_class: "outside",
    payslip_order: 30,
  }),
};
const heads = {};
for (const key of ["basic", "hra", "pf"]) {
  heads[key] = await expect(`${prefix}/salary-heads/${ids[key]}`, "PUT", bodies[key], 200);
  assert.equal(heads[key].id, ids[key]);
  assert.equal(heads[key].code, bodies[key].code);
  assert.equal(heads[key].reviewed_by_subject.length > 0, true);
  // An identical body replays; a different one against a stale version is refused.
  const replay = await expect(`${prefix}/salary-heads/${ids[key]}`, "PUT", bodies[key], 200);
  assert.equal(replay.revision_id, heads[key].revision_id);
}
assert.equal(
  (
    await expect(
      `${prefix}/salary-heads/${ids.basic}`,
      "PUT",
      { ...bodies.basic, expected_version: heads.basic.version - 1, name: "Synthetic stale" },
      409,
    )
  ).error,
  "conflict",
);
const cycle = await expect(
  `${prefix}/salary-heads/${ids.hra}`,
  "PUT",
  { ...bodies.hra, expected_version: heads.hra.version, base_head_ids: [ids.hra] },
  409,
);
assert.equal(cycle.reason, "cycle");
assert.equal(cycle.head_id, ids.hra);
const deductionBase = await expect(
  `${prefix}/salary-heads/${randomUUID()}`,
  "PUT",
  head({
    code: `SYN-VERIFY-${randomUUID().slice(0, 8).toUpperCase()}`,
    name: `Synthetic refused ${randomUUID().slice(0, 8)}`,
    payslip_label: "Refused",
    basis: "percent",
    rate_ppm: 10000,
    base_head_ids: [ids.pf],
    proration: "none",
  }),
  400,
);
assert.equal(deductionBase.error, "invalid_reference");

const structureBody = {
  expected_version: 0,
  code: "SYN-VERIFY-STAFF",
  name: "Synthetic Verify Staff Pay",
  description: "Synthetic verification structure",
  active: true,
  lines: [{ head_id: ids.basic, amount_paise: 3000000 }, { head_id: ids.hra }, { head_id: ids.pf }],
  effective_on: on,
  reviewed: true,
};
const structure = await expect(
  `${prefix}/salary-structures/${ids.structure}`,
  "PUT",
  structureBody,
  200,
);
assert.deepEqual(
  structure.lines.map((l) => [l.head_id, l.amount_paise]),
  [
    [ids.basic, 3000000],
    [ids.hra, undefined],
    [ids.pf, undefined],
  ],
);
const incomplete = await expect(
  `${prefix}/salary-structures/${randomUUID()}`,
  "PUT",
  {
    ...structureBody,
    code: `SYN-VERIFY-${randomUUID().slice(0, 8).toUpperCase()}`,
    name: `Synthetic incomplete ${randomUUID().slice(0, 8)}`,
    lines: [{ head_id: ids.hra }],
  },
  409,
);
assert.equal(incomplete.reason, "structure_incomplete");

// November has 30 days: a full month and half a month, computed from the reviewed values only.
const preview = (half) =>
  expect(
    `${prefix}/salary-structures/${ids.structure}/preview`,
    "POST",
    { on: "2026-11-15", payable_half_days: half },
    200,
  );
const amounts = (p) => Object.fromEntries(p.lines.map((l) => [l.head_id, l.amount_paise]));
const full = await preview(60);
assert.equal(full.period_days, 30);
assert.deepEqual(amounts(full), { [ids.basic]: 3000000, [ids.hra]: 1200000, [ids.pf]: 180000 });
assert.deepEqual(full.totals, {
  gross_paise: 4200000,
  deductions_paise: 180000,
  employer_paise: 0,
  net_paise: 4020000,
  ctc_paise: 4200000,
  negative_net: false,
});
assert.equal(full.wage_test.wages_paise, 3000000);
assert.equal(full.wage_test.excluded_paise, 1200000);
assert.equal(full.wage_test.add_back_paise, 0);
assert.equal(full.wage_test.statutory_wages_paise, 3000000);
const half = await preview(30);
assert.deepEqual(amounts(half), { [ids.basic]: 1500000, [ids.hra]: 600000, [ids.pf]: 180000 });
assert.equal(half.totals.net_paise, 1920000);
await expect(
  `${prefix}/salary-structures/${ids.structure}/preview`,
  "POST",
  { on: "2026-11-15", payable_half_days: 61 },
  400,
);
assert.equal(
  (
    await expect(
      `${prefix}/salary-structures/${ids.structure}/preview`,
      "POST",
      { on: "2026-10-31", payable_half_days: 62 },
      409,
    )
  ).reason,
  "not_in_force",
);

const listed = await list(`${prefix}/salary-heads?kind=deduction`);
assert(listed.some((h) => h.id === ids.pf));
assert(listed.every((h) => h.kind === "deduction"));
const history = await list(`${prefix}/salary-heads/${ids.basic}/history`);
assert.equal(history[0].version, heads.basic.version);
const structures = await list(`${prefix}/salary-structures`);
assert(structures.some((s) => s.id === ids.structure));
console.log(
  `Salary configuration verification passed: structure ${ids.structure} version ${structure.version}.`,
);
