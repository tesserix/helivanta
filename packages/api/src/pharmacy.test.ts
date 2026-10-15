import { expect, it } from "vitest";
import {
  pharmacyProductFormSchema,
  pharmacyMovementInputSchema,
  pharmacyBatchInputSchema,
  pharmacyKey,
  pharmacyRoot,
  newPharmacyId,
} from "./pharmacy";
const product = {
  expected_version: 0,
  sku: "TEST-ONE",
  brand_name: "TEST Product",
  generic_name: "TEST Molecule",
  dosage_form: "capsule",
  route: "oral",
  manufacturer: "TEST Manufacturer",
  marketing_company: "",
  ingredients: [{ name: "TEST A", strength: "500", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "capsule",
  currency: "INR",
  mrp_minor: 12500,
  status: "active",
};
it("rejects regulatory overrides and unsafe or duplicated composition", () => {
  expect(pharmacyProductFormSchema.safeParse(product).success).toBe(true);
  expect(pharmacyProductFormSchema.safeParse({ ...product, schedule: "OTC" }).success).toBe(false);
  expect(
    pharmacyProductFormSchema.safeParse({
      ...product,
      ingredients: [{ name: "TEST A", strength: "0.000", unit: "mg" }],
    }).success,
  ).toBe(false);
  expect(
    pharmacyProductFormSchema.safeParse({
      ...product,
      ingredients: [...product.ingredients, { name: "test a", strength: "5", unit: "mg" }],
    }).success,
  ).toBe(false);
  expect(pharmacyProductFormSchema.safeParse({ ...product, mrp_minor: 0.1 }).success).toBe(false);
});
it("requires explicit whole-unit movement and exact lot-date identity", () => {
  const id = newPharmacyId();
  const movement = {
    location_id: id,
    batch_id: newPharmacyId(),
    kind: "receipt",
    quantity_units: 1,
    reason: "TEST",
    source_reference: "TEST-GRN-L1",
  };
  expect(pharmacyMovementInputSchema.safeParse(movement).success).toBe(true);
  expect(pharmacyMovementInputSchema.safeParse({ ...movement, quantity_units: 0 }).success).toBe(
    false,
  );
  expect(pharmacyMovementInputSchema.safeParse({ ...movement, quantity_units: 1.2 }).success).toBe(
    false,
  );
  expect(
    pharmacyMovementInputSchema.safeParse({ ...movement, batch_id: crypto.randomUUID() }).success,
  ).toBe(false);
  expect(
    pharmacyBatchInputSchema.safeParse({
      product_id: id,
      lot_number: "TEST-L1",
      expires_on: "2030-02-30",
    }).success,
  ).toBe(false);
  expect(
    pharmacyBatchInputSchema.safeParse({
      product_id: id,
      lot_number: "TEST-L1",
      expires_on: "2030-10",
    }).success,
  ).toBe(false);
});
it("partitions query keys by tenant and outlet and mints retry-safe v7 identities", () => {
  const tenantId = "11111111-1111-1111-1111-111111111111";
  const outletId = newPharmacyId();
  const pharmacyId = newPharmacyId();
  const scope = { tenantId, pharmacyId, outletId };
  expect(pharmacyKey(scope, "stock")).not.toEqual(
    pharmacyKey({ ...scope, tenantId: newPharmacyId() }, "stock"),
  );
  expect(pharmacyKey(scope, "stock")).not.toEqual(
    pharmacyKey({ ...scope, outletId: newPharmacyId() }, "stock"),
  );
  expect(pharmacyRoot(scope)).toBe(`/pharmacy/pharmacies/${pharmacyId}/outlets/${outletId}`);
  expect(() => pharmacyRoot({ ...scope, outletId: "../another-outlet" })).toThrow();
  expect(new Set(Array.from({ length: 100 }, () => newPharmacyId())).size).toBe(100);
});
