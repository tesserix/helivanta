import { z } from "zod";
import {
  CustomerBusinessAddressContract,
  CustomerOrganizationProfileContract,
  CustomerBusinessProfileViewContract,
  type CustomerOrganizationProfile,
  CustomerSignupInputContract,
  CustomerSignupReceiptContract,
  CustomerSignupProofContract,
  CustomerSignupSigninInputContract,
  type CustomerSignupSigninInput,
  CustomerSignupDeliveryContract,
  type CustomerSignupInput,
  type CustomerSignupReceipt,
  type CustomerSignupProof,
  type CustomerSignupDelivery,
} from "@tesserix/onboarding-contracts/customer-signup";
import { apiFetch } from "./client";

export type { CustomerSignupInput, CustomerSignupReceipt };
export {
  CustomerSignupInputContract,
  CustomerSignupReceiptContract,
  CustomerBusinessAddressContract,
  CustomerOrganizationProfileContract,
};
export type { CustomerOrganizationProfile };

async function signupRequest(path: string, init: RequestInit = {}) {
  return CustomerSignupReceiptContract.parse(
    await apiFetch<unknown>(path, { ...init, cache: "no-store" }),
  );
}

export function getCustomerSignup(signal?: AbortSignal) {
  return signupRequest("/auth/signup", { signal });
}

export function startCustomerSignup(input: CustomerSignupInput) {
  const body = CustomerSignupInputContract.parse(input);
  return signupRequest("/auth/signup", { method: "POST", body: JSON.stringify(body) });
}

export function continueCustomerSignup(input: CustomerSignupSigninInput) {
  const body = CustomerSignupSigninInputContract.parse(input);
  return signupRequest("/auth/signup/sign-in", { method: "POST", body: JSON.stringify(body) });
}

export function verifyCustomerEmail(input: CustomerSignupProof) {
  const body = CustomerSignupProofContract.parse(input);
  return signupRequest("/auth/signup/verify-email", { method: "POST", body: JSON.stringify(body) });
}

export function setCustomerPassword(input: CustomerSignupProof) {
  const body = CustomerSignupProofContract.parse(input);
  return signupRequest("/auth/signup/password", { method: "POST", body: JSON.stringify(body) });
}

export function sendCustomerLink(kind: "email" | "password", input: CustomerSignupDelivery = {}) {
  const body = CustomerSignupDeliveryContract.parse(input);
  return signupRequest(`/auth/signup/${kind}-link`, { method: "POST", body: JSON.stringify(body) });
}

export async function getCustomerBusinessProfile(
  tenantId: string,
  kind: "hospital" | "pharmacy",
  resourceId: string,
  signal?: AbortSignal,
) {
  const id = z.string().uuid().parse(resourceId);
  const path =
    kind === "hospital"
      ? `/workforce/hospitals/${id}/business-profile`
      : `/pharmacy/pharmacies/${id}/business-profile`;
  return CustomerBusinessProfileViewContract.parse(
    await apiFetch(path, {
      signal,
      cache: "no-store",
      headers: { "X-Helivanta-Expected-Tenant": z.string().uuid().parse(tenantId) },
    }),
  );
}
