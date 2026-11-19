import {
  PasswordRecoveryRequestContract,
  PasswordRecoveryCodeContract,
  PasswordRecoverySubmitContract,
  PasswordRecoveryCommandContract,
  PasswordRecoveryCancellationContract,
  PasswordRecoveryAcceptedContract,
  PasswordRecoveryVerifiedContract,
  PasswordRecoveryStatusContract,
  PasswordRecoveryResultContract,
  type PasswordRecoveryRequest,
  type PasswordRecoverySubmit,
} from "@tesserix/onboarding-contracts/customer-signup";
import { apiFetch } from "./client";

export {
  PasswordRecoveryRequestContract,
  PasswordRecoveryCodeContract,
  PasswordRecoverySubmitContract,
};
export type {
  PasswordRecoveryRequest,
  PasswordRecoverySubmit,
} from "@tesserix/onboarding-contracts/customer-signup";
export { v7 as newRecoveryCommandId } from "uuid";
const publicStatus = PasswordRecoveryStatusContract.omit({ commandId: true }).strict();
const publicResult = PasswordRecoveryResultContract.omit({ commandId: true }).strict();
export type PasswordRecoveryStatus = Omit<
  import("@tesserix/onboarding-contracts/customer-signup").PasswordRecoveryStatus,
  "commandId"
>;
export type PasswordRecoveryResult = Omit<
  import("@tesserix/onboarding-contracts/customer-signup").PasswordRecoveryResult,
  "commandId"
>;
const publicDelivery = PasswordRecoveryAcceptedContract.omit({ challengeId: true }).strict();

export async function requestPasswordRecovery(input: PasswordRecoveryRequest) {
  const body = PasswordRecoveryRequestContract.parse(input);
  return publicDelivery.parse(
    await apiFetch("/auth/password-recovery", {
      method: "POST",
      body: JSON.stringify(body),
      cache: "no-store",
    }),
  );
}
export async function verifyPasswordRecovery(code: string) {
  const body = PasswordRecoveryCodeContract.parse({ code });
  return PasswordRecoveryVerifiedContract.parse(
    await apiFetch("/auth/password-recovery/verify", {
      method: "POST",
      body: JSON.stringify(body),
      cache: "no-store",
    }),
  );
}
export async function getPasswordRecoveryStatus() {
  return publicStatus.parse(await apiFetch("/auth/password-recovery", { cache: "no-store" }));
}
export async function submitRecoveryPassword(input: PasswordRecoverySubmit) {
  const body = PasswordRecoverySubmitContract.parse(input);
  return publicResult.parse(
    await apiFetch("/auth/password-recovery/password", {
      method: "POST",
      body: JSON.stringify(body),
      cache: "no-store",
    }),
  );
}

export async function prepareRecoveryPassword(commandId: string) {
  const body = PasswordRecoveryCommandContract.parse({ command_id: commandId });
  return publicResult.parse(
    await apiFetch("/auth/password-recovery/prepare", {
      method: "POST",
      body: JSON.stringify(body),
      cache: "no-store",
    }),
  );
}

export async function cancelRecoveryPassword(commandId?: string) {
  const body = PasswordRecoveryCancellationContract.parse(
    commandId ? { command_id: commandId } : {},
  );
  return publicResult.parse(
    await apiFetch("/auth/password-recovery/cancel", {
      method: "POST",
      body: JSON.stringify(body),
      cache: "no-store",
    }),
  );
}
