"use client";

import { ShieldCheck } from "lucide-react";
import {
  formatZonedDateTime,
  pharmacyKey,
  pharmacyProductSchedulePath,
  useApiQuery,
  type PharmacyProduct,
  type PharmacyScheduleDeclaration,
  type PharmacyScheduleState,
  type PharmacyScope,
} from "@helivanta/api";
import { RequestFailure } from "@helivanta/ui";
import type { FormState } from "@/lib/use-list-detail";

// A product's schedule class (#1636): declared by one person from a source
// such as the pack label, approved by another. Release and sale need it.

/** The API returns at most this many declarations (`historyLimit`). */
const HISTORY_LIMIT = 50;

export function scheduleLabel(d: Pick<PharmacyScheduleDeclaration, "regulatory_class" | "ndps">) {
  const name = d.regulatory_class === "none" ? "Not scheduled" : `Schedule ${d.regulatory_class}`;
  return `${name} · ${d.ndps ? "NDPS" : "not NDPS"}`;
}

export function ProductScheduleSection({
  scope,
  product,
  timeZone,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct;
  timeZone: string;
  canDeclare: boolean;
  canReview: boolean;
  onStateChange?: FormState;
  onReviewStateChange?: FormState;
}) {
  const state = useApiQuery<PharmacyScheduleState>(
    pharmacyKey(scope, "product-schedule", product.id),
    pharmacyProductSchedulePath(scope, product.id),
  );
  const current = state.data;
  const inForce = current?.in_force;
  return (
    <section aria-labelledby="product-schedule-heading" className="mt-8 border-t pt-6">
      <h4 id="product-schedule-heading" className="mb-1 flex items-center gap-2 font-medium">
        <ShieldCheck className="size-4" />
        Schedule class
      </h4>
      {state.isError ? (
        <RequestFailure
          message="Could not load the schedule class."
          retry={state.refetch}
          error={state.error}
        />
      ) : !current ? (
        <p role="status" className="text-sm text-muted-foreground">
          Loading schedule class…
        </p>
      ) : (
        <div className="space-y-4">
          {inForce ? (
            <p className="text-sm text-muted-foreground">
              {`${scheduleLabel(inForce)}, approved ${formatZonedDateTime(inForce.reviewed_at ?? inForce.created_at, timeZone)}`}
            </p>
          ) : (
            <div className="space-y-1 text-sm">
              <p>{`No schedule class in force: ${current.reason}`}</p>
              <p className="text-muted-foreground">
                Until one is approved, this product cannot be released or sold.
              </p>
            </div>
          )}
          {inForce && (inForce.regulatory_class === "X" || inForce.ndps) && (
            <p className="text-sm text-muted-foreground">
              Schedule X and NDPS products cannot be released or sold until their controls exist.
            </p>
          )}
          {current.history.length > 0 && (
            <ol className="space-y-2" aria-label="Schedule class history">
              {current.history.map((d) => (
                <li key={d.id} className="rounded-md bg-muted p-3 text-sm">
                  <p className="font-medium">{scheduleLabel(d)}</p>
                  <p className="mt-0.5 text-muted-foreground">
                    {d.source} · declared by {d.declared_by},{" "}
                    {formatZonedDateTime(d.created_at, timeZone)}
                  </p>
                  {d.decision !== null && (
                    <p className="mt-0.5 text-xs">
                      {`${d.decision === "approved" ? "Approved" : "Rejected"} by ${d.reviewed_by}: ${d.decision_reason}`}
                    </p>
                  )}
                </li>
              ))}
            </ol>
          )}
          {current.history.length >= HISTORY_LIMIT && (
            <p className="text-xs text-muted-foreground">
              Showing the latest {HISTORY_LIMIT} declarations.
            </p>
          )}
        </div>
      )}
    </section>
  );
}
