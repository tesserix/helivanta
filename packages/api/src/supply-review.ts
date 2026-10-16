import { z } from "zod";
import { apiFetch } from "./client";
import { supplyReviewInputSchema } from "./supply-contract";
import {
  supplyEvidenceHeaders,
  supplyEvidenceRoot,
  type SupplyEvidenceScope,
} from "./supply-evidence";
import { supplyRegistryKey } from "./supply-registration";

const id = supplyReviewInputSchema.shape.evidence_id;
export const supplyReviewFormSchema = supplyReviewInputSchema.superRefine((value, ctx) => {
  const decision = value.action === "verify" || value.action === "reject";
  if (decision && value.method === "not_applicable")
    ctx.addIssue({
      code: "custom",
      path: ["method"],
      message: "Choose an independent verification method.",
    });
  if (decision && !value.reference.trim())
    ctx.addIssue({
      code: "custom",
      path: ["reference"],
      message: "Record the authority-check reference.",
    });
  if (!decision && (value.method !== "not_applicable" || value.reference !== ""))
    ctx.addIssue({
      code: "custom",
      path: ["method"],
      message: "Only review decisions use an authority-check method.",
    });
  for (const field of ["reason", "reference"] as const)
    if (/\p{Cc}/u.test(value[field]))
      ctx.addIssue({
        code: "custom",
        path: [field],
        message: "Use plain text without control characters.",
      });
});
export const supplyReviewReceiptSchema = supplyReviewInputSchema
  .omit({
    expected_version: true,
    expected_party_version: true,
    expected_premise_version: true,
    expected_license_version: true,
  })
  .extend({
    id,
    party_id: id,
    license_id: id,
    premise_id: id,
    version: z.number().int().min(1).max(Number.MAX_SAFE_INTEGER),
    party_version: z.number().int().positive(),
    premise_version: z.number().int().positive(),
    license_version: z.number().int().positive(),
    status: z.enum(["submitted", "verified", "rejected", "suspended"]),
    actor_subject: z.string().min(1),
    created_at: z.string().datetime(),
    updated_at: z.string().datetime(),
  });
export const supplyReviewStateSchema = z.object({
  latest: supplyReviewReceiptSchema.nullable(),
  effective_status: z.enum([
    "not_submitted",
    "submitted",
    "verified",
    "rejected",
    "suspended",
    "review_required",
    "evidence_unavailable",
    "premise_inactive",
    "not_yet_valid",
    "expired",
  ]),
  trading_approved: z.literal(false),
  party_version: z.number().int().positive(),
  premise_version: z.number().int().positive(),
  license_version: z.number().int().positive(),
  allowed_actions: z.array(supplyReviewInputSchema.shape.action),
});
export type SupplyReviewInput = z.infer<typeof supplyReviewInputSchema>;
export type SupplyReviewReceipt = z.infer<typeof supplyReviewReceiptSchema>;
export type SupplyReviewState = z.infer<typeof supplyReviewStateSchema>;
export const supplyReviewKey = (scope: SupplyEvidenceScope) =>
  supplyRegistryKey(scope, "licence-review", scope.licenseId);
export const supplyReviewRoot = (scope: SupplyEvidenceScope) =>
  supplyEvidenceRoot(scope).replace(/\/evidence$/, "/reviews");
export function supplyReviewHeaders(scope: SupplyEvidenceScope) {
  const headers = supplyEvidenceHeaders(scope);
  if (!scope.subject || scope.subject.length > 200)
    throw new Error("Refresh your signed-in workspace before reviewing a licence.");
  headers.set("X-Helivanta-Expected-Subject", scope.subject);
  return headers;
}
export function supplyReviewReceipt(scope: SupplyEvidenceScope, value: unknown) {
  const saved = supplyReviewReceiptSchema.parse(value);
  if (saved.party_id !== scope.partyId || saved.license_id !== scope.licenseId)
    throw new Error("Review ownership does not match this workspace.");
  return saved;
}
export async function recordSupplyReview(
  scope: SupplyEvidenceScope,
  reviewId: string,
  input: SupplyReviewInput,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const requested = supplyReviewFormSchema.parse(input);
  const saved = supplyReviewReceipt(
    scope,
    await apiFetch(`${supplyReviewRoot(scope)}/${id.parse(reviewId)}`, {
      method: "PUT",
      headers: supplyReviewHeaders(scope),
      body: JSON.stringify(requested),
      signal,
    }),
  );
  if (
    saved.id !== reviewId ||
    saved.version !== requested.expected_version + 1 ||
    saved.action !== requested.action ||
    saved.evidence_id !== requested.evidence_id ||
    saved.party_version !== requested.expected_party_version ||
    saved.premise_version !== requested.expected_premise_version ||
    saved.license_version !== requested.expected_license_version ||
    saved.actor_subject !== scope.subject ||
    saved.reason !== requested.reason ||
    saved.method !== requested.method ||
    saved.reference !== requested.reference
  )
    throw new Error("The saved decision does not match this review request.");
  return saved;
}
