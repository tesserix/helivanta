"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  staffPayPath,
  taxRegimeSchemaFor,
  taxYearOf,
  toTaxRegimeInput,
  useApiMutation,
  useApiPagedQuery,
  TAX_REGIMES,
  type TaxRegimeElection,
  type TaxRegimeInput,
  type TaxRegimeRevision,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { RevisionHistory } from "./payroll-workspace";

const regimesKey = (hospital: string, staff: string) => [
  "workforce",
  "tax-regimes",
  hospital,
  staff,
];

export function TaxRegimes({
  hospital,
  base,
  staff,
  writable,
}: {
  hospital: string;
  base: string;
  staff: string;
  writable: boolean;
}) {
  const path = `${staffPayPath(base, staff)}/tax-regimes`;
  const elections = useApiPagedQuery<TaxRegimeElection>(regimesKey(hospital, staff), path);
  const [open, setOpen] = useState<{
    view: "record" | "history";
    election?: TaxRegimeElection;
  } | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const show = (next: typeof open, ev: React.MouseEvent<HTMLButtonElement>) => {
    opener.current = ev.currentTarget;
    setOpen(next);
  };
  const close = () => {
    setOpen(null);
    opener.current?.focus();
  };
  return (
    <div role="region" aria-label="Income-tax regime" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h4 className="font-semibold">Income-tax regime</h4>
          <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
            Record the regime the employee intimated for each tax year. Where none is recorded,
            payroll reports it rather than choosing one.
          </p>
        </div>
        {writable && (
          <Button variant="outline" onClick={(ev) => show({ view: "record" }, ev)}>
            Record election
          </Button>
        )}
      </div>
      {elections.isPending ? (
        <p role="status">Loading elections…</p>
      ) : elections.isError ? (
        <Failure message="Could not load regime elections." retry={elections.retry} />
      ) : elections.items.length === 0 ? (
        <p className="text-sm text-muted-foreground">No regime recorded for any tax year.</p>
      ) : (
        <ul className="divide-y rounded-xl border">
          {elections.items.map((e) => (
            <li
              key={e.id}
              className="flex flex-wrap items-center justify-between gap-3 p-4 text-sm"
            >
              <div>
                <p className="font-medium">
                  {e.tax_year} · {TAX_REGIMES[e.regime]}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Intimated {e.intimated_on} · v{e.version}
                </p>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  aria-label={`History of ${e.tax_year} election`}
                  onClick={(ev) => show({ view: "history", election: e }, ev)}
                >
                  History
                </Button>
                {writable && (
                  <Button
                    variant="outline"
                    aria-label={`Change ${e.tax_year} election`}
                    onClick={(ev) => show({ view: "record", election: e }, ev)}
                  >
                    Change
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {elections.hasMore && (
        <Button
          className="mt-3"
          variant="outline"
          disabled={elections.isFetchingMore}
          onClick={elections.loadMore}
        >
          Load more elections
        </Button>
      )}
      {open?.view === "record" && (
        <RegimeEditor
          key={open.election?.id ?? "new"}
          hospital={hospital}
          staff={staff}
          path={path}
          current={open.election}
          onClose={close}
        />
      )}
      {open?.view === "history" && open.election && (
        <RevisionHistory<TaxRegimeRevision>
          key={open.election.id}
          label={`${open.election.tax_year} election history`}
          queryKey={[...regimesKey(hospital, staff), "history", open.election.tax_year]}
          path={`${path}/${open.election.tax_year}/history`}
          rowKey={(r) => r.id}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                v{r.version} · {TAX_REGIMES[r.regime]} · intimated {r.intimated_on}
              </p>
              <p className="mt-1 text-muted-foreground">
                Recorded by {r.recorded_by_subject}
                {r.note && ` · “${r.note}”`}
              </p>
            </>
          )}
        />
      )}
    </div>
  );
}

function RegimeEditor({
  hospital,
  staff,
  path,
  current,
  onClose,
}: {
  hospital: string;
  staff: string;
  path: string;
  current?: TaxRegimeElection;
  onClose: () => void;
}) {
  const schema = useMemo(() => taxRegimeSchemaFor(current), [current]);
  const form = useZodForm(schema, {
    tax_year: current?.tax_year ?? taxYearOf(today()),
    regime: current?.regime ?? ("" as TaxRegimeElection["regime"]),
    intimated_on: current?.intimated_on ?? today(),
    note: "",
  });
  const save = useApiMutation(
    ({ year, input }: { year: string; input: TaxRegimeInput }) =>
      apiFetch<TaxRegimeElection>(`${path}/${encodeURIComponent(year)}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [regimesKey(hospital, staff)],
      successToast: current ? "Election changed" : "Election recorded",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const errors = form.formState.errors;
  const value = form.watch();
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={current ? `Change ${current.tax_year} election` : "Record election"}
      onSubmit={form.handleSubmit((input) =>
        save.mutate({
          year: input.tax_year,
          input: toTaxRegimeInput(input, current?.version ?? 0),
        }),
      )}
      className="mt-4 space-y-4 rounded-xl border bg-muted/10 p-4"
    >
      <h5 ref={heading} tabIndex={-1} className="text-sm font-semibold outline-none">
        {current ? `Change ${current.tax_year} election` : "Record election"}
      </h5>
      <fieldset disabled={save.isPending} className="grid gap-4 sm:grid-cols-3">
        <Field id="tax-regime-year" label="Tax year" error={errors.tax_year?.message}>
          <input
            id="tax-regime-year"
            className={inputClass}
            disabled={!!current}
            placeholder="2026-27"
            aria-invalid={!!errors.tax_year}
            {...form.register("tax_year")}
          />
        </Field>
        <Field id="tax-regime-regime" label="Regime" error={errors.regime?.message}>
          <SelectControl
            id="tax-regime-regime"
            value={value.regime}
            aria-invalid={!!errors.regime}
            placeholder="Choose a regime"
            onValueChange={(next) =>
              form.setValue("regime", next as TaxRegimeElection["regime"], {
                shouldDirty: true,
                shouldValidate: form.formState.isSubmitted,
              })
            }
            options={Object.entries(TAX_REGIMES).map(([r, label]) => ({ value: r, label }))}
          />
        </Field>
        <Field id="tax-regime-intimated" label="Intimated on" error={errors.intimated_on?.message}>
          <input
            id="tax-regime-intimated"
            type="date"
            className={inputClass}
            aria-invalid={!!errors.intimated_on}
            {...form.register("intimated_on")}
          />
        </Field>
        <div className="sm:col-span-3">
          <Field
            id="tax-regime-note"
            label={current ? "Reason for the change" : "Note (optional)"}
            error={errors.note?.message}
          >
            <textarea
              id="tax-regime-note"
              rows={2}
              className={inputClass}
              aria-invalid={!!errors.note}
              {...form.register("note")}
            />
          </Field>
        </div>
      </fieldset>
      {save.isError && (
        <SaveError
          message={`${save.error.message} Cancel and reload to see the current election.`}
        />
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : current ? "Change election" : "Record election"}
        </Button>
      </div>
    </form>
  );
}
