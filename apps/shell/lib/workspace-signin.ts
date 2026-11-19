import { z } from "zod";

const rememberedWorkspaceKey = "helivanta.login.workspace.v1";
// Routing hint only (no proof, no access), so a month costs nothing but saves a mailbox code.
const rememberedWorkspaceDuration = 30 * 24 * 60 * 60 * 1000;
const workspaceChanged = "helivanta-workspace-preference-changed";

export function subscribeWorkspace(listener: () => void): () => void {
  window.addEventListener("storage", listener);
  window.addEventListener(workspaceChanged, listener);
  return () => {
    window.removeEventListener("storage", listener);
    window.removeEventListener(workspaceChanged, listener);
  };
}

// A routing preference only: native credentials, MFA and membership still apply.
export function rememberWorkspace(tenantId: string, now = Date.now()): void {
  try {
    workspaceSigninScope(tenantId);
    window.localStorage.setItem(
      rememberedWorkspaceKey,
      JSON.stringify({ tenantId, expiresAt: now + rememberedWorkspaceDuration }),
    );
    window.dispatchEvent(new Event(workspaceChanged));
  } catch {
    forgetWorkspace();
  }
}

export function forgetWorkspace(): void {
  try {
    window.localStorage.removeItem(rememberedWorkspaceKey);
    window.dispatchEvent(new Event(workspaceChanged));
  } catch {
    // Storage availability cannot prevent credential-based sign-in.
  }
}

export function readRememberedWorkspace(now = Date.now()): string | null {
  try {
    const raw = window.localStorage.getItem(rememberedWorkspaceKey);
    if (!raw) return null;
    const value = z
      .object({ tenantId: z.string(), expiresAt: z.number().finite() })
      .strict()
      .parse(JSON.parse(raw));
    workspaceSigninScope(value.tenantId);
    if (value.expiresAt <= now || value.expiresAt > now + rememberedWorkspaceDuration) {
      return null;
    }
    return value.tenantId;
  } catch {
    return null;
  }
}

export function workspaceSigninScope(tenantId: string): string {
  const result = z.string().guid().safeParse(tenantId);
  if (
    !result.success ||
    tenantId === "00000000-0000-0000-0000-000000000000" ||
    tenantId !== tenantId.toLowerCase()
  ) {
    throw new Error("Enter a valid workspace ID from your workspace link.");
  }
  return `openid profile email urn:zitadel:iam:org:id:helivanta-${result.data}`;
}
