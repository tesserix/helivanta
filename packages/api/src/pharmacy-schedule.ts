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
  decision: "approved" | "rejected" | null;
  decision_reason: string | null;
  reviewed_by: string | null;
  reviewed_at: string | null;
};

export type PharmacyScheduleState = {
  /** The latest declaration, once approved; null while it awaits review or was rejected. */
  in_force: PharmacyScheduleDeclaration | null;
  origin: "declared_by_outlet";
  /** Why nothing is in force; empty when a class is. */
  reason: string;
  /** Newest first, at most the latest 50. */
  history: PharmacyScheduleDeclaration[];
};

export function pharmacyProductSchedulePath(scope: PharmacyScope, productId: string) {
  return `${pharmacyRoot(scope)}/catalogue/products/${encodeURIComponent(productId)}/schedule`;
}
