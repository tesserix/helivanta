import { z } from "zod";
import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";
import { pharmacyRoot, type PharmacyScope } from "./pharmacy";
import {
  PrescriptionFulfilmentReviewInputContract,
  PrescriptionFulfilmentReviewContextContract,
  type PrescriptionFulfilmentReviewInput,
} from "./pharmacy-verification-contract";

export type {
  PrescriptionReviewContext,
  PrescriptionFulfilmentReviewInput,
  PrescriptionFulfilmentReviewContext,
} from "./pharmacy-verification-contract";
export { PrescriptionFulfilmentReviewInputContract } from "./pharmacy-verification-contract";
const identifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
export async function readPrescriptionFulfilmentReview(
  scope: PharmacyScope & AccountRequestScope,
  prescriptionId: string,
  input: PrescriptionFulfilmentReviewInput,
  signal?: AbortSignal,
) {
  const id = identifier.parse(prescriptionId);
  const intent = PrescriptionFulfilmentReviewInputContract.parse(input);
  const view = PrescriptionFulfilmentReviewContextContract.parse(
    await accountScopedRequest(
      scope,
      `${pharmacyRoot(scope)}/prescriptions/${id}/fulfilment-review-context`,
      signal,
      { method: "POST", body: intent },
    ),
  );
  const source = view.source;
  const requested = [...intent.lines].sort((a, b) => a.line_number - b.line_number);
  if (
    source.owner_tenant_id !== scope.tenantId.toLowerCase() ||
    source.reader_subject !== scope.subject ||
    source.pharmacy_id !== scope.pharmacyId ||
    source.record.outlet_id !== scope.outletId ||
    source.record.id !== id ||
    source.source_state_hash !== intent.source_state_hash ||
    view.lines.length !== requested.length ||
    view.lines.some((line, index) => {
      const expected = requested[index];
      const recorded = source.record.lines.find((row) => row.line_number === line.line_number);
      return (
        !expected ||
        line.line_number !== expected.line_number ||
        line.quantity_units !== expected.quantity_units ||
        !recorded ||
        line.product_id !== recorded.product_id ||
        line.quantity_units > recorded.units_remaining ||
        recorded.dispensings_remaining < 1
      );
    })
  ) {
    throw new Error(
      "The proposed fulfilment could not be matched to this prescription and account. Refresh recorded details before continuing.",
    );
  }
  return view;
}
