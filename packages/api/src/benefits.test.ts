import { afterEach, expect, it, vi } from "vitest";
import {
  benefitDecisionFormSchema,
  benefitEnrollmentHistoryPath,
  benefitPlanFormSchema,
  benefitProposalFormSchema,
  decideBenefitCoverage,
  dependentFormSchema,
  dependentsPath,
  proposeBenefitCoverage,
  saveDependent,
} from "./benefits";

const hospital = "11111111-1111-4111-8111-111111111111";
const staff = "22222222-2222-4222-8222-222222222222";
const enrollment = "33333333-3333-4333-8333-333333333333";
const proposal = "44444444-4444-4444-8444-444444444444";
const plan = "55555555-5555-4555-8555-555555555555";
const relationship = "66666666-6666-4666-8666-666666666666";
const scope = { hospitalId: hospital, staffId: staff };
const stamp = "2026-10-18T00:00:00Z";
const saved = {
  id: proposal,
  enrollment_id: enrollment,
  staff_id: staff,
  plan_id: plan,
  plan_revision_id: relationship,
  base_version: 0,
  status: "pending",
  coverage_from: "2026-01-01",
  eligibility_basis: "Synthetic eligibility",
  proposed_by_subject: "synthetic-hr",
  decision_note: "",
  created_at: stamp,
};
const input = {
  expected_version: 0,
  plan_id: plan,
  coverage_from: "2026-01-01",
  eligibility_basis: "Synthetic eligibility",
};
const issues = (result: { error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
  Object.fromEntries((result.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));
afterEach(() => vi.unstubAllGlobals());

it("needs relationships exactly when a plan covers dependents and an end date to retire", () => {
  const base = {
    expected_version: 0,
    code: "syn-health",
    name: "Synthetic health",
    kind: "health",
    description: "",
    covers_dependents: true,
    covered_relationships: [],
    effective_from: "2026-01-01",
    status: "retired",
  };
  expect(issues(benefitPlanFormSchema.safeParse(base))).toEqual({
    covered_relationships: "Choose at least one relationship.",
    effective_until: "Enter the date the plan ends.",
  });
  expect(
    issues(
      benefitPlanFormSchema.safeParse({
        ...base,
        covers_dependents: false,
        covered_relationships: [relationship],
        effective_until: "2025-12-31",
      }),
    ),
  ).toEqual({
    covered_relationships: "Clear the relationships or cover dependents.",
    effective_until: "The end date must be on or after the start date.",
  });
  const ok = benefitPlanFormSchema.safeParse({
    ...base,
    covered_relationships: [relationship],
    status: "active",
  });
  expect(ok.data?.code).toBe("SYN-HEALTH");
});

it("refuses a future birth date and a dependent without a source", () => {
  const result = dependentFormSchema.safeParse({
    expected_version: 0,
    full_name: "Synthetic Child",
    relationship_revision_id: relationship,
    birth_date: "2199-01-01",
    phone: "",
    status: "active",
    reason: " ",
  });
  expect(issues(result)).toEqual({
    birth_date: "The birth date cannot be in the future.",
    reason: "Say where this comes from.",
  });
});

it("needs a basis, an ordered period and a note only to reject", () => {
  expect(
    issues(
      benefitProposalFormSchema.safeParse({
        ...input,
        coverage_until: "2025-12-31",
        eligibility_basis: " ",
      }),
    ),
  ).toEqual({
    coverage_until: "Coverage must end on or after it starts.",
    eligibility_basis: "Say what the approver should check.",
  });
  expect(benefitDecisionFormSchema.safeParse({ decision: "approve", note: "" }).success).toBe(true);
  expect(
    benefitDecisionFormSchema.safeParse({ decision: "reject", note: " " }).error?.issues[0]
      ?.message,
  ).toBe("Say why you reject it.");
});

it("builds paths only from valid identifiers", () => {
  expect(dependentsPath(scope)).toBe(`/workforce/hospitals/${hospital}/staff/${staff}/dependents`);
  expect(benefitEnrollmentHistoryPath(scope, enrollment)).toBe(
    `/workforce/hospitals/${hospital}/staff/${staff}/benefit-enrollments/${enrollment}/history`,
  );
  expect(() => dependentsPath({ hospitalId: "../x", staffId: staff })).toThrow();
});

it("proposes to the proposal id and refuses a receipt for another enrollment", async () => {
  const calls: { url: string; method?: string; body?: unknown }[] = [];
  let body: unknown = saved;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, method: init.method, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  await expect(proposeBenefitCoverage(scope, enrollment, proposal, input)).resolves.toEqual(saved);
  expect(calls[0]).toEqual({
    url: `/api/v1/workforce/hospitals/${hospital}/staff/${staff}/benefit-enrollments/${enrollment}/proposals/${proposal}`,
    method: "PUT",
    body: input,
  });
  body = { ...saved, enrollment_id: plan };
  await expect(
    decideBenefitCoverage(scope, enrollment, proposal, {
      expected_version: 0,
      decision: "approve",
      note: "",
    }),
  ).rejects.toThrow("The saved proposal did not match this enrollment.");
  expect(calls[1]?.url).toMatch(/\/proposals\/[0-9a-f-]+\/decision$/);
  body = { id: relationship, staff_id: hospital };
  await expect(
    saveDependent(scope, relationship, {
      expected_version: 0,
      full_name: "Synthetic Child",
      relationship_revision_id: relationship,
      birth_date: "2015-06-01",
      phone: "",
      status: "active",
      reason: "Synthetic form",
    }),
  ).rejects.toThrow();
});
