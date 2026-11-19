import { z } from "zod";
import { getUserManager } from "./oidc";
import { workspaceSigninScope } from "./workspace-signin";

export const customerSignupSigninStateContract = z.discriminatedUnion("purpose", [
  z.object({ purpose: z.literal("customer-signup") }).strict(),
  z.object({ purpose: z.literal("customer-signup-login"), tenantId: z.string().uuid() }).strict(),
  z.object({ purpose: z.literal("workspace-login"), tenantId: z.string().guid() }).strict(),
  // The single email-and-password card (#1751 spec D4): no tenant is known
  // before the password, and the API takes the subject's only binding.
  z.object({ purpose: z.literal("email-login") }).strict(),
]);

export function signinForCustomerSignup(tenantId?: string) {
  const state = customerSignupSigninStateContract.parse(
    tenantId ? { purpose: "customer-signup-login", tenantId } : { purpose: "customer-signup" },
  );
  return getUserManager().signinRedirect({
    state,
    ...(tenantId ? { scope: workspaceSigninScope(tenantId) } : {}),
    prompt: "login",
    max_age: 0,
  });
}
