"use client";
import { useEffect, useRef, useState } from "react";
import { useNow } from "@/lib/use-now";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  dutyExchangeFormSchema,
  toDutyExchangeInput,
  useApiMutation,
  useApiPagedQuery,
  DUTY_EXCHANGE_KINDS,
  type DatedAllocation,
  type DutyExchangeDetail,
  type DutyExchangeForm as DutyExchangeValues,
  type DutyExchangeInput,
  type Roster,
  type Staff,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

const kindOptions = Object.entries(DUTY_EXCHANGE_KINDS).map(([value, label]) => ({
  value,
  label,
}));
const dutyLabel = (a: DatedAllocation) =>
  `${a.roster_date} · ${a.shift_code} ${a.start_time}–${a.end_time} · ${a.area_code}`;

// Only future primary duties can change hands; additional shifts follow their primary.
function useDuties(hospital: string, base: string, roster: string, staff: string) {
  const duties = useApiPagedQuery<DatedAllocation>(
    ["workforce", "roster-allocations", hospital, roster, staff],
    `${base}/hr/rosters/${roster}/allocations?staff_id=${staff}`,
    { enabled: !!roster && !!staff },
  );
  const now = useNow();
  return {
    ...duties,
    options: duties.items
      .filter((a) => !a.additional && Date.parse(a.starts_at) > now)
      .map((a) => ({ value: a.id, label: dutyLabel(a) })),
  };
}

export function DutyExchangeForm({
  hospital,
  base,
  onClose,
}: {
  hospital: string;
  base: string;
  onClose: () => void;
}) {
  // One id per form instance, so a retried filing replays instead of creating a second exchange.
  const [id] = useState(() => crypto.randomUUID());
  const form = useZodForm(dutyExchangeFormSchema, {
    kind: "swap",
    roster_id: "",
    requester_staff_id: "",
    requester_allocation_id: "",
    counterpart_staff_id: "",
    counterpart_allocation_id: "",
    counterpart_consent: false,
    reason: "",
  });
  const rosters = useApiPagedQuery<Roster>(
    ["workforce", "rosters", hospital],
    `${base}/hr/rosters`,
  );
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital], `${base}/staff`);
  const [kind, roster, requester, counterpart] = form.watch([
    "kind",
    "roster_id",
    "requester_staff_id",
    "counterpart_staff_id",
  ]);
  const given = useDuties(hospital, base, roster, requester);
  const returned = useDuties(hospital, base, roster, kind === "swap" ? counterpart : "");
  const save = useApiMutation(
    (input: DutyExchangeInput) =>
      apiFetch<DutyExchangeDetail>(`${base}/hr/duty-exchanges/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [["workforce", "duty-exchanges", hospital]],
      successToast: "Duty exchange filed",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => section.current?.scrollIntoView?.({ block: "start", behavior: "instant" }), []);
  const choose = (
    name: keyof DutyExchangeValues,
    value: string,
    clears: (keyof DutyExchangeValues)[],
  ) => {
    form.setValue(name, value, { shouldDirty: true, shouldValidate: true });
    for (const c of clears) form.setValue(c, "", { shouldDirty: true });
  };
  const people = staff.items
    .filter((s) => s.employment_status === "active")
    .map((s) => ({ value: s.id, label: `${s.display_name} · ${s.employee_code}` }));
  const errors = form.formState.errors;
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label="File duty exchange"
      onSubmit={form.handleSubmit((values: DutyExchangeValues) =>
        save.mutate(toDutyExchangeInput(values)),
      )}
      className="scroll-mt-24 space-y-6 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="exchange-kind" label="Exchange" error={errors.kind?.message}>
            <SelectControl
              id="exchange-kind"
              value={kind}
              onValueChange={(v) =>
                choose("kind", v as DutyExchangeValues["kind"], ["counterpart_allocation_id"])
              }
              options={kindOptions}
            />
          </Field>
          <Field id="exchange-roster" label="Published roster" error={errors.roster_id?.message}>
            <SelectControl
              id="exchange-roster"
              value={roster}
              placeholder={rosters.isPending ? "Loading rosters…" : "Choose a published roster"}
              onValueChange={(v) =>
                choose("roster_id", v, ["requester_allocation_id", "counterpart_allocation_id"])
              }
              options={rosters.items
                .filter((r) => r.status === "frozen")
                .map((r) => ({
                  value: r.id,
                  label: `${r.name} · ${r.start_date} to ${r.end_date}`,
                }))}
            />
          </Field>
        </div>
        <div className="grid gap-5 sm:grid-cols-2">
          <Field
            id="exchange-requester"
            label="Gives up the duty"
            error={errors.requester_staff_id?.message}
          >
            <SelectControl
              id="exchange-requester"
              value={requester}
              placeholder={staff.isPending ? "Loading staff…" : "Choose a staff member"}
              onValueChange={(v) => choose("requester_staff_id", v, ["requester_allocation_id"])}
              options={people}
            />
          </Field>
          <Field
            id="exchange-given"
            label="Duty handed over"
            error={errors.requester_allocation_id?.message}
          >
            <SelectControl
              id="exchange-given"
              value={form.watch("requester_allocation_id")}
              placeholder={
                !roster || !requester
                  ? "Choose a roster and person first"
                  : given.isPending
                    ? "Loading duties…"
                    : given.options.length === 0
                      ? "No future duties in this roster"
                      : "Choose a duty"
              }
              onValueChange={(v) => choose("requester_allocation_id", v, [])}
              options={given.options}
            />
          </Field>
          <Field
            id="exchange-counterpart"
            label={kind === "swap" ? "Swaps with" : "Covers the duty"}
            error={errors.counterpart_staff_id?.message}
          >
            <SelectControl
              id="exchange-counterpart"
              value={counterpart}
              placeholder={staff.isPending ? "Loading staff…" : "Choose a staff member"}
              onValueChange={(v) =>
                choose("counterpart_staff_id", v, ["counterpart_allocation_id"])
              }
              options={people}
            />
          </Field>
          {kind === "swap" && (
            <Field
              id="exchange-returned"
              label="Duty given in return"
              error={errors.counterpart_allocation_id?.message}
            >
              <SelectControl
                id="exchange-returned"
                value={form.watch("counterpart_allocation_id")}
                placeholder={
                  !roster || !counterpart
                    ? "Choose a roster and person first"
                    : returned.isPending
                      ? "Loading duties…"
                      : returned.options.length === 0
                        ? "No future duties in this roster"
                        : "Choose a duty"
                }
                onValueChange={(v) => choose("counterpart_allocation_id", v, [])}
                options={returned.options}
              />
            </Field>
          )}
        </div>
        {(given.isError || returned.isError) && (
          <SaveError message="Could not load roster duties. Choose the roster again to retry." />
        )}
        <Field id="exchange-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="exchange-reason"
            rows={3}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
        <div className="rounded-xl border bg-muted/30 p-4">
          <label className="flex items-start gap-3 text-sm">
            <input
              type="checkbox"
              className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
              aria-describedby={errors.counterpart_consent ? "exchange-consent-error" : undefined}
              {...form.register("counterpart_consent")}
            />
            The other person has agreed to this exchange
          </label>
          {errors.counterpart_consent && (
            <p id="exchange-consent-error" className="mt-2 text-xs text-destructive">
              {errors.counterpart_consent.message}
            </p>
          )}
          <p className="mt-3 text-xs text-muted-foreground">
            The reason is shown only to people who file or approve duty exchanges. Leave out
            diagnoses and other clinical detail.
          </p>
        </div>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel filing
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Filing…" : "File duty exchange"}
        </Button>
      </div>
    </form>
  );
}
