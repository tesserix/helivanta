import { z } from "zod";
import {
  StaffMFAResetInputContract,
  StaffMFARecoveryReceiptContract,
  type StaffMFAResetInput,
  type StaffMFARecoveryReceipt,
} from "@tesserix/onboarding-contracts/customer-signup";
import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";

export { StaffMFAResetInputContract, StaffMFARecoveryReceiptContract };
export type { StaffMFAResetInput, StaffMFARecoveryReceipt };
export { v7 as newStaffMFARecoveryCommandId } from "uuid";
export type StaffMFARecoveryScope = AccountRequestScope & {
  employerKind: "hospital" | "pharmacy" | "supplier";
  employerId: string;
  staffId: string;
};

const employerKind = z.enum(["hospital", "pharmacy", "supplier"]);
const resourceId = z
  .string()
  .guid()
  .refine((id) => id !== "00000000-0000-0000-0000-000000000000" && id === id.toLowerCase());
function commandPath(scope: StaffMFARecoveryScope, command: string) {
  const kind = employerKind.parse(scope.employerKind);
  const family = { hospital: "hospitals", pharmacy: "pharmacies", supplier: "suppliers" }[kind];
  return `/workforce/${family}/${resourceId.parse(scope.employerId)}/staff/${resourceId.parse(scope.staffId)}/mfa-recoveries/${StaffMFAResetInputContract.shape.commandId.parse(command)}`;
}
function receiptFor(value: unknown, command: string) {
  const receipt = StaffMFARecoveryReceiptContract.parse(value);
  if (receipt.commandId !== command)
    throw new Error("The recovery response could not be verified. Check the existing request.");
  return receipt;
}
export async function resetStaffMFA(scope: StaffMFARecoveryScope, input: StaffMFAResetInput) {
  const body = StaffMFAResetInputContract.parse(input);
  return receiptFor(
    await accountScopedRequest(scope, commandPath(scope, body.commandId), undefined, {
      method: "PUT",
      body,
    }),
    body.commandId,
  );
}
export async function readStaffMFARecovery(
  scope: StaffMFARecoveryScope,
  command: string,
  signal?: AbortSignal,
) {
  return receiptFor(
    await accountScopedRequest(scope, commandPath(scope, command), signal),
    command,
  );
}
export async function cancelStaffMFARecovery(scope: StaffMFARecoveryScope, command: string) {
  return receiptFor(
    await accountScopedRequest(scope, commandPath(scope, command) + "/cancel", undefined, {
      method: "POST",
      body: {},
    }),
    command,
  );
}
