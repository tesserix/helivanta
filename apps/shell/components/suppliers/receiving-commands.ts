"use client";
import { useRef, useState } from "react";
import {
  ApiError,
  useApiMutation,
  hospitalSupplierReceivingKey,
  recordHospitalSupplierReceipt,
  recoverHospitalSupplierReceipt,
  correctHospitalSupplierReceipt,
  closeHospitalSupplierReceiving,
  recordHospitalSupplierClearance,
  releaseHospitalSupplierReceipt,
  recoverHospitalSupplierRelease,
  type HospitalSupplierReceivingScope,
  type ReceivingInput,
  type ReceivingCorrectionInput,
  type ReceivingClosureInput,
  type ReceivingClearanceInput,
  type ReceivingReleaseInput,
} from "@helivanta/api";
import { useLeaveGuard } from "@helivanta/ui";
export type ReceivingAttempt =
  | { kind: "delivery"; id: string; input: ReceivingInput }
  | { kind: "correction"; id: string; receipt: string; input: ReceivingCorrectionInput }
  | { kind: "closure"; id: string; input: ReceivingClosureInput }
  | { kind: "clearance"; id: string; receipt: string; input: ReceivingClearanceInput }
  | { kind: "release"; id: string; receipt: string; input: ReceivingReleaseInput };
export function useReceivingCommands(scope: HospitalSupplierReceivingScope, shipment: string) {
  const [retained, setRetained] = useState<ReceivingAttempt | null>(null),
    [message, setMessage] = useState<string | null>(null);
  const retainedRef = useRef<ReceivingAttempt | null>(null),
    working = useRef(false),
    uncertain = useRef(false);
  const mutation = useApiMutation(
    async ({
      attempt,
      recover,
    }: {
      attempt: ReceivingAttempt;
      recover: boolean;
    }): Promise<unknown> => {
      switch (attempt.kind) {
        case "delivery":
          return recover
            ? recoverHospitalSupplierReceipt(scope, shipment, attempt.id, attempt.input)
            : recordHospitalSupplierReceipt(scope, shipment, attempt.id, attempt.input);
        case "correction":
          return correctHospitalSupplierReceipt(
            scope,
            shipment,
            attempt.receipt,
            attempt.id,
            attempt.input,
          );
        case "closure":
          return closeHospitalSupplierReceiving(scope, shipment, attempt.id, attempt.input);
        case "clearance":
          return recordHospitalSupplierClearance(
            scope,
            shipment,
            attempt.receipt,
            attempt.id,
            attempt.input,
          );
        case "release":
          return recover
            ? recoverHospitalSupplierRelease(
                scope,
                shipment,
                attempt.receipt,
                attempt.id,
                attempt.input,
              )
            : releaseHospitalSupplierReceipt(
                scope,
                shipment,
                attempt.receipt,
                attempt.id,
                attempt.input,
              );
      }
    },
    {
      invalidate: [[...hospitalSupplierReceivingKey(scope)]],
      suppressErrorToast: true,
      invalidateOnError: true,
    },
  );
  useLeaveGuard(!!retained, "unconfirmed hospital receiving command", !!retained);
  async function run(attempt: ReceivingAttempt, recover = false) {
    if (working.current || (retainedRef.current && retainedRef.current !== attempt)) return false;
    retainedRef.current = attempt;
    setRetained(attempt);
    working.current = true;
    setMessage(null);
    const knownBefore = uncertain.current;
    try {
      await mutation.mutateAsync({ attempt, recover });
      retainedRef.current = null;
      setRetained(null);
      uncertain.current = false;
      setMessage(
        attempt.kind === "delivery"
          ? "Original delivery recorded and current receiving refreshed."
          : attempt.kind === "release"
            ? "Original physical release verified. Its stock movements were recorded by PharmaConnect."
            : attempt.kind === "clearance"
              ? "Independent clearance recorded. Physical stock has not been released by this review."
              : attempt.kind === "closure"
                ? "Receiving closure recorded with current shortages."
                : "Original correction verified. Original receipt evidence remains unchanged.",
      );
      return true;
    } catch (error) {
      if (
        !knownBefore &&
        error instanceof ApiError &&
        [400, 403, 409, 422].includes(error.status)
      ) {
        retainedRef.current = null;
        setRetained(null);
        uncertain.current = false;
      } else {
        uncertain.current = true;
        setMessage(
          "The original receiving command is retained but its outcome is unconfirmed. Recheck current authority and recover or retry the same request before taking another action.",
        );
      }
      return false;
    } finally {
      working.current = false;
    }
  }
  return {
    retained,
    frozen: !!retained || mutation.isPending,
    pending: mutation.isPending,
    error: mutation.error,
    message,
    run,
    reset: mutation.reset,
  };
}
export type ReceivingCommands = ReturnType<typeof useReceivingCommands>;
