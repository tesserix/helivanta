import {
  AccountDiscoveryRequestContract,
  AccountDiscoveryCodeContract,
  AccountDiscoveryAcceptedContract,
  AccountDiscoveryVerifiedContract,
  AccountDiscoveryChoicesContract,
} from "@tesserix/onboarding-contracts/customer-signup";
import { apiFetch } from "./client";

export type { AccountDiscoveryChoice } from "@tesserix/onboarding-contracts/customer-signup";
export { AccountDiscoveryRequestContract, AccountDiscoveryCodeContract };

export async function requestAccountCode(email: string) {
  const body = AccountDiscoveryRequestContract.parse({ email });
  return AccountDiscoveryAcceptedContract.parse(
    await apiFetch("/auth/accounts", {
      method: "POST",
      body: JSON.stringify(body),
      cache: "no-store",
    }),
  );
}

export async function verifyAccountCode(code: string) {
  const body = AccountDiscoveryCodeContract.parse({ code });
  return AccountDiscoveryVerifiedContract.parse(
    await apiFetch("/auth/accounts/verify", {
      method: "POST",
      body: JSON.stringify(body),
      cache: "no-store",
    }),
  );
}

export async function getAccountChoices() {
  return AccountDiscoveryChoicesContract.parse(
    await apiFetch("/auth/accounts", { cache: "no-store" }),
  );
}
