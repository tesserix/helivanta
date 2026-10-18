import { expect, it } from "vitest";
import { emergencyClinicalGrantSchema, emergencyTransportReviewSchema } from "./emergency-review";

it("requires an explicit expiring clinical privilege grant", () => {
  const body = {
    version: 0,
    enabled: false,
    evidence_reference: "SYN-CLINICAL-PRIVILEGE",
    expires_at: "2026-10-20T12:00:00Z",
  };
  expect(emergencyClinicalGrantSchema.parse(body)).toEqual(body);
  for (const patch of [
    { expires_at: "" },
    { evidence_reference: "" },
    { enabled: undefined },
    { version: -1 },
  ])
    expect(emergencyClinicalGrantSchema.safeParse({ ...body, ...patch }).success).toBe(false);
});
it("binds a reviewed transport class to a case version without defaulting clinical choices", () => {
  const body = {
    case_version: 1,
    required_class: "als",
    policy_reference: "SYN-TRANSPORT-POLICY",
    valid_until: "2026-10-20T12:00:00Z",
  };
  expect(emergencyTransportReviewSchema.parse(body)).toEqual(body);
  for (const patch of [
    { case_version: 0 },
    { required_class: "" },
    { required_class: "neonatal" },
    { valid_until: "" },
    { policy_reference: "" },
  ])
    expect(emergencyTransportReviewSchema.safeParse({ ...body, ...patch }).success).toBe(false);
});
