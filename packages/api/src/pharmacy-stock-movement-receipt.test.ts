import { expect, it } from "vitest";
import type { z } from "zod";
import * as contract from "./pharmacy-contract";

it.each([
  "receipt",
  "correction",
  "release",
  "sale",
  "return",
  "supplier_dispatch",
  "quarantine_hold",
  "supplier_return",
])("decodes %s from the stock ledger while preserving command authority", (kind) => {
  const schema = Reflect.get(contract, "pharmacyStockMovementReceiptSchema") as
    z.ZodType | undefined;
  const receipt = {
    id: "0199e000-0000-7000-8000-000000000001",
    outlet_id: "0199e000-0000-7000-8000-000000000002",
    location_id: "0199e000-0000-7000-8000-000000000003",
    batch_id: "0199e000-0000-7000-8000-000000000004",
    kind,
    quantity_units: 4,
    reason: "TEST recorded stock fact",
    source_reference: "TEST commercial evidence",
    actor_subject: "TEST-owner",
    created_at: "2026-11-20T12:00:00Z",
  };
  expect(schema?.safeParse(receipt)).toMatchObject({ success: true, data: receipt });
  if (kind !== "receipt" && kind !== "correction") {
    const command = {
      location_id: receipt.location_id,
      batch_id: receipt.batch_id,
      kind: receipt.kind,
      quantity_units: receipt.quantity_units,
      reason: receipt.reason,
      source_reference: receipt.source_reference,
    };
    expect(contract.pharmacyMovementInputSchema.safeParse(command).success).toBe(false);
  }
});
