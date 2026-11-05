import { describe, expect, it } from "vitest";
import {
  StaffDepartmentBindingCommandInputContract,
  StaffDepartmentBindingProposalInputContract,
} from "./pharmacy-staff-binding-contract";

const proposal = {
  department_id: "0199e000-0000-7000-8000-000000000001",
  department_revision_id: "0199e000-0000-7000-8000-000000000002",
  replaces_binding_id: null,
  replaces_binding_version: null,
  reason: "Synthetic department relationship proposal",
};

describe("Pharmacy department binding intent", () => {
  it("requires an exact replacement id and version together", () => {
    expect(StaffDepartmentBindingProposalInputContract.safeParse(proposal).success).toBe(true);
    expect(
      StaffDepartmentBindingProposalInputContract.safeParse({
        ...proposal,
        replaces_binding_id: "0199e000-0000-7000-8000-000000000003",
        replaces_binding_version: 2,
      }).success,
    ).toBe(true);
    for (const basis of [
      { replaces_binding_id: "0199e000-0000-7000-8000-000000000003" },
      { replaces_binding_version: 2 },
    ]) {
      expect(
        StaffDepartmentBindingProposalInputContract.safeParse({ ...proposal, ...basis }).success,
      ).toBe(false);
    }
  });

  it("requires relationship attestation exactly when approving", () => {
    const command = {
      id: "0199e000-0000-7000-8000-000000000004",
      expected_version: 1,
      reason: "Synthetic independent decision",
    };
    expect(
      StaffDepartmentBindingCommandInputContract.safeParse({
        ...command,
        operation: "approve",
        attest_department_responsibility: true,
      }).success,
    ).toBe(true);
    expect(
      StaffDepartmentBindingCommandInputContract.safeParse({
        ...command,
        operation: "reject",
        attest_department_responsibility: false,
      }).success,
    ).toBe(true);
    expect(
      StaffDepartmentBindingCommandInputContract.safeParse({
        ...command,
        operation: "approve",
        attest_department_responsibility: false,
      }).success,
    ).toBe(false);
    expect(
      StaffDepartmentBindingCommandInputContract.safeParse({
        ...command,
        operation: "reject",
        attest_department_responsibility: true,
      }).success,
    ).toBe(false);
  });
});
