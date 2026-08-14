"use client";

import { useState } from "react";
import { Badge, Button, Input } from "@tesserix/web";
import { Pill } from "lucide-react";
import { Can, apiFetch, useApiMutation, useApiPagedQuery } from "@hms/api";
import { EmptyState, LoadMore, formatTime } from "@hms/ui";

type Dispense = {
  id: string;
  visit_id: string;
  patient_name: string;
  medication: string;
  status: "pending" | "dispensed";
  dispensed_at: string | null;
  created_at: string;
};

export function DispenseList() {
  const [medication, setMedication] = useState("Paracetamol 500mg");
  const dispenses = useApiPagedQuery<Dispense>(["dispenses"], "/pharmacy/dispenses", {
    poll: true,
  });

  const dispenseRow = useApiMutation(
    (id: string) =>
      apiFetch("/pharmacy/dispenses/" + id + "/dispense", {
        method: "POST",
        body: JSON.stringify({ medication }),
      }),
    {
      successToast: "Dispensed",
      invalidate: [["dispenses"]],
    },
  );

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
        <label
          htmlFor="dispense-medication-filter"
          className="flex flex-col gap-1.5 text-sm font-medium"
        >
          Medication
          <Input
            id="dispense-medication-filter"
            value={medication}
            onChange={(e) => setMedication(e.target.value)}
            className="w-56"
          />
        </label>
      </div>
      <ul className="divide-y text-sm">
        {dispenses.items.length === 0 && (
          <li>
            <EmptyState
              icon={Pill}
              title="No dispense tasks yet"
              hint="They appear here when a visit is created in MediCore."
            />
          </li>
        )}
        {dispenses.items.map((d) => (
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
                <Button
                  onClick={() => dispenseRow.mutate(d.id)}
                  disabled={dispenseRow.isPending && dispenseRow.variables === d.id}
                >
                  {dispenseRow.isPending && dispenseRow.variables === d.id
                    ? "Dispensing…"
                    : "Dispense"}
                </Button>
              </Can>
            ) : (
              <time className="shrink-0 tabular-nums text-muted-foreground">
                {formatTime(d.dispensed_at ?? "")}
              </time>
            )}
          </li>
        ))}
      </ul>
      <LoadMore
        hasMore={dispenses.hasMore}
        isLoading={dispenses.isFetchingMore}
        onClick={dispenses.loadMore}
      />
    </section>
  );
}
