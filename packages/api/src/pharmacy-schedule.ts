import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/** A Drugs Rules schedule class an outlet declares for a product (#1386 A2). */
export type PharmacyScheduleClass = "none" | "G" | "H" | "H1" | "X";

/** One declaration of a product's schedule class and its review (#1636). */
export type PharmacyScheduleDeclaration = {
  id: string;
  product_id: string;
  regulatory_class: PharmacyScheduleClass;
  ndps: boolean;
  source: string;
  declared_by: string;
  created_at: string;
  /** Set by the database: less strict than the approved class, so Helivanta decides it (#1681). */
  lowers: boolean;
  /** The pharmacy owner who answered for a lowering, once countersigned. */
  countersigned_by: string | null;
  countersign_reason: string | null;
  countersigned_at: string | null;
  decision: "approved" | "rejected" | null;
  decision_reason: string | null;
  reviewed_by: string | null;
  reviewed_at: string | null;
  /** The decision was Helivanta's, not the outlet's. */
  decided_by_platform: boolean;
};

export type PharmacyScheduleState = {
  /**
   * The class products are handled under: the latest declaration once approved,
   * skipping a lowering Helivanta has not approved. Null while a newer raise
   * awaits review or was rejected.
   */
  in_force: PharmacyScheduleDeclaration | null;
  origin: "declared_by_outlet";
  /** Why the newest declaration is not in force; empty when it is. */
  reason: string;
  /** Newest first, at most the latest 50. */
  history: PharmacyScheduleDeclaration[];
};

export function pharmacyProductSchedulePath(scope: PharmacyScope, productId: string) {
  return `${pharmacyRoot(scope)}/catalogue/products/${encodeURIComponent(productId)}/schedule`;
}
