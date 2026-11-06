import { describe, expect, it } from "vitest";
import { PartnerGrantDraftInputContract } from "./partner-grant-contract";

const id = "019a55c0-1111-7111-8111-111111111111";
const input = {
  owner_product: "pharmacy",
  owner_organization_id: id,
  source_location_id: id,
  relationship_id: id,
  relationship_version: 1,
  recipient_kind: "group",
  recipient_tenant_id: "22222222-2222-2222-2222-222222222222",
  recipient_hospital_id: "11111111-1111-1111-1111-111111111111",
  recipient_group_id: id,
  template_key: "partner.shipment_reader.v1",
  source_jurisdiction: "IN-MH",
  destination_jurisdiction: "IN-OD",
  valid_from: "2026-11-07T00:00:00Z",
  expires_at: "2026-11-08T00:00:00Z",
  reason: "Synthetic receiving-team access intent",
};

describe("partner grant preparation contract", () => {
  it("accepts a hospital group or one named account with a single stock owner", () => {
    expect(PartnerGrantDraftInputContract.safeParse(input).success).toBe(true);
    const named = { ...input };
    Reflect.deleteProperty(named, "recipient_group_id");
    expect(
      PartnerGrantDraftInputContract.safeParse({
        ...named,
        owner_product: "supply",
        recipient_kind: "subject",
        recipient_subject: "synthetic-hospital-buyer",
      }).success,
    ).toBe(true);
  });

  it.each([
    "owner_organization_id",
    "source_location_id",
    "relationship_id",
    "recipient_tenant_id",
    "recipient_hospital_id",
    "recipient_group_id",
  ])("refuses the zero UUID for %s", (field) => {
    expect(
      PartnerGrantDraftInputContract.safeParse({
        ...input,
        [field]: "00000000-0000-0000-0000-000000000000",
      }).success,
    ).toBe(false);
  });

  it.each(["owner_organization_id", "source_location_id", "relationship_id"])(
    "requires UUIDv7 source references for %s",
    (field) => {
      expect(
        PartnerGrantDraftInputContract.safeParse({
          ...input,
          [field]: "bff7c586-c7cf-4c5f-aa3b-e2a8770103c2",
        }).success,
      ).toBe(false);
    },
  );
});
