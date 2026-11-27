"use client";

import { useState } from "react";
import { Button, Input } from "@tesserix/web";
import { z } from "zod";
import {
  cancelWardIndent,
  fetchWardIndentOccupant,
  fetchWardIndentOutlets,
  fetchWardIndentProducts,
  fetchWardIndentScopes,
  fetchWardIndents,
  newPharmacyId,
  putWardIndent,
  useApiMutation,
  usePagedQuery,
  wardIndentsKey,
  type WardIndent,
  type WardIndentDepartment,
  type WardIndentOutletChoice,
} from "@helivanta/api";
import { Field, LoadMore, PageHeading, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { useCommandId } from "./command-id";

const bedSchema = z.object({
  room: z
    .string()
    .trim()
    .regex(/^[A-Za-z0-9][A-Za-z0-9-]{0,19}$/, "Use letters, digits and hyphens"),
  bed: z
    .string()
    .trim()
    .regex(/^[A-Za-z0-9][A-Za-z0-9-]{0,19}$/, "Use letters, digits and hyphens"),
});
const searchSchema = z.object({ q: z.string().trim().max(60, "At most 60 characters") });

export function WardIndentRequests() {
  const scopes = usePagedQuery(wardIndentsKey("scopes"), (cursor, signal) =>
    fetchWardIndentScopes(cursor, signal),
  );
  const [scopeId, setScopeId] = useState<string | null>(null);
  const scope = scopes.items.find((s) => s.id === scopeId) ?? null;
  return (
    <div className="flex flex-col gap-5">
      <PageHeading
        title="Ward medicine requests"
        description="Request medicines from a pharmacy for a patient in your ward."
      />
      {scopes.error ? (
        <RequestFailure
          message="Could not load your ward departments."
          error={scopes.error}
          retry={scopes.retry}
        />
      ) : scopes.isPending ? (
        <p role="status">Loading your ward departments…</p>
      ) : scopes.items.length === 0 ? (
        <Panel>
          <p>No department is assigned to you for ward medicine requests.</p>
        </Panel>
      ) : (
        <Panel>
          <h2 className="mb-2 font-medium">Department</h2>
          <div className="flex flex-wrap gap-2">
            {scopes.items.map((s) => (
              <Button
                key={s.id}
                variant={s.id === scopeId ? "default" : "outline"}
                aria-pressed={s.id === scopeId}
                onClick={() => setScopeId(s.id)}
              >
                {s.hospital_name} · {s.department_name}
              </Button>
            ))}
          </div>
          <LoadMore
            hasMore={scopes.hasMore}
            isLoading={scopes.isFetchingMore}
            onClick={scopes.loadMore}
          />
        </Panel>
      )}
      {scope && (
        <Outlets
          key={scope.id}
          department={{ hospitalId: scope.hospital_id, departmentId: scope.department_id }}
          canRaise={scope.assigned_permissions.includes("pharmacy.indent.raise")}
        />
      )}
    </div>
  );
}

function Outlets({
  department,
  canRaise,
}: {
  department: WardIndentDepartment;
  canRaise: boolean;
}) {
  const outlets = usePagedQuery(wardIndentsKey("outlets", department), (cursor, signal) =>
    fetchWardIndentOutlets(department, cursor, signal),
  );
  const [outlet, setOutlet] = useState<WardIndentOutletChoice | null>(null);
  if (outlets.error)
    return (
      <RequestFailure
        message="Could not load pharmacies."
        error={outlets.error}
        retry={outlets.retry}
      />
    );
  if (outlets.isPending) return <p role="status">Loading pharmacies…</p>;
  if (outlets.items.length === 0)
    return (
      <Panel>
        <p>No verified pharmacy serves this department.</p>
      </Panel>
    );
  return (
    <>
      <Panel>
        <h2 className="mb-2 font-medium">Pharmacy</h2>
        <div className="flex flex-wrap gap-2">
          {outlets.items.map((o) => (
            <Button
              key={o.id}
              variant={o.id === outlet?.id ? "default" : "outline"}
              aria-pressed={o.id === outlet?.id}
              onClick={() => setOutlet(o)}
            >
              {o.pharmacy_name} · {o.name}
            </Button>
          ))}
        </div>
        <LoadMore
          hasMore={outlets.hasMore}
          isLoading={outlets.isFetchingMore}
          onClick={outlets.loadMore}
        />
      </Panel>
      {outlet && (
        <OutletWork
          key={outlet.id}
          department={department}
          outlet={outlet.id}
          canRaise={canRaise}
        />
      )}
    </>
  );
}

function OutletWork({
  department,
  outlet,
  canRaise,
}: {
  department: WardIndentDepartment;
  outlet: string;
  canRaise: boolean;
}) {
  const key = wardIndentsKey("list", department, outlet);
  const list = usePagedQuery<WardIndent>(
    key,
    (cursor, signal) => fetchWardIndents(department, outlet, cursor, signal),
    { poll: true },
  );
  const cancel = useApiMutation((indent: string) => cancelWardIndent(department, outlet, indent), {
    successToast: "Indent cancelled",
    invalidate: [key],
  });
  return (
    <>
      {canRaise && <RaiseIndent department={department} outlet={outlet} listKey={key} />}
      <Panel>
        <h2 className="mb-2 font-medium">Raised indents</h2>
        {list.error ? (
          <RequestFailure message="Could not load indents." error={list.error} retry={list.retry} />
        ) : list.isPending ? (
          <p role="status">Loading indents…</p>
        ) : list.items.length === 0 ? (
          <p>No indents raised at this pharmacy yet.</p>
        ) : (
          <ul className="divide-y">
            {list.items.map((indent) => (
              <li
                key={indent.id}
                className="flex flex-wrap items-center justify-between gap-2 py-3"
              >
                <span>
                  Room {indent.room_code}, bed {indent.bed_code} · {indent.line_count}{" "}
                  {indent.line_count === 1 ? "line" : "lines"} · {indent.status}
                  {indent.close_reason ? ` (${indent.close_reason})` : ""}
                </span>
                {indent.status === "open" && canRaise && (
                  <Button
                    variant="outline"
                    disabled={cancel.isPending}
                    onClick={() => cancel.mutate(indent.id)}
                  >
                    Cancel
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        <LoadMore hasMore={list.hasMore} isLoading={list.isFetchingMore} onClick={list.loadMore} />
      </Panel>
    </>
  );
}

type Chosen = { product_id: string; name: string; units: number };

function RaiseIndent({
  department,
  outlet,
  listKey,
}: {
  department: WardIndentDepartment;
  outlet: string;
  listKey: unknown[];
}) {
  const commandId = useCommandId();
  const bedForm = useZodForm(bedSchema, { room: "", bed: "" });
  const searchForm = useZodForm(searchSchema, { q: "" });
  const [lookup, setLookup] = useState<{ room: string; bed: string } | null>(null);
  const [q, setQ] = useState("");
  const [chosen, setChosen] = useState<Chosen[]>([]);
  const occupant = useApiMutation(
    (v: z.infer<typeof bedSchema>) =>
      fetchWardIndentOccupant(department, outlet, v.room.toUpperCase(), v.bed.toUpperCase()),
    {
      onSuccess: () => undefined,
    },
  );
  const products = usePagedQuery(
    wardIndentsKey("products", department, outlet, q),
    (cursor, signal) => fetchWardIndentProducts(department, outlet, q, cursor, signal),
    {
      enabled: occupant.data !== undefined,
    },
  );
  const body = occupant.data
    ? {
        occupancy_id: occupant.data.occupancy_id,
        lines: chosen.map((c) => ({ product_id: c.product_id, requested_units: c.units })),
      }
    : null;
  const raise = useApiMutation(
    (input: NonNullable<typeof body>) => putWardIndent(department, outlet, commandId(input), input),
    {
      successToast: "Indent raised",
      invalidate: [listKey],
      onSuccess: () => setChosen([]),
    },
  );
  const add = (id: string, name: string) =>
    setChosen((rows) =>
      rows.some((r) => r.product_id === id) ? rows : [...rows, { product_id: id, name, units: 1 }],
    );
  return (
    <Panel>
      <h2 className="mb-2 font-medium">Raise an indent</h2>
      <form
        noValidate
        className="flex flex-wrap items-end gap-3"
        onSubmit={bedForm.handleSubmit((v) => {
          setChosen([]);
          setLookup({ room: v.room.toUpperCase(), bed: v.bed.toUpperCase() });
          occupant.mutate(v);
        })}
      >
        <Field id="ward-room" label="Room" error={bedForm.formState.errors.room?.message}>
          <Input id="ward-room" {...bedForm.register("room")} />
        </Field>
        <Field id="ward-bed" label="Bed" error={bedForm.formState.errors.bed?.message}>
          <Input id="ward-bed" {...bedForm.register("bed")} />
        </Field>
        <Button type="submit" variant="outline" disabled={occupant.isPending}>
          Find patient
        </Button>
      </form>
      {occupant.isError && (
        <p role="alert">That bed has no current patient, or you cannot request for it.</p>
      )}
      {occupant.data && lookup && (
        <div className="mt-4 space-y-4">
          <p>
            Indent for the patient in room {lookup.room}, bed {lookup.bed}.
          </p>
          <form
            noValidate
            className="flex items-end gap-3"
            onSubmit={searchForm.handleSubmit((v) => setQ(v.q.toLowerCase()))}
          >
            <Field
              id="ward-product-search"
              label="Find a medicine"
              error={searchForm.formState.errors.q?.message}
            >
              <Input id="ward-product-search" {...searchForm.register("q")} />
            </Field>
            <Button type="submit" variant="outline">
              Search
            </Button>
          </form>
          {products.error ? (
            <RequestFailure
              message="Could not load medicines."
              error={products.error}
              retry={products.retry}
            />
          ) : (
            <ul className="divide-y">
              {products.items.map((p) => (
                <li key={p.id} className="flex items-center justify-between gap-2 py-2">
                  <span>
                    {p.brand_name} <span className="text-muted-foreground">{p.generic_name}</span>
                  </span>
                  <Button variant="outline" onClick={() => add(p.id, p.brand_name)}>
                    Add {p.brand_name}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <LoadMore
            hasMore={products.hasMore}
            isLoading={products.isFetchingMore}
            onClick={products.loadMore}
          />
          {chosen.length > 0 && (
            <ul className="space-y-2">
              {chosen.map((c) => (
                <li key={c.product_id} className="flex items-center gap-3">
                  <span className="flex-1">{c.name}</span>
                  <label className="text-sm" htmlFor={`units-${c.product_id}`}>
                    Units for {c.name}
                  </label>
                  <Input
                    id={`units-${c.product_id}`}
                    type="number"
                    min={1}
                    max={1000000}
                    className="w-28"
                    value={c.units}
                    onChange={(e) => {
                      const units = Math.floor(Number(e.target.value));
                      setChosen((rows) =>
                        rows.map((r) => (r.product_id === c.product_id ? { ...r, units } : r)),
                      );
                    }}
                  />
                  <Button
                    variant="outline"
                    onClick={() =>
                      setChosen((rows) => rows.filter((r) => r.product_id !== c.product_id))
                    }
                  >
                    Remove {c.name}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          {raise.isError && <p role="alert">{raise.error.message}</p>}
          <Button
            disabled={
              raise.isPending ||
              chosen.length === 0 ||
              chosen.some((c) => !Number.isInteger(c.units) || c.units < 1 || c.units > 1000000) ||
              body === null
            }
            onClick={() => body && raise.mutate(body)}
          >
            Raise indent
          </Button>
        </div>
      )}
    </Panel>
  );
}

void newPharmacyId;
