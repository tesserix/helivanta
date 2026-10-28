"use client";
import { useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  affiliationWithdrawSchema,
  apiFetch,
  newPharmacyId,
  useApiMutation,
  type AffiliationVerificationStatus,
  type PharmacyHospitalAffiliation,
} from "@helivanta/api";
import { ConfirmDialog, Field, useZodForm } from "@helivanta/ui";

const statusLabel: Record<AffiliationVerificationStatus, string> = {
  pending_validation: "Not verified · No access granted",
  requested: "Waiting for the hospital to verify · No access granted",
  verified: "Verified by the hospital",
  declined: "Declined by the hospital · No access granted",
  revoked: "Revoked by the hospital · No access granted",
  withdrawn: "Withdrawn · No access granted",
};

/** One declared hospital link and its verification controls (#1416). */
export function AffiliationVerification({
  link,
  path,
  invalidate,
  canManage,
}: {
  link: PharmacyHospitalAffiliation;
  /** The affiliation's own API path. */
  path: string;
  invalidate: unknown[];
  canManage: boolean;
}) {
  // One id per attempt: a retried click repeats the same round.
  const [requestId, setRequestId] = useState(() => newPharmacyId());
  const [withdrawing, setWithdrawing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const form = useZodForm(affiliationWithdrawSchema, { reason: "" });
  const ask = useApiMutation(
    () =>
      apiFetch(`${path}/verification-requests/${requestId}`, { method: "PUT", body: "{}" }),
    {
      successToast: "Verification requested",
      invalidate: [invalidate],
      onSuccess: () => setRequestId(newPharmacyId()),
      suppressErrorToast: true,
    },
  );
  const withdraw = useApiMutation(
    (reason: string) =>
      apiFetch(`${path}/verification-requests/${link.verification_request_id}/withdraw`, {
        method: "POST",
        body: JSON.stringify({ reason }),
      }),
    {
      successToast: "Verification withdrawn",
      invalidate: [invalidate],
      onSuccess: () => {
        setConfirming(false);
        setWithdrawing(false);
        form.reset();
      },
      suppressErrorToast: true,
    },
  );
  const open = link.verification_status === "requested" || link.verification_status === "verified";
  const error = ask.error ?? withdraw.error;
  return (
    <div>
      <p className="break-all font-medium">{link.hospital_id}</p>
      <p className="mt-1 text-muted-foreground">
        {link.relationship} · {statusLabel[link.verification_status]}
      </p>
      {canManage && !open && (
        <Button
          className="mt-2"
          variant="outline"
          size="sm"
          disabled={ask.isPending}
          onClick={() => ask.mutate()}
        >
          {ask.isPending ? "Requesting…" : "Ask the hospital to verify"}
        </Button>
      )}
      {canManage && open && link.verification_request_id && !withdrawing && (
        <Button className="mt-2" variant="outline" size="sm" onClick={() => setWithdrawing(true)}>
          Withdraw verification
        </Button>
      )}
      {canManage && open && withdrawing && (
        <form
          noValidate
          className="mt-3 flex flex-wrap items-end gap-3"
          onSubmit={form.handleSubmit(() => setConfirming(true))}
        >
          <Field
            id={`withdraw-${link.id}`}
            label="Reason for withdrawing"
            error={form.formState.errors.reason?.message}
          >
            <Input id={`withdraw-${link.id}`} {...form.register("reason")} />
          </Field>
          <Button type="submit" variant="destructive" size="sm">
            Withdraw
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => setWithdrawing(false)}>
            Cancel
          </Button>
        </form>
      )}
      {error && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {error.message}
        </p>
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Withdraw this hospital link?"
        description="The hospital's verification ends now. Asking again starts a new review."
        confirmLabel="Withdraw"
        busy={withdraw.isPending}
        onConfirm={() => withdraw.mutate(form.getValues("reason"))}
      />
    </div>
  );
}
