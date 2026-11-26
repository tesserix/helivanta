"use client";

import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import { usePrescriptionReviewContext, type PharmacyScope } from "@helivanta/api";
import { Panel, RefreshButton, RequestFailure } from "@helivanta/ui";

export function PrescriptionReviewContext({
  scope,
  prescriptionId,
  onClose,
}: {
  scope: PharmacyScope & { subject: string };
  prescriptionId: string;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  const focused = useRef(false);
  const review = usePrescriptionReviewContext(scope, prescriptionId);

  const view =
    !review.contextChanged && !review.isFetching && !review.isError ? review.data : undefined;
  useEffect(() => {
    if (view && !focused.current) {
      heading.current?.focus();
      focused.current = true;
    }
  }, [view]);

  return (
    <Panel aria-label="Recorded prescription review">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <h3 ref={heading} tabIndex={-1} className="text-lg font-semibold outline-none">
          {view
            ? `Recorded prescription for ${view.record.patient_name}`
            : "Recorded prescription review"}
        </h3>
        <Button type="button" variant="outline" onClick={onClose}>
          Close prescription review
        </Button>
      </div>
      <p role="note" className="mt-3 rounded-md border p-3 text-sm">
        This is the pharmacy’s recorded copy. Viewing it does not approve supply. The signed
        clinical source and pharmacist eligibility have not been verified.
      </p>
      {review.contextChanged ? (
        <p role="status" className="mt-4">
          Your workspace changed. Close this review and reopen the prescription to check the current
          account.
        </p>
      ) : review.isPending || review.isFetching ? (
        <p role="status" className="mt-4">
          Loading recorded prescription…
        </p>
      ) : review.isError ? (
        <div className="mt-4">
          <RequestFailure
            message="Could not load the recorded prescription. No clinical verification has been recorded."
            error={review.error}
            retry={review.refetch}
            recheck="Recheck access"
          />
        </div>
      ) : view ? (
        <div className="mt-5 space-y-5">
          <section aria-label="Recorded source details">
            <dl className="grid gap-4 sm:grid-cols-2">
              <div>
                <dt className="text-sm text-muted-foreground">Prescriber</dt>
                <dd>{view.record.prescriber_name}</dd>
                <dd className="text-sm">
                  {view.record.prescriber_council} · {view.record.prescriber_registration}
                </dd>
              </div>
              <div>
                <dt className="text-sm text-muted-foreground">Prescription date</dt>
                <dd>
                  <time dateTime={view.record.prescribed_on}>{view.record.prescribed_on}</time>
                </dd>
              </div>
              <div>
                <dt className="text-sm text-muted-foreground">Patient address</dt>
                <dd>{view.record.patient_address ?? "Patient address not recorded."}</dd>
              </div>
              <div>
                <dt className="text-sm text-muted-foreground">Recorded at</dt>
                <dd>
                  <time dateTime={view.record.created_at}>
                    {new Intl.DateTimeFormat(undefined, {
                      dateStyle: "medium",
                      timeStyle: "short",
                      timeZone: "UTC",
                    }).format(new Date(view.record.created_at))}{" "}
                    UTC
                  </time>
                </dd>
              </div>
              <div>
                <dt className="text-sm text-muted-foreground">Prescriber address</dt>
                <dd>{view.record.prescriber_address ?? "Prescriber address not recorded."}</dd>
              </div>
            </dl>
          </section>
          <section aria-label="Prescribed lines">
            <h4 className="font-semibold">As prescribed</h4>
            <ol className="mt-2 divide-y">
              {view.record.lines.map((line) => (
                <li key={line.line_number} className="py-3">
                  <p className="font-medium">{line.prescribed_as}</p>
                  <p className="text-sm">
                    {line.units_remaining} of {line.quantity_units} units remain in the recorded
                    allowance.
                  </p>
                  <p className="text-sm">
                    {line.dispensings_remaining} of {line.dispensings_allowed} recorded dispensings
                    remain.
                  </p>
                </li>
              ))}
            </ol>
            <p className="text-sm text-muted-foreground">
              These are recorded prescription allowances, not stock availability or permission to
              supply. Proposed supply and substitutions are not available in this view.
            </p>
          </section>
          <section aria-label="Clinical context availability">
            <h4 className="font-semibold">Clinical context</h4>
            <p className="mt-1 text-sm text-muted-foreground">
              Unavailable information must be checked with the clinical source. It does not mean no
              allergies, medicines or safety concerns.
            </p>
            <dl className="mt-3 grid gap-3 sm:grid-cols-2">
              {[
                "Allergies",
                "Active medicines",
                "Age",
                "Weight",
                "Clinical and schedule flags",
              ].map((label) => (
                <div key={label}>
                  <dt className="text-sm text-muted-foreground">{label}</dt>
                  <dd>Unavailable</dd>
                </div>
              ))}
            </dl>
          </section>
          <RefreshButton refresh={review.refetch} error={review.error} busy={review.isFetching}>
            Refresh recorded details
          </RefreshButton>
        </div>
      ) : null}
    </Panel>
  );
}
