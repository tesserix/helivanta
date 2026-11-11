import { z } from "zod";
import { getUserManager } from "./oidc";

export const customerSignupSigninStateContract = z.discriminatedUnion("purpose", [
  z.object({ purpose: z.literal("customer-signup") }).strict(),
  z.object({ purpose: z.literal("customer-signup-login"), tenantId: z.string().uuid() }).strict(),
]);

export function signinForCustomerSignup(tenantId?: string) {
  const state = customerSignupSigninStateContract.parse(
    tenantId ? { purpose: "customer-signup-login", tenantId } : { purpose: "customer-signup" },
  );
  return getUserManager().signinRedirect({ state, prompt: "login", max_age: 0 });
}
