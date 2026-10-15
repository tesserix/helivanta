"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatHalfDays,
  leaveCorrectionSchema,
  useApiMutation,
  useApiPagedQuery,
  LEAVE_LEDGER_KINDS,
  type Hospital,
  type LeaveCorrectionInput,
  type LeaveLedgerEntry,
  type LeaveYear,
  type StaffLeaveBalances,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "./years-panel";

export function LeaveBalancesPanel({
  hospital,
  years,
  correctable,
}: {
  hospital: Hospital;
  years: LeaveYear[];
  correctable: boolean;
}) {
  const [selected, setSelected] = useState<number | null>(null);
  const [asOf, setAsOf] = useState("");
  const year = years.find((y) => y.leave_year === selected) ?? years[0];
  return (
    <section
      aria-label="Staff leave balances"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Staff leave balances</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Balances are summed from the ledger up to the chosen date.
          </p>
        </div>
        {year && (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field id="balances-year" label="Leave year">
              <SelectControl
                id="balances-year"
                value={String(year.leave_year)}
                onValueChange={(v) => {
                  setSelected(Number(v));
                  setAsOf("");
                }}
                options={years.map((y) => ({
                  value: String(y.leave_year),
                  label: `${y.leave_year}${y.closed ? " · closed" : ""}`,
                }))}
              />
            </Field>
            <Field id="balances-as-of" label="Balance as of">
              <input
                id="balances-as-of"
                type="date"
                className={inputClass}
                min={year.start_on}
                max={year.end_on}
                value={asOf}
                onChange={(e) => setAsOf(e.target.value)}
              />
            </Field>
          </div>
        )}
      </div>
      {year ? (
        <BalanceList
          key={`${year.leave_year}:${asOf}`}
          hospital={hospital.id}
          year={year}
          asOf={asOf >= year.start_on && asOf <= year.end_on ? asOf : ""}
          correctable={correctable && !year.closed}
        />
      ) : (
        <p className="p-5 text-sm text-muted-foreground">
          Open a leave year to see staff balances.
        </p>
      )}
    </section>
  );
}

function BalanceList({
  hospital,
  year,
  asOf,
  correctable,
}: {
  hospital: string;
  year: LeaveYear;
  asOf: string;
  correctable: boolean;
}) {
  const base = `/workforce/hospitals/${hospital}`;
  const balances = useApiPagedQuery<StaffLeaveBalances>(
    ["workforce", "leave-balances", hospital, year.leave_year, asOf],
    `${base}/hr/leave-balances?leave_year=${year.leave_year}${asOf ? `&as_of=${asOf}` : ""}`,
  );
  const [open, setOpen] = useState<StaffLeaveBalances | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const close = () => {
    setOpen(null);
    opener.current?.focus();
  };
  return (
    <>
      {balances.isPending ? (
        <p role="status" className="p-5">
          Loading balances…
        </p>
      ) : balances.isError ? (
        <div className="p-5">
          <Failure message="Could not load leave balances." retry={balances.retry} />
        </div>
      ) : balances.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          No staff have a leave structure in this hospital.
        </p>
      ) : (
        <ul className="divide-y">
          {balances.items.map((s) => (
            <li key={s.staff_id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {s.display_name} · {s.employee_code}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {s.structure_name} · {s.active ? "Active" : "Ended"}
                </p>
                <ul className="mt-3 flex flex-wrap gap-2 text-xs">
                  {s.balances.map((b) => (
                    <li key={b.leave_type_id} className="rounded-full bg-muted px-2.5 py-1">
                      {b.name}: {formatHalfDays(b.half_days)}
                    </li>
                  ))}
                </ul>
              </div>
              <Button
                variant="outline"
                aria-label={`Ledger for ${s.display_name}`}
                onClick={(e) => {
                  opener.current = e.currentTarget;
                  setOpen(s);
                }}
              >
                Ledger
              </Button>
            </li>
          ))}
        </ul>
      )}
      {balances.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={balances.isFetchingMore} onClick={balances.loadMore}>
            Load more staff
          </Button>
        </div>
      )}
      {open && (
        <StaffLedger
          key={open.staff_id}
          base={base}
          hospital={hospital}
          year={year}
          staff={open}
          correctable={correctable}
          onClose={close}
        />
      )}
    </>
  );
}

