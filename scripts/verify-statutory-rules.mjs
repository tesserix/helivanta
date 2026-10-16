import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
const tenant = process.env.HELIVANTA_VERIFY_TENANT;
assert(
  ["11111111-1111-1111-1111-111111111111", "8f8ed375-596b-4633-b90b-27fd3e6f299c"].includes(tenant),
  "Verification is restricted to designated synthetic tenants",
);
const base = process.env.HELIVANTA_VERIFY_API;
const proposerCookie = process.env.HELIVANTA_VERIFY_COOKIE;
const approverCookie = process.env.HELIVANTA_VERIFY_APPROVER_COOKIE;
const hospital = process.env.HELIVANTA_VERIFY_HOSPITAL;
assert(
  base && proposerCookie && approverCookie && hospital,
  "Provide proposer and approver sessions for a synthetic verification scope.",
);
const origin = new URL(base);
assert(
  origin.protocol === "https:" || ["localhost", "127.0.0.1"].includes(origin.hostname),
  "Use HTTPS or loopback.",
);
async function request(cookie, path, method = "GET", body) {
  const response = await fetch(new URL(path, origin), {
    method,
    headers: { Cookie: cookie, "Content-Type": "application/json" },
    ...(body ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(60000),
  });
  assert.equal(response.headers.get("cache-control"), "no-store");
  return { status: response.status, body: await response.json() };
}
const as = (cookie) => async (path, method, body, status) => {
  const r = await request(cookie, path, method, body);
  assert.equal(r.status, status, `${method} ${path}: ${JSON.stringify(r.body)}`);
  return r.body;
};
const proposer = as(proposerCookie);
const approver = as(approverCookie);
async function list(path) {
  const data = [];
  let cursor;
  for (let page = 0; page < 100; page++) {
    const body = await proposer(
      `${path}?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
      "GET",
      undefined,
      200,
    );
    data.push(...body.data);
    if (!body.page.has_more) return data;
    cursor = body.page.next_cursor;
    assert(cursor);
  }
  throw new Error("Synthetic verification page limit exceeded.");
}
const whoami = async (cookie, needs) => {
  const me = await request(cookie, "iam/me/permissions");
  assert.equal(me.status, 200);
  assert.equal(me.body.tenant_id, tenant);
  for (const p of needs) assert(me.body.data.includes(p), `Synthetic verifier must hold ${p}`);
  return me.body.subject;
};
const proposerSubject = await whoami(proposerCookie, [
  "workforce.payroll.view",
  "workforce.payroll.configure",
]);
const approverSubject = await whoami(approverCookie, [
  "workforce.payroll.view",
  "workforce.payroll.approve",
]);
assert.notEqual(proposerSubject, approverSubject, "Separation needs two different people.");

// Reserved fixtures; the structure is the one verify-salary-config.mjs maintains. Values are synthetic, never statutory.
const ruleID = "a8888888-8888-4888-8888-888888888820";
const structureID = "a8888888-8888-4888-8888-888888888810";
const prefix = `workforce/hospitals/${encodeURIComponent(hospital)}/hr/payroll`;
const rulePath = `${prefix}/statutory-rules/${ruleID}`;
const pf = {
  wage_basis: "statutory_wages",
  contribution_basis: "capped",
  wage_ceiling_paise: 1000000,
  employee_rate_ppm: 100000,
  employer_rate_ppm: 100000,
  eps_rate_ppm: 60000,
  eps_wage_ceiling_paise: 1000000,
  edli_rate_ppm: 4000,
  edli_wage_ceiling_paise: 1000000,
  admin_rate_ppm: 3000,
  admin_minimum_paise: 0,
  rounding_mode: "nearest",
  rounding_unit_paise: 1,
};
const proposal = (version) => ({
  expected_version: version,
  kind: "pf",
  name: "Synthetic Verify PF",
  covered: true,
  effective_on: "2026-04-01",
  citation: "Synthetic verification notification",
  reference: "",
  pf,
});
const existing = (await list(`${prefix}/statutory-rules`)).find((r) => r.id === ruleID);
if (existing?.pending) {
  assert.equal(
    existing.pending.proposed_by_subject,
    proposerSubject,
    "A stranger's proposal is pending.",
  );
  await proposer(
    `${rulePath}/withdraw`,
    "POST",
    { proposal_id: existing.pending.id, note: "" },
    200,
  );
}
const version = existing?.version ?? 0;

const proposed = await proposer(rulePath, "PUT", proposal(version), 200);
const pendingID = proposed.pending.id;
assert.equal(proposed.pending.status, "pending");
assert.equal(proposed.pending.proposed_by_subject, proposerSubject);
assert.equal(
  (await proposer(rulePath, "PUT", proposal(version), 200)).pending.id,
  pendingID,
  "replay",
);
assert.equal(
  (await proposer(rulePath, "PUT", { ...proposal(version), name: "Synthetic second" }, 409)).reason,
  "proposal_pending",
);
const decision = (decision, note = "") => ({
  proposal_id: pendingID,
  expected_version: version,
  decision,
  note,
});
await proposer(`${rulePath}/decision`, "POST", decision("approve"), 403);
await approver(`${rulePath}/decision`, "POST", decision("reject"), 400);
await approver(`${rulePath}/withdraw`, "POST", { proposal_id: pendingID, note: "" }, 403);
const approved = await approver(`${rulePath}/decision`, "POST", decision("approve"), 200);
assert.equal(approved.version, version + 1);
assert.equal(approved.current.id, pendingID);
assert.equal(approved.current.decided_by_subject, approverSubject);
assert.equal(approved.pending, undefined);
assert.equal(
  (await approver(`${rulePath}/decision`, "POST", decision("approve"), 200)).version,
  version + 1,
);
assert.equal(
  (await approver(`${rulePath}/decision`, "POST", decision("reject", "Synthetic late"), 409))
    .reason,
  "not_pending",
);
assert.equal(
  (
    await proposer(
      rulePath,
      "PUT",
      {
        ...proposal(version + 1),
        effective_on: "2026-03-31",
        name: `Synthetic ${randomUUID().slice(0, 8)}`,
      },
      400,
    )
  ).error,
  "invalid_request",
  "a proposal cannot predate the revision in force",
);

// A withdrawn proposal leaves the approved revision in force.
const second = await proposer(
  rulePath,
  "PUT",
  { ...proposal(version + 1), name: "Synthetic withdrawn" },
  200,
);
const withdrawn = await proposer(
  `${rulePath}/withdraw`,
  "POST",
  { proposal_id: second.pending.id, note: "Synthetic verification" },
  200,
);
assert.equal(withdrawn.current.id, pendingID);
assert.equal(withdrawn.pending, undefined);
const history = await list(`${rulePath}/history`);
assert.equal(history[0].id, second.pending.id);
assert.equal(history[0].status, "withdrawn");
assert.equal(history[1].id, pendingID);
assert.equal(history[1].status, "approved");

// The salary fixture pays ₹30,000 of Labour Code wages: PF is capped at the synthetic ₹10,000 ceiling.
const preview = await proposer(
  `${prefix}/statutory-preview`,
  "POST",
  { structure_id: structureID, on: "2026-11-15", payable_half_days: 60, disability: false },
  200,
);
const result = preview.results.find((r) => r.kind === "pf");
assert.equal(result.status, "applied");
assert.equal(result.revision_id, pendingID);
assert.equal(result.wages_paise, 3000000);
assert.equal(result.base_paise, 1000000);
assert.deepEqual(Object.fromEntries(result.components.map((c) => [c.component, c.amount_paise])), {
  pf_employee: 100000,
  pf_employer_epf: 40000,
  pf_employer_eps: 60000,
  pf_edli: 4000,
  pf_admin: 3000,
});
assert.equal(
  preview.net_after_statutory_paise,
  preview.salary.totals.net_paise - preview.employee_paise,
);
const before = await proposer(
  `${prefix}/statutory-preview`,
  "POST",
  { structure_id: structureID, on: "2026-03-15", payable_half_days: 62, disability: false },
  409,
);
assert.equal(before.reason, "not_in_force", "the salary structure starts later");

// PT and LWF: synthetic slabs on ₹30,000 of Labour Code wages, with a gender split and a month off.
const levyRules = {
  pt: "a8888888-8888-4888-8888-888888888821",
  lwf: "a8888888-8888-4888-8888-888888888822",
};
const all = (n) => Array.from({ length: n }, (_, i) => i + 1);
const pt = {
  state_code: "IN-ZZ",
  wage_basis: "statutory_wages",
  slabs: [
    {
      months: all(11),
      genders: ["female", "male", "other"],
      from_paise: 0,
      to_paise: 2000000,
      amount_paise: 0,
    },
    { months: all(11), genders: ["female"], from_paise: 2000000, amount_paise: 100 },
    { months: all(11), genders: ["male", "other"], from_paise: 2000000, amount_paise: 200 },
  ],
};
const lwf = {
  state_code: "IN-ZZ",
  wage_basis: "statutory_wages",
  deduction_months: [6, 12],
  rounding_mode: "nearest",
  rounding_unit_paise: 1,
  slabs: [
    {
      from_paise: 0,
      to_paise: 1000000,
      employee_fixed_paise: 100,
      employee_rate_ppm: 0,
      employer_fixed_paise: 300,
      employer_rate_ppm: 0,
    },
    {
      from_paise: 1000000,
      employee_fixed_paise: 0,
      employee_rate_ppm: 1000,
      employee_cap_paise: 2000,
      employer_fixed_paise: 500,
      employer_rate_ppm: 0,
    },
  ],
};
async function approveLevy(kind, params) {
  const path = `${prefix}/statutory-rules/${levyRules[kind]}`;
  const found = (await list(`${prefix}/statutory-rules`)).find((r) => r.id === levyRules[kind]);
  if (found?.pending) {
    assert.equal(
      found.pending.proposed_by_subject,
      proposerSubject,
      "A stranger's proposal is pending.",
    );
    await proposer(`${path}/withdraw`, "POST", { proposal_id: found.pending.id, note: "" }, 200);
  }
  const at = found?.version ?? 0;
  const body = {
    expected_version: at,
    kind,
    name: `Synthetic Verify ${kind.toUpperCase()}`,
    covered: true,
    effective_on: "2026-04-01",
    citation: "Synthetic verification notification",
    reference: "",
    [kind]: params,
  };
  const gap = { ...params, slabs: params.slabs.slice(1) };
  assert.match(
    (await proposer(path, "PUT", { ...body, [kind]: gap }, 400)).message,
    /cover every wage/,
    `${kind}: a table that does not start at ₹0 is refused`,
  );
  const pending = (await proposer(path, "PUT", body, 200)).pending.id;
  const done = await approver(
    `${path}/decision`,
    "POST",
    { proposal_id: pending, expected_version: at, decision: "approve", note: "" },
    200,
  );
  assert.equal(done.current.id, pending);
  assert.deepEqual(done.current[kind], params, `${kind}: slabs round-trip in order`);
  return pending;
}
const ptRevision = await approveLevy("pt", pt);
const lwfRevision = await approveLevy("lwf", lwf);
const levies = async (on, gender) => {
  const body = await proposer(
    `${prefix}/statutory-preview`,
    "POST",
    {
      structure_id: structureID,
      on,
      payable_half_days: 60,
      disability: false,
      ...(gender && { gender }),
    },
    200,
  );
  const pick = (kind) => body.results.find((r) => r.kind === kind);
  const amounts = (r) => Object.fromEntries(r.components.map((c) => [c.component, c.amount_paise]));
  return { pt: pick("pt"), lwf: pick("lwf"), amounts, body };
};
const nov = await levies("2026-11-15");
assert.equal(nov.pt.status, "gender_required", "PT that differs by gender is not guessed");
assert.equal(nov.pt.revision_id, ptRevision);
assert.deepEqual(nov.pt.components, []);
assert.equal(nov.lwf.status, "not_due");
assert.equal(nov.lwf.revision_id, lwfRevision);
const women = await levies("2026-11-15", "female");
assert.deepEqual(women.amounts(women.pt), { pt_employee: 100 });
const men = await levies("2026-11-15", "male");
assert.deepEqual(men.amounts(men.pt), { pt_employee: 200 });
const dec = await levies("2026-12-15", "male");
assert.equal(dec.pt.status, "not_due", "a month no slab names has no PT");
assert.equal(dec.lwf.status, "applied");
assert.deepEqual(dec.amounts(dec.lwf), { lwf_employee: 2000, lwf_employer: 500 });
assert.equal(
  dec.body.net_after_statutory_paise,
  dec.body.salary.totals.net_paise - dec.body.employee_paise,
);
console.log(
  `Statutory rules verification passed: rule ${ruleID} version ${approved.version}, PT ${ptRevision}, LWF ${lwfRevision}.`,
);
