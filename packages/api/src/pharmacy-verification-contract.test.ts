import { describe, expect, it } from "vitest";
import {
  PrescriptionVerificationDecisionInputContract,
  PrescriptionVerificationSecondCheckInputContract,
} from "./pharmacy-verification-contract";

const command = {
  expected_version: 1,
  reviewed_state_hash: "a".repeat(64),
  decision: "approved",
  reason_code: null,
  clinical_note: null,
  prescriber_amendment_id: null,
};

describe("Prescription verification command intent", () => {
  it("binds a plain approval to an explicit version and exact digest", () => {
    expect(PrescriptionVerificationDecisionInputContract.parse(command)).toEqual(command);
    for (const changed of [
      { expected_version: 0 },
      { expected_version: Number.MAX_SAFE_INTEGER + 1 },
      { reviewed_state_hash: "A".repeat(64) },
      { reviewed_state_hash: "a".repeat(63) },
      { reviewed_state_hash: "g".repeat(64) },
      { reviewed_state_hash: " " + "a".repeat(64) },
      { reviewed_state_hash: "a".repeat(64) + "\n" },
    ]) {
      expect(
        PrescriptionVerificationDecisionInputContract.safeParse({ ...command, ...changed }).success,
      ).toBe(false);
    }
  });

  it.each(["query_raised", "rejected", "approved_with_change"])(
    "requires a reason and clinical note for %s",
    (decision) => {
      const intent = {
        ...command,
        decision,
        reason_code: "prescriber_clarification",
        clinical_note: "Synthetic prescriber clarification reviewed.",
        prescriber_amendment_id:
          decision === "approved_with_change" ? "0199e000-0000-7000-8000-000000000001" : null,
      };
      expect(PrescriptionVerificationDecisionInputContract.safeParse(intent).success).toBe(true);
      for (const changed of [
        { reason_code: null },
        { clinical_note: null },
        { clinical_note: "   " },
      ]) {
        expect(
          PrescriptionVerificationDecisionInputContract.safeParse({ ...intent, ...changed })
            .success,
        ).toBe(false);
      }
    },
  );

  it("requires an amendment reference exactly for approval with change", () => {
    expect(
      PrescriptionVerificationDecisionInputContract.safeParse({
        ...command,
        decision: "approved_with_change",
        reason_code: "prescriber_amendment",
        clinical_note: "Synthetic amended source reviewed.",
      }).success,
    ).toBe(false);
    expect(
      PrescriptionVerificationDecisionInputContract.safeParse({
        ...command,
        prescriber_amendment_id: "0199e000-0000-7000-8000-000000000001",
      }).success,
    ).toBe(false);
  });

  it("rejects caller assertions of authority and unknown decisions", () => {
    for (const changed of [
      { pharmacist_id: "0199e000-0000-7000-8000-000000000001" },
      { registration_number: "QA-NOT-A-REGISTRATION" },
      { tenant_id: "0199e000-0000-7000-8000-000000000001" },
      { registration_valid: true },
      { break_glass: true },
      { decision: "emergency_approved" },
    ]) {
      expect(
        PrescriptionVerificationDecisionInputContract.safeParse({ ...command, ...changed }).success,
      ).toBe(false);
    }
  });

  it("bounds clinical text without silently normalizing the recorded intent", () => {
    expect(
      PrescriptionVerificationDecisionInputContract.parse({
        ...command,
        clinical_note: "  Synthetic note  ",
      }).clinical_note,
    ).toBe("  Synthetic note  ");
    for (const changed of [
      { clinical_note: "x".repeat(2001) },
      { clinical_note: "Synthetic\0note" },
      { reason_code: "not a machine code" },
      { reason_code: "x".repeat(81) },
      { reason_code: "prescriber_clarification\n" },
    ]) {
      expect(
        PrescriptionVerificationDecisionInputContract.safeParse({ ...command, ...changed }).success,
      ).toBe(false);
    }
  });

  it("uses UTF-16 text bounds consistently with server intent validation", () => {
    expect(
      PrescriptionVerificationDecisionInputContract.safeParse({
        ...command,
        clinical_note: "😀".repeat(1000),
      }).success,
    ).toBe(true);
    expect(
      PrescriptionVerificationDecisionInputContract.safeParse({
        ...command,
        clinical_note: "😀".repeat(1001),
      }).success,
    ).toBe(false);
    expect(
      PrescriptionVerificationDecisionInputContract.safeParse({
        ...command,
        decision: "rejected",
        reason_code: "clarification",
        clinical_note: "\u00a0\ufeff",
      }).success,
    ).toBe(false);
  });
});

describe("Prescription second-check command intent", () => {
  const check = {
    expected_version: 2,
    reviewed_state_hash: "b".repeat(64),
    primary_decision_id: "0199e000-0000-7000-8000-000000000002",
  };

  it("names the exact primary decision, version and digest", () => {
    expect(PrescriptionVerificationSecondCheckInputContract.parse(check)).toEqual(check);
    for (const changed of [
      { primary_decision_id: "00000000-0000-0000-0000-000000000000" },
      { primary_decision_id: null },
      { expected_version: 0 },
      { reviewed_state_hash: "" },
      { second_pharmacist_id: "0199e000-0000-7000-8000-000000000003" },
    ]) {
      expect(
        PrescriptionVerificationSecondCheckInputContract.safeParse({ ...check, ...changed })
          .success,
      ).toBe(false);
    }
  });
});