function StaffLedger({
  base,
  hospital,
  year,
  staff,
  correctable,
  onClose,
}: {
  base: string;
  hospital: string;
  year: LeaveYear;
  staff: StaffLeaveBalances;
  correctable: boolean;
  onClose: () => void;
}) {
  const ledger = useApiPagedQuery<LeaveLedgerEntry>(
    ["workforce", "leave-ledger", hospital, staff.staff_id, year.leave_year],
    `${base}/staff/${staff.staff_id}/leave-ledger?leave_year=${year.leave_year}`,
  );
  const [correcting, setCorrecting] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label="Leave ledger" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {staff.display_name} · leave year {year.leave_year} ledger
        </h4>
        <div className="flex gap-2">
          {correctable && !correcting && staff.balances.length > 0 && (
            <Button variant="outline" onClick={() => setCorrecting(true)}>
              Correct balance
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Close ledger
          </Button>
        </div>
      </div>
      {correcting && (
        <CorrectionForm
          base={base}
          hospital={hospital}
          year={year}
          staff={staff}
          onClose={() => setCorrecting(false)}
        />
      )}
      {ledger.isPending ? (
        <p role="status">Loading ledger…</p>
      ) : ledger.isError ? (
        <Failure message="Could not load the leave ledger." retry={ledger.retry} />
      ) : ledger.items.length === 0 ? (
        <p className="text-sm text-muted-foreground">No entries in this leave year.</p>
      ) : (
        <ul className="space-y-2 text-sm">
          {ledger.items.map((e) => (
            <li key={e.id} className="rounded-xl border p-3">
              <p className="font-medium">
                {e.name} · {LEAVE_LEDGER_KINDS[e.kind]} · {e.half_days > 0 ? "+" : ""}
                {formatHalfDays(e.half_days)}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                Effective {e.effective_on} · by {e.actor_subject}
                {e.reason ? ` · ${e.reason}` : ""}
              </p>
            </li>
          ))}
        </ul>
      )}
      {ledger.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={ledger.isFetchingMore}
          onClick={ledger.loadMore}
        >
          Load more entries
        </Button>
      )}
    </div>
  );
}

function CorrectionForm({
  base,
  hospital,
  year,
  staff,
  onClose,
}: {
  base: string;
  hospital: string;
  year: LeaveYear;
  staff: StaffLeaveBalances;
  onClose: () => void;
}) {
  // One id per form instance, so a retried save replays instead of posting a second correction.
  const [entry] = useState(() => crypto.randomUUID());
  const now = today();
  const form = useZodForm(leaveCorrectionSchema, {
    leave_type_id: staff.balances[0]?.leave_type_id ?? "",
    leave_year: year.leave_year,
    half_days: 0,
    effective_on: now < year.start_on ? year.start_on : now > year.end_on ? year.end_on : now,
    reason: "",
  });
  const save = useApiMutation(
    (input: LeaveCorrectionInput) =>
      apiFetch<LeaveLedgerEntry>(`${base}/staff/${staff.staff_id}/leave-corrections/${entry}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [
        ["workforce", "leave-ledger", hospital],
        ["workforce", "leave-balances", hospital],
      ],
      successToast: "Correction recorded",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={`Correct balance for ${staff.display_name}`}
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="mb-6 space-y-5 rounded-xl border p-4"
    >
      <fieldset disabled={save.isPending} className="space-y-5">
        <div className="grid gap-5 sm:grid-cols-3">
          <Field id="correction-type" label="Leave type" error={errors.leave_type_id?.message}>
            <SelectControl
              id="correction-type"
              value={form.watch("leave_type_id")}
              onValueChange={(v) =>
                form.setValue("leave_type_id", v, { shouldDirty: true, shouldValidate: true })
              }
              options={staff.balances.map((b) => ({
                value: b.leave_type_id,
                label: `${b.name} (${b.code})`,
              }))}
            />
          </Field>
          <Field
            id="correction-amount"
            label="Adjustment in half-days (negative deducts)"
            error={errors.half_days?.message}
          >
            <input
              id="correction-amount"
              type="number"
              step={1}
              className={inputClass}
              aria-invalid={!!errors.half_days}
              {...form.register("half_days", { valueAsNumber: true })}
            />
          </Field>
          <Field
            id="correction-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="correction-effective"
              type="date"
              className={inputClass}
              min={year.start_on}
              max={year.end_on}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
        </div>
        <Field id="correction-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="correction-reason"
            rows={3}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel correction
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record correction"}
        </Button>
      </div>
    </form>
  );
}
