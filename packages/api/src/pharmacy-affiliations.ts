import { z } from "zod";

// A pharmacy's link to a hospital is verified by that hospital (#1416).
export type AffiliationVerificationStatus =
  "pending_validation" | "requested" | "verified" | "declined" | "revoked" | "withdrawn";

/** One step of an affiliation's verification history, oldest first by sequence. */
export type AffiliationVerificationStep = {
  id: string;
  affiliation_id: string;
  sequence: number;
  kind: "requested" | "accepted" | "declined" | "revoked" | "withdrawn";
  request_id: string;
  decision_id: string | null;
  actor_subject: string | null;
  reason: string | null;
  created_at: string;
};

const reason = z
  .string()
  .trim()
  .min(1, "Give a reason")
  .max(500, "Keep the reason under 500 characters");

export const affiliationWithdrawSchema = z.object({ reason });

/** The hospital's queue of pharmacy requests, in patientmaster. */
export type PharmacyAffiliationReview = {
  request_id: string;
  affiliation_id: string;
  pharmacy_id: string;
  pharmacy_name: string;
  hospital_id: string;
  relationship: "owner" | "serving" | "partner";
  status: "pending" | "accepted" | "declined" | "revoked" | "withdrawn";
  created_at: string;
  updated_at: string;
  decisions: {
    id: string;
    outcome: "accepted" | "declined" | "revoked";
    reason: string;
    reviewer_subject: string;
    created_at: string;
  }[];
};

export const pharmacyAffiliationDecisionSchema = z.object({
  outcome: z.enum(["accepted", "declined", "revoked"]),
  reason,
});

/** A pharmacist the hospital lets look up its patients for one pharmacy (#1416b). */
export type PharmacyLookupGrant = {
  subject: string;
  pharmacy_id: string;
  granted_by: string;
  created_at: string;
};

export const pharmacyLookupGrantSchema = z.object({
  subject: z.string().trim().min(1, "Choose a member").max(200),
});
