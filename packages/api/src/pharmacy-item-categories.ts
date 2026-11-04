import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/** An outlet's item category, or a subcategory of one (#988). */
export type PharmacyItemCategory = {
  id: string;
  /** Set for a subcategory: its category. */
  parent_id: string | null;
  code: string;
  name: string;
  /** Retired categories keep their products and take no new ones. */
  retired_at: string | null;
  created_by: string;
  created_at: string;
};

/** One declaration of a product's category and its review (#988). */
export type PharmacyProductCategoryDeclaration = {
  id: string;
  category_id: string;
  subcategory_id: string | null;
  effective_from: string;
  reason: string;
  declared_by: string;
  created_at: string;
  decision: "approved" | "rejected" | null;
  decision_reason: string | null;
  reviewed_by: string | null;
  /** When an approval takes effect: never before the day it was approved. */
  in_force_from: string | null;
};

export type PharmacyProductCategory = {
  in_force: {
    declaration_id: string;
    category_id: string;
    subcategory_id: string | null;
    in_force_from: string;
  } | null;
  history: PharmacyProductCategoryDeclaration[];
};

export function pharmacyItemCategoriesPath(scope: PharmacyScope) {
  return `${pharmacyRoot(scope)}/catalogue/categories`;
}

export function pharmacyProductCategoryPath(scope: PharmacyScope, productId: string) {
  return `${pharmacyRoot(scope)}/catalogue/products/${encodeURIComponent(productId)}/category`;
}
