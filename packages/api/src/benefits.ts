import { z } from "zod";
import { apiFetch } from "./client";
import { internationalPhone } from "./enrollment-fields";
import { evidenceClient, type EvidenceScope } from "./workforce-evidence";
import {
  benefitDecisionInputContract,
  benefitEvidenceInputContract,
  benefitPlanContract,
  benefitPlanInputContract,
  benefitProposalContract,
  benefitProposalInputContract,
  benefitWithdrawInputContract,
  dependentContract,
  dependentInputContract,
  type BenefitDecisionInput,
  type BenefitPlan,
  type BenefitPlanInput,
  type BenefitProposal,
  type BenefitProposalInput,
  type BenefitWithdrawInput,
  type Dependent,
  type DependentInput,
} from "./benefits-contract";
export * from "./benefits-contract";

export type BenefitScope = { hospitalId: string; staffId: string };
const identifier = benefitPlanContract.shape.id;

const hospitalPath = (hospitalId: string) => `/workforce/hospitals/${identifier.parse(hospitalId)}`;
export const benefitPlansPath = (hospitalId: string) =>
  `${hospitalPath(hospitalId)}/hr/benefits/plans`;
export const benefitPlanHistoryPath = (hospitalId: string, plan: string) =>
  `${benefitPlansPath(hospitalId)}/${identifier.parse(plan)}/history`;
export const benefitPlansKey = (hospitalId: string) => ["workforce", "benefit-plans", hospitalId];
export const benefitCoveragePath = (hospitalId: string) =>
  `${hospitalPath(hospitalId)}/hr/benefits/coverage.csv`;

const staffPath = (scope: BenefitScope) =>
  `${hospitalPath(scope.hospitalId)}/staff/${identifier.parse(scope.staffId)}`;
export const dependentsPath = (scope: BenefitScope) => `${staffPath(scope)}/dependents`;
export const dependentHistoryPath = (scope: BenefitScope, dependent: string) =>
  `${dependentsPath(scope)}/${identifier.parse(dependent)}/history`;
export const dependentsKey = (scope: BenefitScope) => [
  "workforce",
  "dependents",
  scope.hospitalId,
  scope.staffId,
];
export const benefitEnrollmentsPath = (scope: BenefitScope) =>
  `${staffPath(scope)}/benefit-enrollments`;
export const benefitEnrollmentHistoryPath = (scope: BenefitScope, enrollment: string) =>
  `${benefitEnrollmentsPath(scope)}/${identifier.parse(enrollment)}/history`;
export const benefitEnrollmentsKey = (scope: BenefitScope) => [
  "workforce",
  "benefit-enrollments",
  scope.hospitalId,
  scope.staffId,
];
const proposalPath = (scope: BenefitScope, enrollment: string, proposal: string) =>
  `${benefitEnrollmentsPath(scope)}/${identifier.parse(enrollment)}/proposals/${identifier.parse(proposal)}`;

export const benefitEvidence = evidenceClient({
  key: "benefit-evidence",
  recordKind: "benefit_proposal",
  category: "benefit_eligibility_evidence",
  input: benefitEvidenceInputContract,
});
export const benefitEvidenceScope = (
  scope: BenefitScope,
  enrollment: string,
  proposal: string,
): EvidenceScope => ({
  recordPath: proposalPath(scope, enrollment, proposal),
  staffId: scope.staffId,
  recordId: proposal,
});

// The form adds what the contract leaves to the server: an ordered period and relationships only when dependents are covered.
export const benefitPlanFormSchema = benefitPlanInputContract.superRefine((value, ctx) => {
  if (value.effective_until !== undefined && value.effective_until < value.effective_from)
    ctx.addIssue({
      code: "custom",
      path: ["effective_until"],
      message: "The end date must be on or after the start date.",
    });
  if (value.status === "retired" && value.effective_until === undefined)
    ctx.addIssue({
      code: "custom",
      path: ["effective_until"],
      message: "Enter the date the plan ends.",
    });
  if (value.covers_dependents && value.covered_relationships.length === 0)
    ctx.addIssue({
      code: "custom",
      path: ["covered_relationships"],
      message: "Choose at least one relationship.",
    });
  if (!value.covers_dependents && value.covered_relationships.length > 0)
    ctx.addIssue({
      code: "custom",
      path: ["covered_relationships"],
      message: "Clear the relationships or cover dependents.",
    });
  if (new Set(value.covered_relationships).size !== value.covered_relationships.length)
    ctx.addIssue({
      code: "custom",
      path: ["covered_relationships"],
      message: "List each relationship once.",
    });
});
export type BenefitPlanForm = z.infer<typeof benefitPlanFormSchema>;

