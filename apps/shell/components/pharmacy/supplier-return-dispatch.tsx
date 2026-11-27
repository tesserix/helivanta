"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import { Field, LoadMore, RequestFailure, SelectControl, useLeaveGuard } from "@helivanta/ui";
import {
  ApiError,
  dispatchPharmacySupplierReturn,
  recoverPharmacySupplierReturnDispatch,
  pharmacySupplierReturnDispatchInputSchema,
  pharmacySupplierReturnKey,
  pharmacyActorHeaders,
  pharmacyLicencesRoot,
  newPharmacyId,
  useApiMutation,
  useApiPagedQuery,
  type PharmacyActorScope,
  type PharmacyLicence,
  type PharmacySupplierReturn,
  type PharmacySupplierReturnDispatchInput,
} from "@helivanta/api";
import { z } from "@/lib/csp-zod";

type Props = {
  scope: PharmacyActorScope;
  value: Pick<PharmacySupplierReturn, "id" | "number" | "state" | "version">;
  canStart: boolean;
  timeZone: string;
};
// The dispatch fields a person types, named as the form labels them.
const dispatchFieldNames: Record<string, string> = {
  document_reference: "outward reference",
  recipient_name: "recipient",
  reason: "reason",
};

export function supplierReturnDispatchStorageKey(scope: PharmacyActorScope, id: string) {
  return (
    "helivanta:supplier-return-dispatch:v1:" +
    JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId, id])
  );
}
export function SupplierReturnDispatch(props: Props) {
  return (
    <DispatchForm key={supplierReturnDispatchStorageKey(props.scope, props.value.id)} {...props} />
  );
}
function DispatchForm({ scope, value, canStart, timeZone }: Props) {
  const client = useQueryClient();
  const storageKey = supplierReturnDispatchStorageKey(scope, value.id);
  const [command, setCommand] = useState<string | null>(() => {
    try {
      const stored = sessionStorage.getItem(storageKey);
      return stored === null
        ? null
        : z.string().guid().safeParse(stored).success
          ? stored
          : "unavailable";
    } catch {
      return "unavailable";
    }
  });
  const [licence, setLicence] = useState("");
  const [reference, setReference] = useState("");
  const [recipient, setRecipient] = useState("");
  const [at, setAt] = useState("");
  const [reason, setReason] = useState("");
  const [attested, setAttested] = useState(false);
  const [notFound, setNotFound] = useState(false);
  const [verified, setVerified] = useState(false);
  const [failure, setFailure] = useState("");
  const startAllowed = canStart && value.state === "draft" && value.version === 0;
  const licences = useApiPagedQuery<PharmacyLicence>(
    pharmacySupplierReturnKey(scope, "dispatch-licences"),
    pharmacyLicencesRoot(scope),
    {
      enabled: startAllowed,
      request: { headers: pharmacyActorHeaders(scope), cache: "no-store" },
      gcTime: 0,
    },
  );
  const today = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
  const scopeMismatch = licences.items.some((row) => row.outlet_id !== scope.outletId);
  const choices = licences.items.filter(
    (row) =>
      row.decision === "approved" &&
      !!row.reviewed_evidence_file_id &&
      row.valid_from <= today &&
      row.valid_until >= today,
  );
  function confirmed() {
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      /* The verified server receipt remains authoritative. */
    }
    setVerified(true);
    setCommand(null);
    setFailure("");
    void client.invalidateQueries({ queryKey: pharmacySupplierReturnKey(scope) });
  }
  const dispatch = useApiMutation(
    (input: { id: string; body: PharmacySupplierReturnDispatchInput }) =>
      dispatchPharmacySupplierReturn(scope, value.id, input.id, input.body),
    {
      onSuccess: confirmed,
      suppressErrorToast: true,
      onError: () => {
        setFailure(
          "The dispatch result is unconfirmed. Check the original result before submitting again.",
        );
        void client.invalidateQueries({ queryKey: pharmacySupplierReturnKey(scope) });
      },
    },
  );
  const recover = useApiMutation(
    () => recoverPharmacySupplierReturnDispatch(scope, value.id, command!),
    {
      onSuccess: confirmed,
      suppressErrorToast: true,
      onError: (error) => {
        setNotFound(error instanceof ApiError && error.status === 404);
        setFailure(
          error instanceof ApiError && error.status === 404
            ? "No committed dispatch was found for this reference. Check again if the request may still be processing."
            : "The original dispatch result is unavailable. Keep this reference and check again.",
        );
      },
    },
  );
  const pending = dispatch.isPending || recover.isPending;
  useLeaveGuard(
    !verified && (!!reference || !!recipient || !!at || !!reason || !!licence || command !== null),
    `physical dispatch for return ${value.number}`,
    pending,
  );
  if (verified)
    return (
      <p role="status">
        Dispatch verified for return {value.number}. Supplier credit remains separate.
      </p>
    );
  if (!startAllowed && command === null) return null;
  const showForm = startAllowed && (command === null || notFound) && command !== "unavailable";
  return (
    <form
      className="mt-5 space-y-4 border-t pt-4"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        setFailure("");
        if (pending) return;
        if (
          scopeMismatch ||
          !startAllowed ||
          licences.isError ||
          licences.isPending ||
          !choices.some((row) => row.id === licence) ||
          !attested
        ) {
          setFailure("Choose a current reviewed licence and confirm the physical handoff.");
          return;
        }
        const parsedAt = new Date(at);
        if (!at || !Number.isFinite(parsedAt.getTime())) {
          setFailure("Enter the actual handoff time.");
          return;
        }
        const parsed = pharmacySupplierReturnDispatchInputSchema.safeParse({
          expected_version: 0,
          licence_id: licence,
          document_reference: reference,
          recipient_name: recipient,
          handed_over_at: parsedAt.toISOString(),
          physical_disposition: "handed_to_supplier",
          reason,
        });
        if (!parsed.success) {
          const tooLong = parsed.error.issues.find(
            (issue) => issue.code === "too_big" && String(issue.path[0]) in dispatchFieldNames,
          );
          setFailure(
            tooLong?.code === "too_big"
              ? `Shorten the ${dispatchFieldNames[String(tooLong.path[0])]} to at most ${tooLong.maximum} characters.`
              : "Complete the outward reference, recipient, handoff time and reason.",
          );
          return;
        }
        if (command !== null && !notFound) {
          setFailure("Check the original dispatch result first.");
          return;
        }
        const id = command ?? newPharmacyId();
        try {
          const cancellationKey =
            "helivanta:supplier-return-cancellation:v1:" +
            JSON.stringify([
              scope.tenantId,
              scope.subject,
              scope.pharmacyId,
              scope.outletId,
              value.id,
            ]);
          if (sessionStorage.getItem(cancellationKey) !== null) {
            setFailure("Verify the pending cancellation before recording a physical dispatch.");
            return;
          }
          sessionStorage.setItem(storageKey, id);
          if (sessionStorage.getItem(storageKey) !== id) throw new Error("storage");
        } catch {
          setFailure(
            "Recovery storage is unavailable. Restore browser storage before dispatching.",
          );
          return;
        }
        setCommand(id);
        setNotFound(false);
        dispatch.mutate({ id, body: parsed.data });
      }}
    >
      <h4 className="font-semibold">Physical supplier return dispatch</h4>
      <p className="text-sm text-muted-foreground">
        Record the complete return only after the units have been handed out from quarantine. This
        removes stock and does not record a supplier credit.
      </p>
      {failure && <p role="alert">{failure}</p>}
      {command && command !== "unavailable" && (
        <>
          <p role="status">Keep this return until its original dispatch result is verified.</p>
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={() => {
              setFailure("");
              recover.mutate();
            }}
          >
            Check dispatch result
          </Button>
        </>
      )}
      {command === "unavailable" && (
        <p role="alert">
          Recovery storage is unavailable. Restore browser storage and reload this return.
        </p>
      )}
      {showForm &&
        (licences.isError || scopeMismatch ? (
          <RequestFailure
            message="Reviewed outlet licences could not be verified for this outlet. Previously loaded choices are hidden."
            error={licences.error}
            retry={licences.retry}
          />
        ) : licences.isPending ? (
          <p role="status">Checking outlet licences…</p>
        ) : (
          <>
            <Field id="supplier-return-dispatch-licence" label="Reviewed outlet licence">
              <SelectControl
                id="supplier-return-dispatch-licence"
                value={licence}
                onValueChange={setLicence}
                disabled={pending}
                options={choices.map((row) => ({
                  value: row.id,
                  label: `${row.licence_number} · ${row.form_label} · valid until ${row.valid_until}`,
                }))}
              />
            </Field>
            {!choices.length && (
              <p className="text-sm">
                No current reviewed outlet licence is available. Complete licence review before
                dispatch.
              </p>
            )}
            <LoadMore
              label="Load more outlet licences"
              hasMore={licences.hasMore}
              isLoading={licences.isFetchingMore}
              onClick={licences.loadMore}
            />
            <Field id="supplier-return-outward-reference" label="Outward document reference">
              <Input
                id="supplier-return-outward-reference"
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                disabled={pending}
              />
            </Field>
            <Field id="supplier-return-recipient" label="Recipient name">
              <Input
                id="supplier-return-recipient"
                value={recipient}
                onChange={(event) => setRecipient(event.target.value)}
                disabled={pending}
              />
            </Field>
            <Field id="supplier-return-handoff" label="Handoff time (device time)">
              <Input
                id="supplier-return-handoff"
                type="datetime-local"
                step={0.001}
                value={at}
                onChange={(event) => setAt(event.target.value)}
                disabled={pending}
              />
            </Field>
            <p className="text-sm text-muted-foreground">
              Time is interpreted in your device time zone,{" "}
              {Intl.DateTimeFormat().resolvedOptions().timeZone}.
            </p>
            <Field id="supplier-return-dispatch-reason" label="Dispatch reason">
              <Input
                id="supplier-return-dispatch-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                disabled={pending}
              />
            </Field>
            <label className="flex items-start gap-3 text-sm">
              <input
                type="checkbox"
                checked={attested}
                onChange={(event) => setAttested(event.target.checked)}
                disabled={pending}
              />
              I confirm all return units were physically handed to this recipient.
            </label>
            <Button type="submit" disabled={pending || !choices.length}>
              {notFound ? "Retry original physical dispatch" : "Record physical dispatch"}
            </Button>
          </>
        ))}
    </form>
  );
}
