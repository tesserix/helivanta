import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions } from "./enrollment";
import { onTenantTransition, tenantTransitionPending } from "./tenant-transition";

export type AccountRequestScope = { tenantId: string; subject: string };
const tenant = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const subject = z
  .string()
  .min(1)
  .max(200)
  .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value));

export async function accountScopedRequest(
  scope: AccountRequestScope,
  path: string,
  signal?: AbortSignal,
  command?: { method: "PUT" | "POST"; body: unknown },
) {
  const tenantId = tenant.parse(scope.tenantId).toLowerCase();
  const currentSubject = subject.parse(scope.subject);
  signal?.throwIfAborted();
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  const transition = new AbortController();
  const combined = signal ? AbortSignal.any([transition.signal, signal]) : transition.signal;
  const unsubscribe = onTenantTransition(() => {
    if (tenantTransitionPending()) transition.abort();
  });
  try {
    const raw = await apiFetch(path, {
      ...enrollmentRequestOptions({ tenantId, subject: currentSubject }),
      ...(command ? { method: command.method, body: JSON.stringify(command.body) } : {}),
      signal: combined,
    });
    combined.throwIfAborted();
    return raw;
  } finally {
    unsubscribe();
  }
}