export const dependentFormSchema = dependentInputContract
  .extend({
    phone: internationalPhone,
    reason: z
      .string()
      .trim()
      .min(1, "Say where this comes from.")
      .max(500, "Keep it to 500 characters."),
  })
  .superRefine((value, ctx) => {
    // Matches the server: no later than tomorrow in UTC, so no time zone rejects a newborn.
    if (value.birth_date > new Date(Date.now() + 86_400_000).toISOString().slice(0, 10))
      ctx.addIssue({
        code: "custom",
        path: ["birth_date"],
        message: "The birth date cannot be in the future.",
      });
  });
export type DependentForm = z.infer<typeof dependentFormSchema>;

export const benefitProposalFormSchema = benefitProposalInputContract
  .extend({
    eligibility_basis: z
      .string()
      .trim()
      .min(1, "Say what the approver should check.")
      .max(500, "Keep it to 500 characters."),
  })
  .superRefine((value, ctx) => {
    if (value.coverage_until !== undefined && value.coverage_until < value.coverage_from)
      ctx.addIssue({
        code: "custom",
        path: ["coverage_until"],
        message: "Coverage must end on or after it starts.",
      });
  });
export type BenefitProposalForm = z.infer<typeof benefitProposalFormSchema>;

export const benefitDecisionFormSchema = z
  .object({ decision: z.enum(["approve", "reject"]), note: z.string().max(500) })
  .superRefine((value, ctx) => {
    if (value.decision === "reject" && !value.note.trim())
      ctx.addIssue({ code: "custom", path: ["note"], message: "Say why you reject it." });
  });
export type BenefitDecisionForm = z.infer<typeof benefitDecisionFormSchema>;

export async function saveBenefitPlan(
  hospitalId: string,
  plan: string,
  input: BenefitPlanInput,
): Promise<BenefitPlan> {
  const body = benefitPlanInputContract.parse(input);
  const saved = benefitPlanContract.parse(
    await apiFetch(`${benefitPlansPath(hospitalId)}/${identifier.parse(plan)}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (saved.id !== plan) throw new Error("The saved plan did not match.");
  return saved;
}

export async function saveDependent(
  scope: BenefitScope,
  dependent: string,
  input: DependentInput,
): Promise<Dependent> {
  const body = dependentInputContract.parse(input);
  const saved = dependentContract.parse(
    await apiFetch(`${dependentsPath(scope)}/${identifier.parse(dependent)}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  if (saved.id !== dependent || saved.staff_id !== scope.staffId)
    throw new Error("The saved dependent did not match this staff member.");
  return saved;
}

function owned(
  scope: BenefitScope,
  enrollment: string,
  proposal: string,
  value: unknown,
): BenefitProposal {
  const saved = benefitProposalContract.parse(value);
  if (
    saved.id !== proposal ||
    saved.staff_id !== scope.staffId ||
    saved.enrollment_id !== enrollment
  )
    throw new Error("The saved proposal did not match this enrollment.");
  return saved;
}

export async function proposeBenefitCoverage(
  scope: BenefitScope,
  enrollment: string,
  proposal: string,
  input: BenefitProposalInput,
): Promise<BenefitProposal> {
  const body = benefitProposalInputContract.parse(input);
  return owned(
    scope,
    enrollment,
    proposal,
    await apiFetch(proposalPath(scope, enrollment, proposal), {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}

export async function decideBenefitCoverage(
  scope: BenefitScope,
  enrollment: string,
  proposal: string,
  input: BenefitDecisionInput,
): Promise<BenefitProposal> {
  const body = benefitDecisionInputContract.parse(input);
  return owned(
    scope,
    enrollment,
    proposal,
    await apiFetch(`${proposalPath(scope, enrollment, proposal)}/decision`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  );
}

export async function withdrawBenefitCoverage(
  scope: BenefitScope,
  enrollment: string,
  proposal: string,
  input: BenefitWithdrawInput,
): Promise<BenefitProposal> {
  const body = benefitWithdrawInputContract.parse(input);
  return owned(
    scope,
    enrollment,
    proposal,
    await apiFetch(`${proposalPath(scope, enrollment, proposal)}/withdraw`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  );
}
