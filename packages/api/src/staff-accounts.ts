import { z } from "zod";
import {
  StaffContactInputContract,
  StaffIdentityReceiptContract,
  StaffIdentityObservationReceiptContract,
  StaffInvitationReceiptContract,
  type StaffContactInput,
  type StaffIdentityReceipt,
} from "@tesserix/onboarding-contracts/staff-setup";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import type { StaffAssignmentScope } from "./staff-assignments";

export { StaffContactInputContract as staffContactInputContract };
export type { StaffContactInput, StaffIdentityReceipt };
export type StaffAccountObservation = ReturnType<
  typeof StaffIdentityObservationReceiptContract.parse
>;
export { v7 as newStaffAccountCommandId } from "uuid";

function commandPath(id: string) {
  z.uuidv7()
    .refine((value) => value === value.toLowerCase())
    .parse(id);
  return "/iam/staff-account-commands/" + id;
}
function boundReceipt(value: unknown, scope: StaffAssignmentScope, id: string) {
  const receipt = StaffIdentityReceiptContract.parse(value);
  if (
    receipt.schema_version !== 1 ||
    receipt.command_id !== id ||
    receipt.tenant_id !== scope.tenantId ||
    receipt.actor_subject !== scope.subject ||
    receipt.product_key !== "helivanta"
  ) {
    throw new Error("The account command response could not be verified.");
  }
  return receipt;
}
export async function createStaffAccount(
  scope: StaffAssignmentScope,
  id: string,
  input: StaffContactInput,
) {
  const body = StaffContactInputContract.parse(input);
  const receipt = await apiFetch<unknown>(commandPath(id), {
    ...membershipRequestOptions(scope),
    method: "PUT",
    body: JSON.stringify(body),
  });
  return boundReceipt(receipt, scope, id);
}
export async function readStaffAccount(scope: StaffAssignmentScope, id: string) {
  const receipt = await apiFetch<unknown>(commandPath(id), {
    ...membershipRequestOptions(scope),
    method: "GET",
  });
  return boundReceipt(receipt, scope, id);
}
export async function observeStaffAccount(scope: StaffAssignmentScope, id: string) {
  const observation = StaffIdentityObservationReceiptContract.parse(
    await apiFetch<unknown>(commandPath(id) + "/observations", {
      ...membershipRequestOptions(scope),
      method: "POST",
      body: "{}",
    }),
  );
  boundReceipt(observation.receipt, scope, id);
  return observation;
}
export async function deliverStaffAccountInvitation(
  scope: StaffAssignmentScope,
  id: string,
  delivery: string,
) {
  z.uuidv7()
    .refine((value) => value === value.toLowerCase())
    .parse(delivery);
  const receipt = StaffInvitationReceiptContract.parse(
    await apiFetch<unknown>(commandPath(id) + "/invitations/" + delivery, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: "{}",
    }),
  );
  if (receipt.invitation_id && receipt.invitation_id !== delivery) {
    throw new Error("The setup delivery response could not be verified.");
  }
  return receipt;
}
