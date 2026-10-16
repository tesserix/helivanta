"use client";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button } from "@tesserix/web";
import { useApiPagedQuery, usePermissions, type Hospital } from "@helivanta/api";
import { HmsShell, Field, SelectControl } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";
import { Failure } from "../roster-policy/policy-ui";
import { HeadsPanel } from "./heads-panel";
import { StructuresPanel } from "./structures-panel";

export function PayrollWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <HmsShell active="/workforce/payroll-setup" tenantPicker={<TenantPicker />}>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Payroll setup</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Define the salary heads and structures this hospital pays. Nothing is pre-loaded: every
            head, rate, cap and exemption is entered and reviewed by your payroll team, and each
            change becomes a dated revision.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="payroll-hospital" label="Hospital">
              <SelectControl
                id="payroll-hospital"
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
        <HospitalPayroll key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before setting up payroll.
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
    </HmsShell>
  );
}

function HospitalPayroll({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const writable = hospital.access_level === "manager" && can("workforce.payroll.configure");
  return (
    <div className="space-y-6">
      <HeadsPanel hospital={hospital} writable={writable} />
      <StructuresPanel hospital={hospital} writable={writable} />
    </div>
  );
}

export const headsKey = (hospital: string) => ["workforce", "salary-heads", hospital];
export const structuresKey = (hospital: string) => ["workforce", "salary-structures", hospital];

export function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{value}</dd>
    </div>
  );
}

export function Check({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <input
        type="checkbox"
        className="size-4 shrink-0 accent-(--hms-accent)"
        checked={checked}
        disabled={disabled}
        onChange={(ev) => onChange(ev.target.checked)}
      />
      {label}
    </label>
  );
}

export function ReviewCheck({
  id,
  error,
  register,
  children,
}: {
  id: string;
  error?: string;
  register: object;
  children: ReactNode;
}) {
  return (
    <div className="rounded-xl border bg-muted/30 p-4">
      <label className="flex items-start gap-3 text-sm">
        <input
          type="checkbox"
          className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
          aria-describedby={error ? `${id}-error` : undefined}
          {...register}
        />
        {children}
      </label>
      {error && (
        <p id={`${id}-error`} className="mt-2 text-xs text-destructive">
          {error}
        </p>
      )}
      <p className="mt-3 text-xs text-muted-foreground">
        Changes create a new revision effective on or after the current one. Earlier periods keep
        the revision that was in force on their date.
      </p>
    </div>
  );
}

export function RevisionHistory<T extends { revision_id: string }>({
  label,
  queryKey,
  path,
  render,
  onClose,
}: {
  label: string;
  queryKey: unknown[];
  path: string;
  render: (row: T) => ReactNode;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<T>(queryKey, path);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label={label} className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {label}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading revisions…</p>
      ) : history.isError ? (
        <Failure message="Could not load revisions." retry={history.retry} />
      ) : (
        <ul className="space-y-3">
          {history.items.map((r) => (
            <li key={r.revision_id} className="rounded-xl border p-4 text-sm">
              {render(r)}
            </li>
          ))}
        </ul>
      )}
      {history.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={history.isFetchingMore}
          onClick={history.loadMore}
        >
          Load more revisions
        </Button>
      )}
    </div>
  );
}
