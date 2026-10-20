"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { useApiPagedQuery, usePermissions, type Hospital } from "@helivanta/api";
import { Field, SelectControl } from "@helivanta/ui";
import { SettingsPanel } from "./settings-panel";
import { AssignmentsPanel } from "./assignments-panel";
import { Failure } from "./policy-ui";

export function PolicyWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  const { can } = usePermissions();
  const writable =
    !!hospital && can("workforce.roster.manage") && hospital.access_level === "manager";
  return (
    <>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Roster policy</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Set the hospital timezone, roster week and automatic population, then assign weekly
            templates to staff.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="policy-hospital" label="Hospital">
              <SelectControl
                id="policy-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={hospitals.items.map((h) => ({ value: h.id, label: h.name }))}
              />
            </Field>
          </div>
        )}
      </div>
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <div key={hospital.id} className="space-y-8">
          <SettingsPanel hospital={hospital} writable={writable} />
          <AssignmentsPanel hospital={hospital} writable={writable} />
        </div>
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing roster policy.
        </p>
      )}
      {hospitals.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={hospitals.isFetchingMore}
          onClick={hospitals.loadMore}
        >
          Load more hospitals
        </Button>
      )}
    </>
  );
}
