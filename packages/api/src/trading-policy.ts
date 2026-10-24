import { z } from "zod";
import { apiFetch } from "./client";
import { countryProfile } from "./country-profiles";
import { tenantTransitionPending } from "./tenant-transition";
import {
  SubmissionContract,
  ReviewContract,
  AccessChangeContract,
  SourceContract,
} from "./trading-policy-contract";

export { v7 as newCountryPolicyId } from "uuid";
export { RequirementContract, SourceContract } from "./trading-policy-contract";
export type {
  Submission as CountryPolicySubmission,
  Review as CountryPolicyReview,
  AccessChange as CountryPolicyAccessChange,
} from "./trading-policy-contract";

const versionId = z
  .string()
  .guid()
  .regex(
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    "Choose a valid version identity.",
  );
const time = z.string().datetime({ offset: true });
const scopeFields = SubmissionContract.pick({
  country: true,
  jurisdiction: true,
  category: true,
  operation: true,
  currency: true,
});

const submissionFields = SubmissionContract.extend({
  id: versionId,
  scope_id: versionId,
});
export const countryPolicySubmissionSchema = submissionFields.superRefine((value, ctx) => {
  const issue = (path: (string | number)[], message: string) =>
    ctx.addIssue({ code: "custom", path, message });
  if (countryProfile(value.country)?.currency !== value.currency)
    issue(["currency"], "Use the selected country's currency.");
  if (new Date(value.effective_until).getTime() <= new Date(value.effective_from).getTime())
    issue(["effective_until"], "End the effective window after its start.");
  if (value.jurisdiction !== value.jurisdiction.trim())
    issue(["jurisdiction"], "Remove leading and trailing spaces.");
  if (value.sources.length === 0) issue(["sources"], "Add at least one reviewed source.");
  const keys = new Set<string>();
  value.sources.forEach((source, index) => {
    if (keys.has(source.key)) issue(["sources", index, "key"], "Each source needs a unique key.");
    keys.add(source.key);
    for (const field of ["key", "title"] as const)
      if (source[field] !== source[field].trim())
        issue(["sources", index, field], "Remove leading and trailing spaces.");
    try {
      const url = new URL(source.url);
      if (
        url.protocol !== "https:" ||
        !url.hostname ||
        url.username ||
        url.password ||
        url.search ||
        url.hash
      )
        throw new Error("invalid source");
    } catch {
      issue(
        ["sources", index, "url"],
        "Use an HTTPS citation without credentials, a query or a fragment.",
      );
    }
  });
  const seen = new Set<string>();
  value.requirements.forEach((requirement, index) => {
    if (seen.has(requirement.domain))
      issue(["requirements", index, "domain"], "Record each requirement domain once.");
    seen.add(requirement.domain);
    if (requirement.reason !== requirement.reason.trim())
      issue(["requirements", index, "reason"], "Remove leading and trailing spaces.");
    if (
      !requirement.source_keys.length ||
      new Set(requirement.source_keys).size !== requirement.source_keys.length ||
      requirement.source_keys.some((key) => !keys.has(key))
    )
      issue(["requirements", index, "source_keys"], "Cite one or more distinct reviewed sources.");
  });
  if (seen.size !== 11 || value.requirements.length !== 11)
    issue(["requirements"], "Review all eleven requirement domains.");
});
function utcTimestamp(value: string) {
  const parsed = new Date(`${value}Z`);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}
const editorTime = z
  .string()
  .refine(
    (value) =>
      /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d{1,3})?)?$/.test(value) &&
      utcTimestamp(value).slice(0, 16) === value.slice(0, 16),
    "Choose a complete UTC date and time.",
  );
const editorFields = submissionFields.omit({ currency: true }).extend({
  effective_from: editorTime,
  effective_until: editorTime,
  sources: z
    .array(SourceContract.extend({ reviewed_at: editorTime }))
    .min(1)
    .max(30),
});
function editorSubmission(value: z.infer<typeof editorFields>) {
  return {
    ...value,
    currency: countryProfile(value.country)?.currency,
    effective_from: utcTimestamp(value.effective_from),
    effective_until: utcTimestamp(value.effective_until),
    sources: value.sources.map((source) => ({
      ...source,
      reviewed_at: utcTimestamp(source.reviewed_at),
    })),
  };
}
export const countryPolicyEditorSchema = editorFields.superRefine((value, ctx) => {
  const checked = countryPolicySubmissionSchema.safeParse(editorSubmission(value));
  if (!checked.success) checked.error.issues.forEach((issue) => ctx.addIssue({ ...issue }));
});
export type CountryPolicyEditorValues = z.infer<typeof countryPolicyEditorSchema>;
export function countryPolicySubmissionFromEditor(value: CountryPolicyEditorValues) {
  return countryPolicySubmissionSchema.parse(
    editorSubmission(countryPolicyEditorSchema.parse(value)),
  );
}

