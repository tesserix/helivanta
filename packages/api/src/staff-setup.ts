import { v7 } from "uuid";
import {
  StaffInvitationReceiptContract,
  type StaffRecipientInput,
} from "@tesserix/onboarding-contracts/staff-setup";
import { apiFetch } from "./client";

export { StaffInvitationReceiptContract };
export type { StaffRecipientInput };
export type StaffSetupReceipt = ReturnType<typeof StaffInvitationReceiptContract.parse>;
export type StaffSetupAction = "status" | "email" | "password" | "resend";

export function newStaffInvitationDeliveryID() {
  return v7();
}

export async function staffSetup(
  action: StaffSetupAction,
  input: StaffRecipientInput,
  signal?: AbortSignal,
) {
  const receipt = StaffInvitationReceiptContract.parse(
    await apiFetch<unknown>(`/auth/staff-setup/${action}`, {
      method: "POST",
      body: JSON.stringify(input),
      cache: "no-store",
      signal,
    }),
  );
  const expected = action === "resend" ? input.delivery_id : input.invitation_id;
  if (receipt.invitation_id && receipt.invitation_id !== expected?.toLowerCase()) {
    throw new Error("The setup response could not be verified.");
  }
  return receipt;
}
