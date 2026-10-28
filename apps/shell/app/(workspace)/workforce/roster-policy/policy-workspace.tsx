"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { useApiPagedQuery, usePermissions, type Hospital } from "@helivanta/api";
import { Field, PageHeading, SelectControl } from "@helivanta/ui";
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
      <PageHeading
        description="Set the hospital timezone, roster week and automatic population, then assign weekly templates to staff."
        actions={
          hospital && (
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
          )
        }
      />
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
