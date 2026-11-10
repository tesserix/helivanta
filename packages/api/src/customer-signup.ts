import {
  CustomerSignupInputContract,
  CustomerSignupReceiptContract,
  CustomerSignupProofContract,
  CustomerSignupDeliveryContract,
  type CustomerSignupInput,
  type CustomerSignupReceipt,
  type CustomerSignupProof,
  type CustomerSignupDelivery,
} from "@tesserix/onboarding-contracts/customer-signup";
import { apiFetch } from "./client";

export type { CustomerSignupInput, CustomerSignupReceipt };

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
