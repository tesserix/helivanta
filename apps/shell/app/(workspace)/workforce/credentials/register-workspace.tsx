"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  credentialRegisterFilterSchema,
  credentialRegisterKey,
  credentialRegisterPath,
  useApiPagedQuery,
  usePermissions,
  type CredentialRegisterEntry,
  type Hospital,
} from "@helivanta/api";
import { Field, LoadMore, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { standingText } from "../credential-panel";

export function CredentialRegisterWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const managed = hospitals.items.filter((h) => h.access_level === "manager");
  const [selected, setSelected] = useState("");
  const hospital = managed.find((h) => h.id === selected) ?? managed[0];
  return (
    <>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Credential register</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Every reviewed licence and registration, soonest expiry first, in the hospital calendar.
            Only approved reviews appear here.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="register-hospital" label="Hospital">
              <SelectControl
                id="register-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={managed.map((h) => ({ value: h.id, label: h.name }))}
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
        <Register key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital management access to view the credential
          register.
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

function Register({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const form = useZodForm(credentialRegisterFilterSchema, { within: "" });
  const [within, setWithin] = useState<number | null>(null);
  const allowed = can("workforce.enrollment.manage");
  const rows = useApiPagedQuery<CredentialRegisterEntry>(
    credentialRegisterKey(hospital.id),
    credentialRegisterPath(hospital.id, within),
    { enabled: allowed, request: { cache: "no-store" } },
  );
  if (!allowed)
    return (
      <p role="alert" className="rounded-2xl border bg-card p-8">
        You need enrollment management access to view the credential register.
      </p>
    );
  const zoneUnset =
    rows.error instanceof ApiError && rows.error.detail.reason === "time_zone_unset";
  return (
    <section aria-label="Credential register" className="space-y-6">
      <form
        noValidate
        autoComplete="off"
        aria-label="Register filter"
        className="flex flex-wrap items-end gap-3"
        onSubmit={form.handleSubmit((values) =>
          setWithin(values.within === "" ? null : Number(values.within)),
        )}
      >
        <div className="w-full sm:w-64">
          <Field
            id="register-within"
            label="Expiring within (days)"
            error={form.formState.errors.within?.message}
          >
            <input
              id="register-within"
              inputMode="numeric"
              placeholder="All reviewed credentials"
              className={inputClass}
              {...form.register("within")}
            />
          </Field>
        </div>
        <Button type="submit">Show credentials</Button>
      </form>
      {rows.isPending ? (
        <p role="status">Loading credentials…</p>
      ) : zoneUnset ? (
        <p role="alert" className="rounded-xl border p-6 text-sm">
          {rows.error?.message}
        </p>
      ) : rows.isError ? (
        <Failure message="Could not load the credential register." retry={rows.retry} />
      ) : rows.items.length === 0 ? (
        <p className="rounded-xl border p-6 text-sm">
          {within === null
            ? "No credentials have been reviewed at this hospital yet."
            : "No reviewed credentials expire in this window."}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-2xl border bg-card">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Reviewed credentials at {hospital.name}</caption>
            <thead className="bg-muted/40 text-xs text-muted-foreground">
              <tr>
                {["Staff", "Credential", "Number", "Valid from", "Valid until", "Status"].map(
                  (label) => (
                    <th key={label} scope="col" className="px-4 py-3 font-medium">
                      {label}
                    </th>
                  ),
                )}
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.items.map((row) => (
                <tr key={`${row.staff_id}:${row.credential_id}`}>
                  <td className="px-4 py-3">
                    {row.display_name}
                    <span className="block font-mono text-xs text-muted-foreground">
                      {row.employee_code}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    {row.type}
                    <span className="block text-xs text-muted-foreground">{row.issuer}</span>
                  </td>
                  <td className="px-4 py-3">{row.number}</td>
                  <td className="px-4 py-3">{row.valid_from}</td>
                  <td className="px-4 py-3">{row.valid_until}</td>
                  <td className="px-4 py-3">{standingText(row.status, row.days_remaining)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <LoadMore
            hasMore={rows.hasMore}
            isLoading={rows.isFetchingMore}
            onClick={rows.loadMore}
          />
        </div>
      )}
    </section>
  );
}
