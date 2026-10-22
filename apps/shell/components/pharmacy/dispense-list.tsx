"use client";

import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { CheckCheck, Pill, type LucideIcon } from "lucide-react";
import { Can, apiFetch, useApiMutation, useApiPagedQuery } from "@helivanta/api";
import { Field, formatTime, LoadMore, Panel, QueryState, useZodForm } from "@helivanta/ui";

type Dispense = {
  id: string;
  visit_id: string;
  patient_name: string;
  medication: string;
  status: "pending" | "dispensed";
  dispensed_at: string | null;
  created_at: string;
};

// No default: each dispense records what the pharmacist typed for that row (#1272).
const dispenseSchema = z.object({
  medication: z.string().trim().min(1, "Medication is required").max(200),
});

// The queue splits on the server (#1258 D4): a pending row on a later page
// must never hide behind dispensed ones, so each status is its own list.
export function DispenseList() {
  return (
    <div className="max-w-3xl space-y-6">
      <StatusList
        status="pending"
        title="Waiting"
        label="waiting dispenses"
        empty={{
          icon: Pill,
          title: "Nothing waiting",
          hint: "Prescriptions from MediCore visits appear here.",
        }}
      />
      <StatusList
        status="dispensed"
        title="Dispensed"
        label="dispensed medicines"
        empty={{ icon: CheckCheck, title: "Nothing dispensed yet" }}
      />
    </div>
  );
}

function StatusList({
  status,
  title,
  label,
  empty,
}: {
  status: Dispense["status"];
  title: string;
  label: string;
  empty: { icon: LucideIcon; title: string; hint?: string };
}) {
  const dispenses = useApiPagedQuery<Dispense>(
    ["dispenses", status],
    `/pharmacy/dispenses?status=${status}`,
    { poll: true },
  );
  const headingId = `dispenses-${status}`;
  return (
    <Panel flush aria-labelledby={headingId}>
      <h2 id={headingId} className="border-b px-5 py-4 text-sm font-semibold text-foreground">
        {title}
      </h2>
      <QueryState query={dispenses} label={label} empty={empty}>
        {(items) => (
          <ul className="divide-y text-sm">
            {items.map((d) => (
              <li key={d.id} className="flex items-center justify-between gap-4 px-5 py-3">
                <div className="min-w-0">
                  <span className="block truncate font-medium text-foreground">
                    {d.patient_name}
                  </span>
                  <span className="mt-0.5 block text-muted-foreground">
                    {d.status === "dispensed"
                      ? d.medication
                      : `Waiting since ${formatTime(d.created_at)}`}
                  </span>
                </div>
                {d.status === "pending" ? (
                  <Can permission="pharmacy.dispense.fulfil">
                    <DispenseForm id={d.id} />
                  </Can>
                ) : (
                  <time className="shrink-0 tabular-nums text-muted-foreground">
                    {formatTime(d.dispensed_at ?? "")}
                  </time>
                )}
              </li>
            ))}
          </ul>
        )}
      </QueryState>
      <LoadMore
        hasMore={dispenses.hasMore}
        isLoading={dispenses.isFetchingMore}
        onClick={dispenses.loadMore}
      />
    </Panel>
  );
}

function DispenseForm({ id }: { id: string }) {
  const form = useZodForm(dispenseSchema, { medication: "" });
  const dispense = useApiMutation(
    (values: z.infer<typeof dispenseSchema>) =>
      apiFetch("/pharmacy/dispenses/" + id + "/dispense", {
        method: "POST",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Dispensed",
      invalidate: [["dispenses"]],
    },
  );
  const fieldId = `dispense-medication-${id}`;

  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => dispense.mutate(values))}
      className="flex shrink-0 items-start gap-2"
    >
      <div className="w-56">
        <Field id={fieldId} label="Medication" error={form.formState.errors.medication?.message}>
          <Input id={fieldId} {...form.register("medication")} />
        </Field>
      </div>
      <Button type="submit" className="mt-6" disabled={dispense.isPending}>
        {dispense.isPending ? "Dispensing…" : "Dispense"}
      </Button>
    </form>
  );
}
