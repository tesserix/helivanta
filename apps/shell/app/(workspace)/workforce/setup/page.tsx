"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  hrMasterSchema,
  HR_MASTER_LABELS,
  type HRMaster,
  type HRMasterInput,
  type HRMasterKind,
  type HRMasterRevision,
  type Hospital,
} from "@helivanta/api";
import { Field, PageHeading, SelectControl, useZodForm } from "@helivanta/ui";

const inputClass =
  "w-full rounded-lg border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

export default function HRSetup() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "setup-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <>
      <PageHeading
        description="The departments, designations, grades and other lists staff records choose from. Every change keeps its revision history."
        actions={
          hospital && (
            <div className="w-full sm:w-72">
              <Field id="hr-hospital" label="Hospital">
                <SelectControl
                  id="hr-hospital"
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
        <div role="alert">
          Could not load hospitals.{" "}
          <Button variant="outline" onClick={hospitals.retry}>
            Try again
          </Button>
        </div>
      ) : hospital ? (
        <MasterCatalog key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before using HR setup.
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

function MasterCatalog({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const [kind, setKind] = useState<HRMasterKind>("department");
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<HRMaster | "new" | null>(null);
  const [history, setHistory] = useState<HRMaster | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const path = `/workforce/hospitals/${hospital.id}/hr/masters`;
  const records = useApiPagedQuery<HRMaster>(
    ["workforce", "masters", hospital.id, kind],
    `${path}?kind=${kind}`,
  );
  const writable = can("workforce.master.manage") && hospital.access_level === "manager";
  const rows = records.items.filter((row) =>
    `${row.code} ${row.name}`.toLowerCase().includes(filter.toLowerCase()),
  );
  const close = () => {
    setEditing(null);
    setHistory(null);
    opener.current?.focus();
  };
  return (
    <>
      <section className="overflow-hidden rounded-2xl border bg-card shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
          <div>
            <h3 className="font-semibold">{hospital.name}</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              {writable ? "Manage hospital configuration" : "Read-only configuration"} · Changes
              preserve earlier revisions
            </p>
          </div>
          {writable && (
            <Button
              onClick={(event) => {
                opener.current = event.currentTarget;
                setHistory(null);
                setEditing("new");
              }}
            >
              Add setup record
            </Button>
          )}
        </div>
        <div className="grid gap-4 border-b p-5 sm:grid-cols-2">
          <Field id="hr-kind" label="Category">
            <SelectControl
              id="hr-kind"
              value={kind}
              onValueChange={(value) => {
                setKind(value as HRMasterKind);
                setFilter("");
                close();
              }}
              options={Object.entries(HR_MASTER_LABELS).map(([value, label]) => ({ value, label }))}
            />
          </Field>
          <Field id="hr-search" label="Filter loaded records">
            <input
              id="hr-search"
              className={inputClass}
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder="Search name or code"
            />
          </Field>
        </div>
        {records.isPending ? (
          <p className="p-8" role="status">
            Loading HR setup…
          </p>
        ) : records.isError ? (
          <div className="p-8" role="alert">
            Could not load HR setup.{" "}
            <Button variant="outline" onClick={records.retry}>
              Try again
            </Button>
          </div>
        ) : !rows.length ? (
          <div className="p-12 text-center">
            <h4 className="font-semibold">
              {filter
                ? "No matching loaded records"
                : `No ${HR_MASTER_LABELS[kind].toLowerCase()} yet`}
            </h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {filter
                ? "Try another name or code."
                : "Add the choices your hospital uses. Retire old choices to preserve their history."}
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">
                {HR_MASTER_LABELS[kind]} at {hospital.name}
              </caption>
              <thead className="bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  {["Name", "Code", "Status", "Revision", "Actions"].map((label) => (
                    <th key={label} scope="col" className="px-5 py-3 font-medium">
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((row) => (
                  <tr key={row.id} className="hover:bg-muted/25">
                    <td className="px-5 py-4">
                      <span className="font-medium">{row.name}</span>
                      {row.description && (
                        <p className="mt-1 max-w-md text-xs text-muted-foreground">
                          {row.description}
                        </p>
                      )}
                    </td>
                    <td className="px-5 py-4 font-mono text-xs">{row.code}</td>
                    <td className="px-5 py-4">
                      <span
                        className={
                          row.active
                            ? "rounded-full bg-(--hms-accent-dim) px-2.5 py-1 text-xs text-(--hms-accent)"
                            : "rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground"
                        }
                      >
                        {row.active ? "Active" : "Retired"}
                      </span>
                    </td>
                    <td className="px-5 py-4">{row.version}</td>
                    <td className="px-5 py-4">
                      <div className="flex gap-1">
                        {writable && (
                          <Button
                            size="sm"
                            variant="ghost"
                            aria-label={`Edit ${row.name}`}
                            onClick={(event) => {
                              opener.current = event.currentTarget;
                              setHistory(null);
                              setEditing(row);
                            }}
                          >
                            Edit
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={`History for ${row.name}`}
                          onClick={(event) => {
                            opener.current = event.currentTarget;
                            setEditing(null);
                            setHistory(row);
                          }}
                        >
                          History
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t px-5 py-3">
          <span className="text-xs text-muted-foreground">
            {records.items.length} loaded records · Hospital-specific choices
          </span>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={records.retry}>
              Refresh setup
            </Button>
            {records.hasMore && (
              <Button
                size="sm"
                variant="outline"
                disabled={records.isFetchingMore}
                onClick={records.loadMore}
              >
                Load more records
              </Button>
            )}
          </div>
        </div>
      </section>
      {editing && (
        <MasterEditor
          key={editing === "new" ? `new-${kind}` : editing.id}
          path={path}
          hospital={hospital}
          kind={kind}
          record={editing === "new" ? undefined : editing}
          onClose={close}
        />
      )}
      {history && (
        <MasterHistory
          key={history.id}
          path={`${path}/${history.id}/history`}
          master={history}
          onClose={close}
        />
      )}
    </>
  );
}

function MasterEditor({
  path,
  hospital,
  kind,
  record,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  kind: HRMasterKind;
  record?: HRMaster;
  onClose: () => void;
}) {
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const form = useZodForm(hrMasterSchema, {
    expected_version: record?.version ?? 0,
    kind: record?.kind ?? kind,
    code: record?.code ?? "",
    name: record?.name ?? "",
    description: record?.description ?? "",
    active: record?.active ?? true,
  });
  const section = useRef<HTMLElement>(null);
  const save = useApiMutation(
    (input: HRMasterInput) =>
      apiFetch<HRMaster>(`${path}/${id}`, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [
        ["workforce", "masters", hospital.id],
        ["workforce", "master-choices", hospital.id],
      ],
      successToast: "HR setup saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
    form.setFocus("name");
  }, [form]);
  return (
    <section
      ref={section}
      className="mt-6 scroll-mt-24 rounded-2xl border bg-card p-6 shadow-sm"
      aria-label={record ? "Edit HR setup" : "Add HR setup"}
    >
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h3 className="text-xl font-semibold">
            {record ? "Edit setup record" : "Add setup record"}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {HR_MASTER_LABELS[kind]} · {hospital.name}
          </p>
        </div>
        <Button variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
      </div>
      <form
        noValidate
        autoComplete="off"
        onSubmit={form.handleSubmit((input) => save.mutate(input))}
        className="space-y-5"
      >
        <fieldset disabled={save.isPending} className="space-y-5">
          <div className="grid gap-5 sm:grid-cols-2">
            {(
              [
                ["name", "Name"],
                ["code", "Code"],
              ] as const
            ).map(([name, label]) => (
              <Field
                key={name}
                id={`hr-${name}`}
                label={label}
                error={form.formState.errors[name]?.message}
              >
                <input
                  id={`hr-${name}`}
                  className={inputClass}
                  disabled={name === "code" && !!record}
                  aria-invalid={!!form.formState.errors[name]}
                  aria-describedby={form.formState.errors[name] ? `hr-${name}-error` : undefined}
                  {...form.register(name)}
                />
              </Field>
            ))}
          </div>
          <Field
            id="hr-description"
            label="Description"
            error={form.formState.errors.description?.message}
          >
            <textarea
              id="hr-description"
              rows={3}
              className={inputClass}
              {...form.register("description")}
            />
          </Field>
          <label className="flex items-center gap-3 text-sm">
            <input
              type="checkbox"
              className="size-4 accent-(--hms-accent)"
              {...form.register("active")}
            />
            Available for new assignments
          </label>
          <p className="text-xs text-muted-foreground">
            Codes and categories stay fixed. Retiring a record keeps its history and existing staff
            assignments.
          </p>
        </fieldset>
        {save.isError && (
          <p
            role="alert"
            className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          >
            {save.error.message} Your input is preserved. To load current values, cancel changes and
            refresh setup.
          </p>
        )}
        <div className="flex justify-end border-t pt-5">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save setup record"}
          </Button>
        </div>
      </form>
    </section>
  );
}

function MasterHistory({
  path,
  master,
  onClose,
}: {
  path: string;
  master: HRMaster;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<HRMasterRevision>(
    ["workforce", "master-history", master.id],
    path,
  );
  const section = useRef<HTMLElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
    heading.current?.focus();
  }, []);
  return (
    <section
      ref={section}
      className="mt-6 scroll-mt-24 rounded-2xl border bg-card p-6 shadow-sm"
      aria-label="HR revision history"
    >
      <div className="mb-5 flex items-start justify-between gap-4">
        <div>
          <h3 ref={heading} tabIndex={-1} className="text-xl font-semibold">
            Revision history
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {master.name} · {master.code}
          </p>
        </div>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading history…</p>
      ) : history.isError ? (
        <div role="alert">
          Could not load history.{" "}
          <Button variant="outline" onClick={history.retry}>
            Try again
          </Button>
        </div>
      ) : (
        <ol className="space-y-3">
          {history.items.map((revision) => (
            <li key={revision.id} className="rounded-xl border p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="font-medium">{revision.name}</p>
                <span className="text-xs text-muted-foreground">
                  Revision {revision.version} · {revision.active ? "Active" : "Retired"}
                </span>
              </div>
              {revision.description && (
                <p className="mt-1 text-sm text-muted-foreground">{revision.description}</p>
              )}
              <p className="mt-2 text-xs text-muted-foreground">
                {new Date(revision.created_at).toLocaleString()}
              </p>
            </li>
          ))}
        </ol>
      )}
      {history.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={history.isFetchingMore}
          onClick={history.loadMore}
        >
          Load earlier revisions
        </Button>
      )}
    </section>
  );
}
