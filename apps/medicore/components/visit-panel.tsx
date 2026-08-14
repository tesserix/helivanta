"use client";

import { z } from "zod";
import { Badge, Button, Input } from "@tesserix/web";
import { CalendarPlus } from "lucide-react";
import { Can, apiFetch, useApiMutation, useApiPagedQuery } from "@hms/api";
import { EmptyState, Field, LoadMore, formatTime, useZodForm } from "@hms/ui";

type Visit = {
  id: string;
  patient_name: string;
  department: string;
  status: string;
  created_at: string;
};

const visitSchema = z.object({
  patient_name: z.string().min(1, "Patient name is required").max(200),
});

export function VisitPanel({ department }: { department: "OPD" | "IPD" }) {
  const visits = useApiPagedQuery<Visit>(["visits"], "/medicore/visits");
  const form = useZodForm(visitSchema, { patient_name: "" });

  const createVisit = useApiMutation(
    (values: z.infer<typeof visitSchema>) =>
      apiFetch<{ id: string }>("/medicore/visits", {
        method: "POST",
        body: JSON.stringify({ ...values, department }),
      }),
    {
      successToast: "Visit created",
      invalidate: [["visits"]],
      onSuccess: () => form.reset(),
    },
  );

  return (
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">Visits</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">
          New visits open a pending dispense in Pharmacy and a pending order in Lab.
        </p>
      </div>
      <Can permission="medicore.visit.create">
        <form
          noValidate
          onSubmit={form.handleSubmit((values) => createVisit.mutate(values))}
          className="flex flex-wrap items-end gap-3 border-b px-5 py-4"
        >
          <div className="min-w-56 flex-1">
            <Field
              id="patient_name"
              label="Patient name"
              error={form.formState.errors.patient_name?.message}
            >
              <Input
                id="patient_name"
                placeholder="e.g. Asha Rao"
                {...form.register("patient_name")}
              />
            </Field>
          </div>
          <Button type="submit" disabled={createVisit.isPending}>
            {createVisit.isPending ? "Creating…" : "Create visit"}
          </Button>
        </form>
      </Can>
      <ul className="divide-y text-sm">
        {visits.items.length === 0 && (
          <li>
            <EmptyState
              icon={CalendarPlus}
              title="No visits yet"
              hint="Create the first one above."
            />
          </li>
        )}
        {visits.items.map((v) => (
          <li key={v.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <div className="flex min-w-0 items-center gap-3">
              <span className="truncate font-medium text-foreground">{v.patient_name}</span>
              <Badge variant="secondary">{v.department}</Badge>
            </div>
            <time className="shrink-0 tabular-nums text-muted-foreground">
              {formatTime(v.created_at)}
            </time>
          </li>
        ))}
      </ul>
      <LoadMore
        hasMore={visits.hasMore}
        isLoading={visits.isFetchingMore}
        onClick={visits.loadMore}
      />
    </section>
  );
}
