import { z } from "zod";

export function workspaceSigninScope(tenantId: string): string {
  const result = z.string().uuid().safeParse(tenantId);
  if (
    !result.success ||
    tenantId === "00000000-0000-0000-0000-000000000000" ||
    tenantId !== tenantId.toLowerCase()
  ) {
    throw new Error("Enter a valid workspace ID from your workspace link.");
  }
  return `openid profile email urn:zitadel:iam:org:id:helivanta-${result.data}`;
}
