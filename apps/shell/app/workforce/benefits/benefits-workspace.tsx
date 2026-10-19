"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { useApiPagedQuery, usePermissions, type Hospital } from "@helivanta/api";
import { HmsShell, Field, SelectControl } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";
import { Failure } from "../roster-policy/policy-ui";
import { CoverageExport } from "./coverage-export";
import { PlansPanel } from "./plans-panel";
import { StaffBenefitsPanel } from "./staff-benefits-panel";

export function BenefitsWorkspace() {
  const { can, isLoading } = usePermissions();
  const allowed = can("workforce.benefits.view");
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
    { enabled: allowed },
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <HmsShell active="/workforce/benefits" tenantPicker={<TenantPicker />}>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Benefits</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Record the benefit plans this hospital offers, each employee&apos;s dependents and who
            each plan covers. Nothing is pre-loaded or decided automatically: every coverage change
            needs a second person&apos;s approval, and every change keeps its history.
          </p>
        </div>
        {allowed && hospital && (
          <div className="w-full sm:w-72">
            <Field id="benefits-hospital" label="Hospital">
              <SelectControl
                id="benefits-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={hospitals.items.map((h) => ({ value: h.id, label: h.name }))}
              />
            </Field>
          </div>
        )}
      </div>
      {isLoading ? (
        <p role="status">Loading…</p>
      ) : !allowed ? (
        <p className="rounded-2xl border bg-card p-8">
          Benefits are confidential. Ask your tenant administrator for benefits access to see plans,
          dependents and coverage.
        </p>
      ) : hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <HospitalBenefits key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before recording benefits.
        </p>
      )}
      {allowed && hospitals.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={hospitals.isFetchingMore}
          onClick={hospitals.loadMore}
        >
          Load more hospitals
        </Button>
      )}
    </HmsShell>
  );
}

function HospitalBenefits({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const manager = hospital.access_level === "manager";
  const writable = manager && can("workforce.benefits.manage");
  const approver = manager && can("workforce.benefits.approve");
  return (
    <div className="space-y-6">
      <PlansPanel hospital={hospital} writable={writable} />
      <StaffBenefitsPanel hospital={hospital} writable={writable} approver={approver} />
      {can("workforce.benefits.export") && <CoverageExport hospital={hospital} />}
    </div>
  );
}
