"use client";
import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  newPharmacyId,
  pharmacyAffiliationDecisionSchema,
  useApiMutation,
  useApiPagedQuery,
  type PharmacyAffiliationReview,
} from "@helivanta/api";
import { ConfirmDialog, Field, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";

type Outcome = z.infer<typeof pharmacyAffiliationDecisionSchema>["outcome"];

const statusLabel: Record<PharmacyAffiliationReview["status"], string> = {
  pending: "Waiting for your decision",
  accepted: "Verified",
  declined: "Declined",
  revoked: "Revoked",
  withdrawn: "Withdrawn by the pharmacy",
};

/** Pharmacies asking this hospital to verify their affiliation (#1416). */
export function PharmacyAffiliationReviews({ hospital }: { hospital: { id: string; name: string } }) {
  const key = ["patients", "pharmacy-affiliations", hospital.id];
  const reviews = useApiPagedQuery<PharmacyAffiliationReview>(
    key,
    `/patients/pharmacy-affiliations?hospital_id=${hospital.id}`,
  );
  return (
    <Panel className="mt-6" aria-labelledby="pharmacy-affiliations">
      <h3 id="pharmacy-affiliations" className="font-semibold">
        Pharmacy affiliations
      </h3>
      <p className="mt-1 text-sm leading-6 text-muted-foreground">
        Pharmacies that say they belong to or serve {hospital.name}. Verify only a pharmacy you know
        is linked to this hospital. You can revoke a verification later.
      </p>
      {reviews.isError ? (
        <RequestFailure message="Could not load pharmacy requests." retry={reviews.retry} />
      ) : reviews.items.length === 0 && !reviews.isPending ? (
        <p className="mt-4 text-sm text-muted-foreground">No pharmacy has asked yet.</p>
      ) : (
        <ul className="mt-4 divide-y text-sm">
          {reviews.items.map((review) => (
            <li key={review.request_id} className="py-3">
              <ReviewItem review={review} hospital={hospital} invalidate={key} />
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more pharmacy requests"
        hasMore={reviews.hasMore}
        isLoading={reviews.isFetchingMore}
        onClick={reviews.loadMore}
      />
    </Panel>
  );
}

function ReviewItem({
  review,
  hospital,
  invalidate,
}: {
  review: PharmacyAffiliationReview;
  hospital: { id: string; name: string };
  invalidate: unknown[];
}) {
  // One decision id per attempt, so a retried click is the same decision.
  const [decisionId, setDecisionId] = useState(() => newPharmacyId());
  const [confirmRevoke, setConfirmRevoke] = useState(false);
  const form = useZodForm(pharmacyAffiliationDecisionSchema, {
    outcome: review.status === "accepted" ? "revoked" : "accepted",
    reason: "",
  });
  const put = (values: z.infer<typeof pharmacyAffiliationDecisionSchema>) =>
    apiFetch(
      `/patients/pharmacy-affiliations/${review.request_id}/decision/${decisionId}?hospital_id=${hospital.id}`,
      { method: "PUT", body: JSON.stringify(values) },
    );
  const done = {
    invalidate: [invalidate],
    invalidateOnError: true,
    onSuccess: () => {
      setDecisionId(newPharmacyId());
      setConfirmRevoke(false);
      form.reset();
    },
    suppressErrorToast: true,
  };
  // One mutation per button, so each toast repeats its button's verb.
  const mutations: Record<Outcome, ReturnType<typeof useApiMutation<unknown, z.infer<typeof pharmacyAffiliationDecisionSchema>>>> = {
    accepted: useApiMutation(put, { ...done, successToast: "Pharmacy verified" }),
    declined: useApiMutation(put, { ...done, successToast: "Request declined" }),
    revoked: useApiMutation(put, { ...done, successToast: "Verification revoked" }),
  };
  const pending = Object.values(mutations).some((m) => m.isPending);
  const failed = Object.values(mutations).find((m) => m.isError)?.error;
  const submit = (outcome: Outcome) => {
    form.setValue("outcome", outcome);
    void form.handleSubmit((values) =>
      outcome === "revoked" ? setConfirmRevoke(true) : mutations[outcome].mutate(values),
    )();
  };
  const decidable = review.status === "pending" || review.status === "accepted";
  const latest = review.decisions.at(-1);
  return (
    <div>
      <p className="font-medium">{review.pharmacy_name}</p>
      <p className="mt-1 text-muted-foreground">
        {review.relationship} · {statusLabel[review.status]}
        {latest && ` · ${latest.reason}`}
      </p>
      {decidable && (
        <form
          noValidate
          className="mt-3 flex flex-wrap items-end gap-3"
          onSubmit={(event) => event.preventDefault()}
        >
          <Field
            id={`affiliation-reason-${review.request_id}`}
            label="Reason"
            error={form.formState.errors.reason?.message}
          >
            <Input id={`affiliation-reason-${review.request_id}`} {...form.register("reason")} />
          </Field>
          {review.status === "pending" ? (
            <>
              <Button type="button" size="sm" disabled={pending} onClick={() => submit("accepted")}>
                Verify
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={pending}
                onClick={() => submit("declined")}
              >
                Decline
              </Button>
            </>
          ) : (
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={pending}
              onClick={() => submit("revoked")}
            >
              Revoke
            </Button>
          )}
        </form>
      )}
      {failed && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {failed.message}
        </p>
      )}
      <ConfirmDialog
        open={confirmRevoke}
        onOpenChange={setConfirmRevoke}
        title={`Revoke ${review.pharmacy_name}?`}
        description="The pharmacy's verified link to this hospital ends now. It must ask again to be verified."
        confirmLabel="Revoke"
        busy={mutations.revoked.isPending}
        onConfirm={() =>
          mutations.revoked.mutate({ outcome: "revoked", reason: form.getValues("reason") })
        }
      />
    </div>
  );
}