export const countryPolicyScopeSchema = scopeFields.extend({
  id: versionId,
  owner_subject: z.string().min(1),
  access_role: z.enum(["owner", "reader", "editor", "reviewer"]),
  status: z.enum(["unapproved", "active", "suspended"]),
  current_version: versionId.nullable(),
  revision: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
  created_at: time,
  policy_decision: z
    .object({ allowed: z.boolean(), reason: z.string(), policy_version: versionId.optional() })
    .optional(),
});
export const countryPolicyVersionSchema = SubmissionContract.pick({
  id: true,
  effective_from: true,
  effective_until: true,
  sources: true,
  requirements: true,
}).extend({
  scope: scopeFields,
  state: z.enum(["submitted", "approved", "rejected"]),
  author: z.string().min(1),
  submitted_by: z.string().min(1),
  approved_by: z.string(),
  approved_at: time,
});
export const countryPolicyVersionSummarySchema = countryPolicyVersionSchema
  .pick({ id: true, effective_from: true, effective_until: true, state: true, author: true })
  .extend({ created_at: time });
export const countryPolicyReceiptSchema = z.object({
  id: versionId,
  scope_id: versionId,
  version_id: versionId.nullable(),
  actor_subject: z.string().min(1),
  action: z.enum([
    "submitted",
    "access_granted",
    "access_revoked",
    ...ReviewContract.shape.action.options,
  ]),
  reason: z.string(),
  expected_revision: z.number().int().nonnegative(),
  result_revision: z.number().int().positive(),
  created_at: time,
});
export const countryPolicyReviewSchema = ReviewContract.extend({ id: versionId }).superRefine(
  (value, ctx) => {
    if (
      (value.action === "suspended" && value.version_id !== "") ||
      (value.action !== "suspended" && !versionId.safeParse(value.version_id).success)
    )
      ctx.addIssue({
        code: "custom",
        path: ["version_id"],
        message: "Choose the submitted version to review.",
      });
  },
);
export const countryPolicyAccessSchema = AccessChangeContract.extend({ id: versionId }).superRefine(
  (value, ctx) => {
    if ((value.revoke && value.role !== "") || (!value.revoke && !value.role))
      ctx.addIssue({
        code: "custom",
        path: ["role"],
        message: "Choose an explicit scope role when granting access.",
      });
  },
);

export type CountryPolicyScope = z.infer<typeof countryPolicyScopeSchema>;
export type CountryPolicyVersion = z.infer<typeof countryPolicyVersionSchema>;
export type CountryPolicyVersionSummary = z.infer<typeof countryPolicyVersionSummarySchema>;
export type CountryPolicyReceipt = z.infer<typeof countryPolicyReceiptSchema>;
export type CountryPolicyIdentity = { tenantId: string; subject: string };

export const COUNTRY_POLICY_ROOT = "/workspace/country-policies";
export const countryPolicyKey = (identity: CountryPolicyIdentity, ...parts: unknown[]) => [
  "country-policy",
  identity.tenantId,
  identity.subject,
  ...parts,
];
export function countryPolicyRequest(identity: CountryPolicyIdentity) {
  return {
    cache: "no-store" as const,
    headers: { "X-Helivanta-Expected-Tenant": z.string().guid().parse(identity.tenantId) },
  };
}
export function countryPolicyPath(id: string) {
  return `${COUNTRY_POLICY_ROOT}/${versionId.parse(id)}`;
}

export async function submitCountryPolicy(
  identity: CountryPolicyIdentity,
  input: z.infer<typeof countryPolicySubmissionSchema>,
  signal: AbortSignal,
) {
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return countryPolicyReceiptSchema.parse(
    await apiFetch(COUNTRY_POLICY_ROOT, {
      ...countryPolicyRequest(identity),
      method: "POST",
      body: JSON.stringify(countryPolicySubmissionSchema.parse(input)),
      signal,
    }),
  );
}
export async function reviewCountryPolicy(
  identity: CountryPolicyIdentity,
  id: string,
  input: z.infer<typeof countryPolicyReviewSchema>,
  signal: AbortSignal,
) {
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return countryPolicyReceiptSchema.parse(
    await apiFetch(`${countryPolicyPath(id)}/review`, {
      ...countryPolicyRequest(identity),
      method: "POST",
      body: JSON.stringify(countryPolicyReviewSchema.parse(input)),
      signal,
    }),
  );
}
export async function changeCountryPolicyAccess(
  identity: CountryPolicyIdentity,
  id: string,
  input: z.infer<typeof countryPolicyAccessSchema>,
  signal: AbortSignal,
) {
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return countryPolicyReceiptSchema.parse(
    await apiFetch(`${countryPolicyPath(id)}/access`, {
      ...countryPolicyRequest(identity),
      method: "POST",
      body: JSON.stringify(countryPolicyAccessSchema.parse(input)),
      signal,
    }),
  );
}
