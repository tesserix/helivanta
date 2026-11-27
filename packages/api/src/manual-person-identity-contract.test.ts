import { describe, expect, it } from "vitest";
import {
  ManualIdentityCaseInputContract,
  ManualIdentityExaminationInputContract,
  ManualIdentityDecisionInputContract,
} from "./manual-person-identity-contract";

const id = "0199e000-0000-7000-8000-000000000001";
const intent = {
  source_profile_id: id,
  expected_profile_version: 1,
  consent_acknowledged: true,
  reason: "Identity assurance for this account",
};

describe("manual identity command contracts", () => {
  it("retains exact text instead of silently normalizing command intent", () => {
    const input = { ...intent, reason: "  Examined original evidence  " };
    expect(ManualIdentityCaseInputContract.parse(input)).toEqual(input);
  });

  it.each(["\nEvidence", "Evidence\n", "Evidence\u0085", " ", "\ufeff"])(
    "rejects invalid reason %j",
    (reason) => {
      expect(ManualIdentityCaseInputContract.safeParse({ ...intent, reason }).success).toBe(false);
    },
  );

  it("rejects non-v7 identity references", () => {
    expect(
      ManualIdentityCaseInputContract.safeParse({
        ...intent,
        source_profile_id: "11111111-1111-4111-8111-111111111111",
      }).success,
    ).toBe(false);
  });

  it("rejects caller-supplied account identity and missing consent or version", () => {
    for (const changed of [
      { subject: "other-account" },
      { person_id: id },
      { consent_acknowledged: false },
      { expected_profile_version: undefined },
      { expected_profile_version: 0 },
    ]) {
      expect(ManualIdentityCaseInputContract.safeParse({ ...intent, ...changed }).success).toBe(
        false,
      );
    }
  });

  it("requires explicit original examination and person-version preconditions", () => {
    const examination = {
      expected_case_version: 1,
      person_id: id,
      expected_person_version: 0,
      source_person_reference: id,
      source_reference: id,
      finding_reference: id,
      observed_at: "2026-11-27T00:00:00Z",
      valid_until: "2026-11-28T00:00:00Z",
      original_evidence_examined: true,
      known_associations_checked: true,
      reason: "Original evidence examined",
    };
    expect(ManualIdentityExaminationInputContract.parse(examination)).toEqual(examination);
    for (const changed of [
      { original_evidence_examined: false },
      { known_associations_checked: false },
      { expected_person_version: undefined },
      { expected_person_version: -1 },
    ]) {
      expect(
        ManualIdentityExaminationInputContract.safeParse({ ...examination, ...changed }).success,
      ).toBe(false);
    }
  });

  it("requires attributable independent approval or rejection", () => {
    const decision = {
      expected_case_version: 2,
      examination_id: id,
      decision: "approve",
      evidence_examined: true,
      independent_person_confirmed: true,
      reason: "Independent examination finding",
    };
    expect(ManualIdentityDecisionInputContract.parse(decision)).toEqual(decision);
    expect(
      ManualIdentityDecisionInputContract.parse({ ...decision, decision: "reject" }).decision,
    ).toBe("reject");
    for (const changed of [
      { decision: "accept" },
      { evidence_examined: false },
      { independent_person_confirmed: false },
      { examination_id: null },
    ]) {
      expect(
        ManualIdentityDecisionInputContract.safeParse({ ...decision, ...changed }).success,
      ).toBe(false);
    }
  });
});
