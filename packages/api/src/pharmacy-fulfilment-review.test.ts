import { beforeEach, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import { readPrescriptionFulfilmentReview } from "./pharmacy-fulfilment-review";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const request = vi.mocked(apiFetch);
const id = "0199e000-0000-7000-8000-000000000001";
const scope = {
  tenantId: "11111111-1111-7111-8111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000004",
  outletId: "0199e000-0000-7000-8000-000000000002",
  subject: "synthetic-preparer",
};
const input = { source_state_hash: "a".repeat(64), lines: [{ line_number: 1, quantity_units: 5 }] };
const receipt = () => ({
  source: {
    owner_tenant_id: scope.tenantId,
    pharmacy_id: scope.pharmacyId,
    reader_subject: scope.subject,
    source_schema_version: 1,
    source_kind: "recorded_prescription",
    source_authenticity: "not_evaluated",
    source_state_hash: input.source_state_hash,
    clinical_authority: "not_evaluated",
    clinical_context: {
      allergies: "unavailable",
      active_medications: "unavailable",
      patient_age: "unavailable",
      patient_weight: "unavailable",
      clinical_flags: "unavailable",
    },
    record: {
      id,
      outlet_id: scope.outletId,
      prescriber_name: "Synthetic Prescriber",
      prescriber_registration: "QA/123",
      prescriber_council: "Synthetic council",
      prescribed_on: "2026-11-25",
      patient_name: "Synthetic Patient",
      patient_address: null,
      prescriber_address: null,
      recorded_by: "synthetic-recorder",
      created_at: "2026-11-26T00:00:00Z",
      lines: [
        {
          line_number: 1,
          prescribed_as: "Synthetic medicine",
          product_id: id,
          quantity_units: 20,
          dispensings_allowed: 1,
          units_remaining: 20,
          dispensings_remaining: 1,
        },
      ],
    },
  },
  lines: [{ line_number: 1, product_id: id, quantity_units: 5 }],
  fulfilment_state_hash: "b".repeat(64),
  state: "proposed",
  stock_reservation: "not_reserved",
  clinical_authority: "not_evaluated",
});
beforeEach(() => request.mockReset());
it("binds the proposed fill to current account, source and selected quantities", async () => {
  request.mockResolvedValue(receipt());
  await expect(readPrescriptionFulfilmentReview(scope, id, input)).resolves.toMatchObject({
    state: "proposed",
    clinical_authority: "not_evaluated",
  });
  const [path, init] = request.mock.calls[0]!;
  expect(path).toContain(`/prescriptions/${id}/fulfilment-review-context`);
  expect(init?.method).toBe("POST");
  expect(JSON.parse(init?.body as string)).toEqual(input);
  const headers = new Headers(init?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each(["owner_tenant_id", "reader_subject", "pharmacy_id", "source_state_hash"])(
  "rejects changed source coordinate %s",
  async (field) => {
    const view = receipt();
    view.source = {
      ...view.source,
      [field]:
        field === "reader_subject"
          ? "another-reader"
          : field === "source_state_hash"
            ? "c".repeat(64)
            : scope.outletId,
    };
    request.mockResolvedValue(view);
    await expect(readPrescriptionFulfilmentReview(scope, id, input)).rejects.toThrow();
  },
);
it.each([
  { line_number: 1, quantity_units: 6, product_id: id },
  { line_number: 2, quantity_units: 5, product_id: id },
  { line_number: 1, quantity_units: 5, product_id: scope.pharmacyId },
])("rejects different fulfilment %j", async (line) => {
  request.mockResolvedValue({ ...receipt(), lines: [line] });
  await expect(readPrescriptionFulfilmentReview(scope, id, input)).rejects.toThrow();
});
it("rejects a response asserting reservation or clinical approval", async () => {
  for (const change of [
    { state: "approved" },
    { stock_reservation: "reserved" },
    { clinical_authority: "approved" },
  ]) {
    request.mockResolvedValue({ ...receipt(), ...change });
    await expect(readPrescriptionFulfilmentReview(scope, id, input)).rejects.toThrow();
  }
});
it("rejects client assertions of product or approval authority before sending", async () => {
  await expect(
    readPrescriptionFulfilmentReview(scope, id, {
      ...input,
      clinical_authority: "approved",
    } as unknown as typeof input),
  ).rejects.toThrow();
  await expect(
    readPrescriptionFulfilmentReview(scope, id, {
      ...input,
      lines: [{ ...input.lines[0]!, product_id: id }],
    } as unknown as typeof input),
  ).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
});

it.each([
  { source_state_hash: "A".repeat(64) },
  { lines: [] },
  { lines: [{ line_number: 1, quantity_units: 0 }] },
  { lines: [{ line_number: 1, quantity_units: 1.5 }] },
  { lines: [{ line_number: 1, quantity_units: 100001 }] },
  { lines: [{ line_number: 0, quantity_units: 5 }] },
  {
    lines: [
      { line_number: 1, quantity_units: 5 },
      { line_number: 1, quantity_units: 6 },
    ],
  },
])("rejects invalid proposed quantities before sending %j", async (change) => {
  await expect(
    readPrescriptionFulfilmentReview(scope, id, { ...input, ...change }),
  ).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
});
