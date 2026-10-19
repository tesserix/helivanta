import { vi } from "vitest";

// Synthetic values only: these prove behaviour, never a real policy or person.
export const hospital = "11111111-1111-4111-8111-111111111111";
export const staffID = "22222222-2222-4222-8222-222222222222";
export const spouseMaster = "33333333-3333-4333-8333-333333333333";
export const spouseRevision = "33333333-3333-4333-8333-333333333334";
export const childMaster = "44444444-4444-4444-8444-444444444444";
export const childRevision = "44444444-4444-4444-8444-444444444445";
export const parentMaster = "55555555-5555-4555-8555-555555555555";
export const healthID = "66666666-6666-4666-8666-666666666666";
export const lifeID = "77777777-7777-4777-8777-777777777777";
export const dependentID = "88888888-8888-4888-8888-888888888888";
export const enrollmentID = "99999999-9999-4999-8999-999999999999";
export const proposalID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
export const revisionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
export const stamp = "2026-10-19T00:00:00Z";
export const VIEW = "workforce.benefits.view";
export const MANAGE = "workforce.benefits.manage";
export const APPROVE = "workforce.benefits.approve";

const page = (data: unknown[], more = false) => ({
  data,
  page: { has_more: more, next_cursor: more ? "next" : null },
});
const master = (id: string, revision: string, code: string, name: string, active = true) => ({
  id,
  revision_id: revision,
  kind: "relationship",
  code,
  name,
  description: "",
  active,
  version: 1,
  created_at: stamp,
  updated_at: stamp,
});
export const relationships = [
  master(spouseMaster, spouseRevision, "SPOUSE", "Synthetic Spouse"),
  master(childMaster, childRevision, "CHILD", "Synthetic Child"),
  master(parentMaster, "55555555-5555-4555-8555-555555555556", "PARENT", "Synthetic Parent", false),
];
export const member = {
  id: staffID,
  display_name: "Synthetic Nurse",
  employee_code: "SYN-1",
  employment_status: "active",
};
export const healthPlan = {
  id: healthID,
  code: "SYN-HEALTH",
  name: "Synthetic Health",
  kind: "health",
  description: "Synthetic cover for tests",
  covers_dependents: true,
  covered_relationships: [spouseMaster],
  effective_from: "2026-01-01",
  status: "active",
  version: 2,
  revision_id: revisionID,
  created_at: stamp,
  updated_at: stamp,
};
export const lifePlan = {
  ...healthPlan,
  id: lifeID,
  code: "SYN-LIFE",
  name: "Synthetic Life",
  kind: "life",
  description: "",
  covers_dependents: false,
  covered_relationships: [],
  effective_until: "2026-06-30",
  status: "retired",
  version: 3,
};
export const spouse = {
  id: dependentID,
  staff_id: staffID,
  full_name: "Synthetic Partner",
  relationship_revision_id: spouseRevision,
  relationship_master_id: spouseMaster,
  relationship_name: "Synthetic Spouse",
  birth_date: "1990-05-01",
  phone: "",
  status: "active",
  version: 1,
  revision_id: revisionID,
  created_at: stamp,
  updated_at: stamp,
};
export const childDependentID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
// An older spouse revision: the master was renamed after this dependent was recorded.
export const recordedSpouseRevision = "33333333-3333-4333-8333-333333333335";
export const child = {
  ...spouse,
  id: childDependentID,
  full_name: "Synthetic Kid",
  relationship_revision_id: childRevision,
  relationship_master_id: childMaster,
  relationship_name: "Synthetic Child",
  birth_date: "2020-01-01",
};
export const enrollment = (over: object = {}) => ({
  id: enrollmentID,
  staff_id: staffID,
  plan_id: healthID,
  dependent_id: dependentID,
  created_at: stamp,
  ...over,
});
export const proposal = (over: object = {}) => ({
  id: proposalID,
  enrollment_id: enrollmentID,
  staff_id: staffID,
  plan_id: healthID,
  plan_revision_id: revisionID,
  base_version: 0,
  status: "pending",
  coverage_from: "2026-04-01",
  eligibility_basis: "Synthetic declaration form",
  proposed_by_subject: "synthetic-maker",
  decision_note: "",
  created_at: stamp,
  ...over,
});
export const coverage = {
  original_plan_revision_id: revisionID,
  coverage_from: "2026-02-01",
  version: 1,
  proposal_id: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
  updated_at: stamp,
};

export type Write = { method: string; url: string; body: Record<string, unknown> };
type Reply = { status: number; body: unknown };

const last = (url: string, from = 1) => url.split("/").at(-from)!;

