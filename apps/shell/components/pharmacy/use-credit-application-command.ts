"use client";

import "@/lib/csp-zod";

import { useRef, useState } from "react";
import { z } from "zod";
import {
  ApiError,
  newPharmacyId,
  pharmacyKey,
  pharmacySupplierCreditAllocationInputSchema,
  useApiMutation,
  type PharmacyActorScope,
} from "@helivanta/api";
const identifier = pharmacySupplierCreditAllocationInputSchema.shape.payable_id;
const referenceSchema = z.object({ id: identifier, target: identifier.optional() }).strict();
export type CreditApplicationReference = z.infer<typeof referenceSchema>;
export function creditApplicationCommandKey(
  scope: PharmacyActorScope,
  credit: string,
  kind: "application" | "application-reversal",
) {
  return `helivanta:supplier-credit-${kind}:v1:${JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId, credit])}`;
}
export function readCreditApplicationReference(key: string) {
  try {
    const value = sessionStorage.getItem(key);
    return {
      reference: value === null ? null : referenceSchema.parse(JSON.parse(value)),
      error: "",
    };
  } catch {
    return {
      reference: null,
      error:
        "The original application recovery reference is unavailable or invalid. Restore it before creating another command.",
    };
  }
}
export function useCreditApplicationCommand<Intent, Receipt>({
  scope,
  storageKey,
  label,
  canWrite,
  write,
  recover,
  onVerified,
  initial,
}: {
  scope: PharmacyActorScope;
  storageKey: string;
  label: string;
  canWrite: boolean;
  write: (id: string, intent: Intent) => Promise<Receipt>;
  recover: (reference: CreditApplicationReference, intent: Intent | undefined) => Promise<Receipt>;
  onVerified: () => void;
  initial: ReturnType<typeof readCreditApplicationReference>;
}) {
  const [reference, setReference] = useState(initial.reference);
  const [attempt, setAttempt] = useState<Intent | undefined>();
  const [missing, setMissing] = useState(false),
    [notice, setNotice] = useState("");
  const locked = useRef(false),
    uncertain = useRef(initial.reference !== null);
  function finish() {
    locked.current = false;
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      setNotice(
        `${label} is verified but its recovery reference could not be cleared. Check the same original before continuing.`,
      );
      return;
    }
    uncertain.current = false;
    setReference(null);
    setAttempt(undefined);
    setMissing(false);
    setNotice(`${label} verified. Review the updated credit and bill balances.`);
    onVerified();
  }
  const save = useApiMutation(
    ({ id, intent }: { id: string; intent: Intent }) => write(id, intent),
    {
      gcTime: 0,
      suppressErrorToast: true,
      invalidate: [pharmacyKey(scope)],
      onSuccess: finish,
      onError: (error) => {
        locked.current = false;
        if (
          !uncertain.current &&
          error instanceof ApiError &&
          [400, 401, 403, 404, 409, 422].includes(error.status)
        ) {
          try {
            sessionStorage.removeItem(storageKey);
          } catch {
            uncertain.current = true;
            setNotice(
              "Keep the original recovery reference and check the recorded result before continuing.",
            );
            return;
          }
          setReference(null);
          setAttempt(undefined);
          setNotice(
            `${label} was refused. Refresh the current credit and bill observations before creating another command.`,
          );
        } else {
          uncertain.current = true;
          setNotice(`${label} outcome is unconfirmed. Check the original result before retrying.`);
        }
      },
    },
  );
  const recovery = useApiMutation((value: CreditApplicationReference) => recover(value, attempt), {
    gcTime: 0,
    suppressErrorToast: true,
    invalidate: [pharmacyKey(scope)],
    onSuccess: finish,
    onError: (error) => {
      locked.current = false;
      if (error instanceof ApiError && error.status === 404) {
        setMissing(true);
        setNotice(
          attempt
            ? "The original is not visible. Retry only the exact submitted command."
            : "The original is not visible. Re-enter the original intent using the same reference; an existing command cannot be overwritten.",
        );
      } else
        setNotice(
          "The original result could not be verified. Keep its reference and check again with current financial access.",
        );
    },
  });
  return {
    reference,
    storageError: initial.error,
    notice,
    missing,
    hasIntent: attempt !== undefined,
    busy: save.isPending || recovery.isPending,
    submit: (intent: Intent, target?: string) => {
      if (locked.current || !canWrite || initial.error || (reference !== null && !missing)) return;
      if (reference?.target && reference.target !== target) {
        setNotice("Resolve the original target before creating another command.");
        return;
      }
      const next =
        reference ?? referenceSchema.parse({ id: newPharmacyId(), ...(target ? { target } : {}) });
      try {
        sessionStorage.setItem(storageKey, JSON.stringify(next));
      } catch {
        setNotice("Session storage is unavailable. No financial command has been submitted.");
        return;
      }
      locked.current = true;
      setReference(next);
      setAttempt(intent);
      setMissing(false);
      setNotice("");
      save.mutate({ id: next.id, intent });
    },
    check: () => {
      if (locked.current || !reference || initial.error) return;
      locked.current = true;
      uncertain.current = true;
      setMissing(false);
      recovery.mutate(reference);
    },
    retryExact: () => {
      if (
        locked.current ||
        !reference ||
        !missing ||
        attempt === undefined ||
        !canWrite ||
        initial.error
      )
        return;
      locked.current = true;
      setMissing(false);
      save.mutate({ id: reference.id, intent: attempt });
    },
  };
}
