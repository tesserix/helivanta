"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { useApiPagedQuery, usePermissions, type Hospital, type LeaveYear } from "@helivanta/api";
import { Field, PageHeading, SelectControl } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { LeaveSettingsPanel, LeaveYearsPanel } from "./years-panel";
import { LeaveAssignmentsPanel } from "./assignments-panel";
import { LeaveBalancesPanel } from "./balances-panel";

export function LeaveBalancesWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <>
      <PageHeading
        description="Assign leave structures, post monthly credits, close leave years and correct balances. Every change is an append-only ledger entry."
        actions={
          hospital && (
            <div className="w-full sm:w-72">
              <Field id="balances-hospital" label="Hospital">
                <SelectControl
                  id="balances-hospital"
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
        <LeaveLedger key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing leave balances.
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

function LeaveLedger({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const manager = hospital.access_level === "manager";
  const years = useApiPagedQuery<LeaveYear>(
    ["workforce", "leave-years", hospital.id],
    `/workforce/hospitals/${hospital.id}/hr/leave-years`,
  );
  return (
    <div className="space-y-6">
      <LeaveSettingsPanel
        hospital={hospital}
        writable={manager && can("workforce.leave.configure")}
      />
      <LeaveYearsPanel
        hospital={hospital}
        years={years}
        writable={manager && can("workforce.leave.allocate")}
      />
      <LeaveAssignmentsPanel
        hospital={hospital}
        writable={manager && can("workforce.leave.allocate")}
      />
      <LeaveBalancesPanel
        hospital={hospital}
        years={years.items}
        correctable={manager && can("workforce.leave.correct")}
      />
    </div>
  );
}
