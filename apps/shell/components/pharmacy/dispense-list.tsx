"use client";

import { z } from "zod";
import { Badge, Button, Input } from "@tesserix/web";
import { Pill } from "lucide-react";
import { Can, apiFetch, useApiMutation, useApiPagedQuery } from "@helivanta/api";
import { Field, formatTime, LoadMore, QueryState, useZodForm } from "@helivanta/ui";

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

export function DispenseList() {
  const dispenses = useApiPagedQuery<Dispense>(["dispenses"], "/pharmacy/dispenses", {
    poll: true,
  });

  const pending = dispenses.items.filter((d) => d.status === "pending").length;

  return (
    <section className="max-w-3xl rounded-lg border bg-card">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b px-5 py-4">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Dispense queue</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            {pending === 0
              ? "Nothing waiting right now."
              : `${pending} pending dispense${pending === 1 ? "" : "s"}.`}
          </p>
        </div>
      </div>
      <QueryState
        query={dispenses}
        label="dispense tasks"
        empty={{
          icon: Pill,
          title: "No dispense tasks yet",
          hint: "They appear here when a visit is created in MediCore.",
        }}
      >
        {(items) => (
          <ul className="divide-y text-sm">
            {items.map((d) => (
              <li key={d.id} className="flex items-center justify-between gap-4 px-5 py-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-3">
                    <span className="truncate font-medium text-foreground">{d.patient_name}</span>
                    <Badge variant={d.status === "pending" ? "secondary" : "outline"}>
                      {d.status === "pending" ? "Pending" : "Dispensed"}
                    </Badge>
                  </div>
                  <div className="mt-0.5 text-muted-foreground">
                    {d.status === "dispensed" ? d.medication : "Awaiting dispense"}
                  </div>
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
    </section>
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
