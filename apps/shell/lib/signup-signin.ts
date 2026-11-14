import { z } from "zod";
import { getUserManager } from "./oidc";
import { workspaceSigninScope } from "./workspace-signin";

export const customerSignupSigninStateContract = z.discriminatedUnion("purpose", [
  z.object({ purpose: z.literal("customer-signup") }).strict(),
  z.object({ purpose: z.literal("customer-signup-login"), tenantId: z.string().uuid() }).strict(),
  z.object({ purpose: z.literal("workspace-login"), tenantId: z.string().guid() }).strict(),
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
