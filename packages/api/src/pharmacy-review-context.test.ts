import { afterEach, describe, expect, it, vi } from "vitest";
import { readPrescriptionReviewContext, prescriptionSchema } from "./pharmacy-prescriptions";
import { PrescriptionReviewContextContract } from "./pharmacy-verification-contract";

const scope = {
  subject: "synthetic-pharmacist",
  tenantId: "11111111-1111-7111-8111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000004",
  outletId: "0199e000-0000-7000-8000-000000000002",
};
const id = "0199e000-0000-7000-8000-000000000001";
const source = {
  owner_tenant_id: scope.tenantId,
  reader_subject: scope.subject,
  pharmacy_id: scope.pharmacyId,
  source_schema_version: 1,
  source_kind: "recorded_prescription",
  source_authenticity: "not_evaluated",
  source_state_hash: "a".repeat(64),
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
    recorded_by: "synthetic-pharmacist",
    created_at: "2026-11-26T00:00:00Z",
    lines: [
      {
        line_number: 1,
        prescribed_as: "Synthetic product",
        product_id: "0199e000-0000-7000-8000-000000000003",
        quantity_units: 20,
        dispensings_allowed: 2,
        units_remaining: 15,
        dispensings_remaining: 1,
      },
    ],
  },
};

afterEach(() => vi.unstubAllGlobals());

describe("Recorded prescription review context", () => {
  it("uses the same canonical recorded-prescription response as the existing SDK", () => {
    expect(prescriptionSchema.parse(source.record)).toEqual(source.record);
    expect(PrescriptionReviewContextContract.parse(source)).toEqual(source);
    for (const change of [
      { source_state_hash: "a".repeat(64) + "\n" },
      { clinical_authority: "authorized" },
      { source_authenticity: "verified" },
      { clinical_context: { ...source.clinical_context, allergies: "none" } },
      { record: { ...source.record, lines: [] } },
      { record: { ...source.record, lines: [{ ...source.record.lines[0], units_remaining: 21 }] } },
      {
        record: {
          ...source.record,
          lines: [{ ...source.record.lines[0], dispensings_remaining: 3 }],
        },
      },
      { record: { ...source.record, lines: [{ ...source.record.lines[0], line_number: 2 }] } },
    ])
      expect(PrescriptionReviewContextContract.safeParse({ ...source, ...change }).success).toBe(
        false,
      );
  });

  it("reads through Global API without caching and pins the expected tenant", async () => {
    const fetch = vi.fn(async () => Response.json(source));
    vi.stubGlobal("fetch", fetch);
    const signal = new AbortController().signal;
    expect(await readPrescriptionReviewContext(scope, id, signal)).toEqual(source);
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe(
      `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/prescriptions/${id}/review-context`,
    );
    expect(init.cache).toBe("no-store");
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
    expect(init.signal).toBe(signal);
    expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  });

  it("rejects a receipt from another tenant, pharmacy, outlet or prescription", async () => {
    for (const change of [
      { reader_subject: "synthetic-other-reader" },
      { owner_tenant_id: "22222222-2222-7222-8222-222222222222" },
      { pharmacy_id: "0199e000-0000-7000-8000-000000000005" },
      { record: { ...source.record, outlet_id: "0199e000-0000-7000-8000-000000000006" } },
      { record: { ...source.record, id: "0199e000-0000-7000-8000-000000000007" } },
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => Response.json({ ...source, ...change })),
      );
      await expect(readPrescriptionReviewContext(scope, id)).rejects.toThrow(/context changed/);
    }
  });

  it("does not turn an unavailable or forbidden source into empty or approved data", async () => {
    for (const status of [403, 404, 503]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () =>
          Response.json(
            { error: "unavailable", message: "Synthetic unavailable source" },
            { status },
          ),
        ),
      );
      await expect(readPrescriptionReviewContext(scope, id)).rejects.toThrow();
    }
  });
});
