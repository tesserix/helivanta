"use client";

import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { ClipboardList } from "lucide-react";
import { apiFetch, useApiMutation, useApiPagedQuery } from "@hms/api";
import { EmptyState, Field, LoadMore, useZodForm } from "@hms/ui";

type Medication = { id: string; name: string; strength: string; created_at: string };

const medicationSchema = z.object({
  name: z.string().min(1, "Name is required").max(200),
  strength: z.string().max(100),
});

export function MedicationsPanel() {
  const medications = useApiPagedQuery<Medication>(["medications"], "/pharmacy/medications");
  const form = useZodForm(medicationSchema, { name: "", strength: "" });

  const addMedication = useApiMutation(
    (values: z.infer<typeof medicationSchema>) =>
      apiFetch("/pharmacy/medications", {
        method: "POST",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Medication added",
      invalidate: [["medications"]],
      onSuccess: () => form.reset(),
    },
  );

  return (
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">Formulary</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">
          Medications available for dispensing in this store.
        </p>
      </div>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => addMedication.mutate(values))}
        className="flex flex-wrap items-end gap-3 border-b px-5 py-4"
      >
        <div className="min-w-48 flex-1">
          <Field id="name" label="Name" error={form.formState.errors.name?.message}>
            <Input id="name" placeholder="e.g. Paracetamol" {...form.register("name")} />
          </Field>
        </div>
        <div className="w-36">
          <Field id="strength" label="Strength" error={form.formState.errors.strength?.message}>
            <Input id="strength" placeholder="500mg" {...form.register("strength")} />
          </Field>
        </div>
        <Button type="submit" disabled={addMedication.isPending}>
          {addMedication.isPending ? "Adding…" : "Add medication"}
        </Button>
      </form>
      <ul className="divide-y text-sm">
        {medications.items.length === 0 && (
          <li>
            <EmptyState
              icon={ClipboardList}
              title="No medications yet"
              hint="Add the first one above."
            />
          </li>
        )}
        {medications.items.map((m) => (
          <li key={m.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <span className="truncate font-medium text-foreground">{m.name}</span>
            <span className="shrink-0 text-muted-foreground">{m.strength}</span>
          </li>
        ))}
      </ul>
      <LoadMore
        hasMore={medications.hasMore}
        isLoading={medications.isFetchingMore}
        onClick={medications.loadMore}
      />
    </section>
  );
}
