"use client";

import { useEffect, useId, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  onTenantTransition,
  tenantTransitionPending,
  readPrescriptionFulfilmentReview,
  PrescriptionFulfilmentReviewInputContract,
  type PrescriptionReviewContext,
  type PharmacyScope,
} from "@helivanta/api";
import { RequestFailure } from "@helivanta/ui";

type Props = { scope: PharmacyScope & { subject: string }; source: PrescriptionReviewContext };
export function PrescriptionFulfilmentReview({ scope, source }: Props) {
  if (
    source.owner_tenant_id !== scope.tenantId ||
    source.pharmacy_id !== scope.pharmacyId ||
    source.record.outlet_id !== scope.outletId ||
    source.reader_subject !== scope.subject
  ) {
    return <p role="status">Reopen the recorded prescription in your current workspace.</p>;
  }
  return (
    <ProposedFill
      key={`${scope.tenantId}:${scope.subject}:${scope.pharmacyId}:${scope.outletId}:${source.source_state_hash}`}
      scope={scope}
      source={source}
    />
  );
}
function ProposedFill({ scope, source }: Props) {
  const id = useId();
  const [quantities, setQuantities] = useState<Record<number, string>>({});
  const [error, setError] = useState("");
  const [changed, setChanged] = useState(tenantTransitionPending);
  const mutation = useMutation({
    mutationFn: (input: Parameters<typeof readPrescriptionFulfilmentReview>[2]) =>
      readPrescriptionFulfilmentReview(scope, source.record.id, input),
  });
  const reset = mutation.reset;
  useEffect(
    () =>
      onTenantTransition(() => {
        if (tenantTransitionPending()) {
          setChanged(true);
          setQuantities({});
          reset();
        }
      }),
    [reset],
  );
  function check() {
    const parsed = PrescriptionFulfilmentReviewInputContract.safeParse({
      source_state_hash: source.source_state_hash,
      lines: source.record.lines
        .filter((line) => quantities[line.line_number]?.trim())
        .map((line) => ({
          line_number: line.line_number,
          quantity_units: Number(quantities[line.line_number]),
        })),
    });
    if (
      !parsed.success ||
      parsed.data.lines.some((line) => {
        const recorded = source.record.lines.find((row) => row.line_number === line.line_number);
        return (
          !recorded ||
          recorded.dispensings_remaining < 1 ||
          line.quantity_units > recorded.units_remaining
        );
      })
    ) {
      setError("Choose positive whole-unit quantities within the recorded remaining allowance.");
      return;
    }
    setError("");
    mutation.mutate(parsed.data);
  }
  if (changed)
    return (
      <p role="status">
        Your account changed. Reopen the prescription before proposing quantities.
      </p>
    );
  const stale = mutation.error instanceof ApiError && mutation.error.status === 409;
  return (
    <section aria-label="Proposed supply for review" className="space-y-3 rounded-md border p-4">
      <h4 className="font-semibold">Proposed supply for clinical review</h4>
      <p className="text-sm text-muted-foreground">
        Choose units explicitly. This checks prescription allowance only. It does not approve supply
        or confirm stock availability.
      </p>
      <form
        noValidate
        className="space-y-3"
        onSubmit={(event) => {
          event.preventDefault();
          check();
        }}
      >
        {source.record.lines.map((line) => (
          <div key={line.line_number}>
            <label htmlFor={`${id}-${line.line_number}`}>
              Proposed units for {line.prescribed_as}
            </label>
            <Input
              id={`${id}-${line.line_number}`}
              type="number"
              min="1"
              max={line.units_remaining}
              step="1"
              value={quantities[line.line_number] ?? ""}
              disabled={
                mutation.isPending || line.units_remaining < 1 || line.dispensings_remaining < 1
              }
              onChange={(event) => {
                setQuantities((current) => ({
                  ...current,
                  [line.line_number]: event.target.value,
                }));
                setError("");
                reset();
              }}
            />
            <p className="text-sm text-muted-foreground">
              {line.units_remaining} units and {line.dispensings_remaining} permitted fills remain.
              Leave blank to omit this line.
            </p>
          </div>
        ))}
        {error && <p role="alert">{error}</p>}
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? "Checking proposed quantities…" : "Check proposed quantities"}
        </Button>
      </form>
      {stale ? (
        <p role="alert">
          The prescription allowance changed. Refresh recorded details before proposing supply
          again.
        </p>
      ) : mutation.isError ? (
        <RequestFailure
          message="Could not check the proposed quantities. No supply has been approved."
          error={mutation.error}
          retry={check}
        />
      ) : null}
      {mutation.data && !mutation.isPending && !mutation.isError && (
        <div className="space-y-2">
          <p role="status">Proposed quantities checked against this recorded source.</p>
          <ul>
            {mutation.data.lines.map((line) => (
              <li key={line.line_number}>
                {
                  source.record.lines.find((row) => row.line_number === line.line_number)
                    ?.prescribed_as
                }
                : {line.quantity_units} units proposed
              </li>
            ))}
          </ul>
          <p className="text-sm">
            Clinical authority has not been evaluated. Stock has not been reserved. Qualified
            approval and dispensing checks remain required.
          </p>
        </div>
      )}
    </section>
  );
}