// Echoes a write as the record the API would return, so the client's receipt checks pass.
function echo(url: string, body: Record<string, unknown>): unknown {
  if (url.includes("/hr/benefits/plans/")) {
    const id = last(url);
    const prior = [healthPlan, lifePlan].find((p) => p.id === id);
    const { expected_version, ...rest } = body;
    return {
      ...rest,
      id,
      version: Number(expected_version) + 1,
      revision_id: revisionID,
      created_at: prior?.created_at ?? stamp,
      updated_at: stamp,
    };
  }
  if (url.includes("/dependents/")) {
    const { expected_version, full_name, relationship_revision_id, birth_date, phone, status } =
      body;
    const name = relationships.find((r) => r.revision_id === relationship_revision_id);
    return {
      full_name,
      relationship_revision_id,
      birth_date,
      phone,
      status,
      id: last(url),
      staff_id: staffID,
      relationship_master_id: name?.id ?? spouseMaster,
      relationship_name: name?.name ?? "Synthetic Spouse",
      version: Number(expected_version) + 1,
      revision_id: revisionID,
      created_at: stamp,
      updated_at: stamp,
    };
  }
  const decided = url.endsWith("/decision") || url.endsWith("/withdraw");
  const proposalPart = decided ? last(url, 2) : last(url);
  const enrollment = url.split("/benefit-enrollments/")[1]!.split("/")[0]!;
  if (decided)
    return proposal({
      id: proposalPart,
      enrollment_id: enrollment,
      status: url.endsWith("/withdraw")
        ? "withdrawn"
        : body.decision === "approve"
          ? "approved"
          : "rejected",
      decided_by_subject: "synthetic-checker",
      decided_at: stamp,
      decision_note: body.note,
    });
  const { expected_version, ...rest } = body;
  return proposal({
    ...rest,
    id: proposalPart,
    enrollment_id: enrollment,
    base_version: expected_version,
  });
}

export function mockBenefits({
  permissions = [VIEW, MANAGE],
  subject = "synthetic-maker",
  access = "manager",
  plans = [healthPlan, lifePlan] as unknown[],
  dependents = [spouse] as unknown[],
  enrollments = [] as unknown[],
  history = [] as unknown[],
  staff = member,
  fail = "",
  replies = [] as Reply[],
  secondPage = {},
}: {
  permissions?: string[];
  subject?: string;
  access?: string;
  plans?: unknown[];
  dependents?: unknown[];
  enrollments?: unknown[];
  history?: unknown[];
  staff?: object;
  fail?: "" | "plans" | "dependents" | "enrollments";
  replies?: Reply[];
  // Served after a first page that reports more.
  secondPage?: { plans?: unknown[]; dependents?: unknown[] };
} = {}) {
  const writes: Write[] = [];
  const reads: string[] = [];
  const queue = [...replies];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (requested: string, init?: RequestInit) => {
      const url = requested.replace(/\?cursor=next$/, "");
      const listed = (data: unknown[], next?: unknown[]) =>
        url === requested ? page(data, !!next) : page(next ?? []);
      let reply: Reply = { status: 200, body: page([]) };
      if (init?.method === "PUT" || init?.method === "POST") {
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        writes.push({ method: init.method, url, body });
        reply = queue.shift() ?? { status: 200, body: echo(url, body) };
        return new Response(JSON.stringify(reply.body), { status: reply.status });
      }
      reads.push(requested);
      const failing = (name: typeof fail) =>
        fail === name && { status: 500, body: { error: "internal", message: "boom" } };
      if (url.includes("permissions"))
        reply.body = {
          data: permissions,
          subject,
          tenant_id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
        };
      else if (url.endsWith("/workforce/hospitals"))
        reply.body = page([{ id: hospital, name: "Synthetic Hospital", access_level: access }]);
      else if (url.endsWith(`/hospitals/${hospital}/staff`)) reply.body = page([staff]);
      else if (url.endsWith("/hr/masters?kind=relationship")) reply.body = page(relationships);
      else if (url.endsWith("/history")) reply.body = page(history);
      else if (url.endsWith("/hr/benefits/plans"))
        reply = failing("plans") || { status: 200, body: listed(plans, secondPage.plans) };
      else if (url.endsWith(`/staff/${staffID}/dependents`))
        reply = failing("dependents") || {
          status: 200,
          body: listed(dependents, secondPage.dependents),
        };
      else if (url.endsWith(`/staff/${staffID}/benefit-enrollments`))
        reply = failing("enrollments") || { status: 200, body: page(enrollments) };
      return new Response(JSON.stringify(reply.body), { status: reply.status });
    }),
  );
  return { writes, reads };
}

export const conflict = (reason: string, message: string): Reply => ({
  status: 409,
  body: { error: "conflict", message, reason },
});
