"use client";

import { useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  newSupplyId,
  supplyBinFieldsSchema,
  supplyBinInputSchema,
  supplyBinsKey,
  supplyBinsRoot,
  supplyHeaders,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  type SupplyBin,
  type SupplyBinInput,
  type SupplyPremise,
  type SupplyRegistryScope,
  type SupplyRegistryRevision,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  formatDateTime,
  useZodForm,
  RequestFailure,
} from "@helivanta/ui";

const label = (value: string) => value.replaceAll("_", " ");
export function WarehouseBins({
  scope,
  premise,
  roles,
  onClose,
  operational,
}: {
  scope: SupplyRegistryScope;
  premise: Pick<SupplyPremise, "id" | "code" | "name" | "status">;
  operational?: { read: boolean; write: boolean };
  roles: string[];
  onClose: () => void;
}) {
  const { can } = usePermissions();
  const allowed = operational
    ? can("supply.operations.read") && operational.read
    : can("supply.bin.read") &&
      roles.some((role) => ["administrator", "purchasing", "compliance", "auditor"].includes(role));
  const writable = operational
    ? can("supply.operations.write") && operational.write
    : can("supply.bin.write") &&
      roles.some((role) => ["administrator", "purchasing"].includes(role));
  const route = operational
    ? `/supply/parties/${scope.partyId}/operating-locations/${premise.id}/bins`
    : supplyBinsRoot(scope, premise.id);
  const queryKey = supplyBinsKey(scope, premise.id, ...(operational ? ["operating"] : []));
  const bins = useApiPagedQuery<SupplyBin>(queryKey, route, {
    enabled: allowed,
    request: { headers: supplyHeaders(scope) },
  });
  const [editing, setEditing] = useState<SupplyBin | null>();
  const [history, setHistory] = useState<string | null>(null);
  const target = useRef<HTMLDivElement>(null);
  function edit(row: SupplyBin | null) {
    setEditing(row);
    requestAnimationFrame(() => {
      target.current?.scrollIntoView({ block: "start", behavior: "instant" });
      target.current?.querySelector<HTMLInputElement>("input")?.focus({ preventScroll: true });
    });
  }
  if (!allowed) return null;
  return (
    <section aria-label="Warehouse bins" className="space-y-5 rounded-xl border bg-card p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h5 tabIndex={-1} className="font-semibold">
            Bins · {premise.name}
          </h5>
          <p className="mt-1 text-sm text-muted-foreground">
            {premise.code} · {label(premise.status)} premise. Bin storage classes are declarations,
            not compliance approvals.
          </p>
        </div>
        <Button variant="ghost" onClick={onClose}>
          Close bins
        </Button>
      </div>
      {bins.isError ? (
        <RequestFailure
          message="Could not load warehouse bins. Refresh access or contact your administrator."
          retry={bins.retry}
        />
      ) : bins.isPending ? (
        <p role="status">Loading bins…</p>
      ) : (
        <>
          {writable && premise.status === "active" && (
            <Button variant="outline" onClick={() => edit(null)}>
              Add bin
            </Button>
          )}
          {premise.status !== "active" && (
            <p role="status" className="text-sm text-muted-foreground">
              This premise is inactive. Retained bins can be viewed or retired; activate the premise
              before activating bins.
            </p>
          )}
          {bins.items.length === 0 ? (
            <p className="text-sm text-muted-foreground">No bins recorded for this premise.</p>
          ) : (
            <ul className="divide-y">
              {bins.items.map((row) => (
                <li key={row.id} className="flex flex-wrap items-start justify-between gap-3 py-4">
                  <div>
                    <p className="break-words font-medium">
                      {row.code} · {row.name}
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {[
                        row.aisle && `Aisle ${row.aisle}`,
                        row.rack && `Rack ${row.rack}`,
                        row.shelf && `Shelf ${row.shelf}`,
                      ]
                        .filter(Boolean)
                        .join(" · ") || "Position not specified"}
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {label(row.storage_class)} · {label(row.status)} · Version {row.version}
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {writable && (
                      <Button
                        variant="outline"
                        aria-label={`Edit bin ${row.code}`}
                        onClick={() => edit(row)}
                      >
                        Edit
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      aria-label={`History for bin ${row.code}`}
                      onClick={() => setHistory(row.id)}
                    >
                      History
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <LoadMore
            hasMore={bins.hasMore}
            isLoading={bins.isFetchingMore}
            onClick={bins.loadMore}
          />
          <div ref={target} className="scroll-mt-32">
            {writable && editing !== undefined && (
              <BinEditor
                key={editing ? `${editing.id}:${editing.version}` : "new"}
                scope={scope}
                route={route}
                queryKey={queryKey}
                row={editing}
                onClose={() => setEditing(undefined)}
              />
            )}
          </div>
          {history && (
            <BinHistory
              key={history}
              scope={scope}
              route={route}
              queryKey={queryKey}
              id={history}
              onClose={() => setHistory(null)}
            />
          )}
        </>
      )}
    </section>
  );
}
function BinEditor({
  scope,
  route,
  queryKey,
  row,
  onClose,
}: {
  scope: SupplyRegistryScope;
  route: string;
  queryKey: unknown[];
  row: SupplyBin | null;
  onClose: () => void;
}) {
  const [id] = useState(() => row?.id ?? newSupplyId());
  const form = useZodForm(
    supplyBinInputSchema,
    row
      ? { ...supplyBinFieldsSchema.strip().parse(row), expected_version: row.version }
      : {
          expected_version: 0,
          code: "",
          name: "",
          aisle: "",
          rack: "",
          shelf: "",
          storage_class: "ambient",
          status: "active",
        },
  );
  const save = useApiMutation(
    (value: SupplyBinInput) =>
      apiFetch<SupplyBin>(`${route}/${id}`, {
        method: "PUT",
        headers: supplyHeaders(scope),
        body: JSON.stringify(value),
      }),
    {
      invalidate: [queryKey],
      successToast: "Bin saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  return (
    <form
      noValidate
      aria-label="Storage bin form"
      onSubmit={form.handleSubmit((value) => save.mutate(value))}
      className="space-y-5 rounded-xl border bg-background p-5"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h6 className="font-semibold">{row ? "Edit bin" : "New bin"}</h6>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Close bin form
        </Button>
      </div>
      <fieldset disabled={save.isPending} className="grid gap-4 sm:grid-cols-2">
        {(
          [
            ["code", "Bin code"],
            ["name", "Bin name"],
            ["aisle", "Aisle"],
            ["rack", "Rack"],
            ["shelf", "Shelf"],
          ] as const
        ).map(([name, text]) => (
          <Field
            key={name}
            id={`bin-${name}`}
            label={text}
            error={form.formState.errors[name]?.message}
          >
            <Input
              id={`bin-${name}`}
              {...form.register(name)}
              aria-invalid={!!form.formState.errors[name]}
              aria-describedby={form.formState.errors[name] ? `bin-${name}-error` : undefined}
            />
          </Field>
        ))}
        <FormSelectField
          control={form.control}
          name="storage_class"
          id="bin-storage-class"
          label="Storage class"
          options={supplyBinFieldsSchema.shape.storage_class.options.map((value) => ({
            value,
            label: label(value),
          }))}
        />
        <FormSelectField
          control={form.control}
          name="status"
          id="bin-status"
          label="Bin status"
          options={supplyBinFieldsSchema.shape.status.options.map((value) => ({
            value,
            label: label(value),
          }))}
        />
      </fieldset>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message} Your draft is retained; refresh the list if another user changed this
          bin.
        </p>
      )}
      <Button type="submit" disabled={save.isPending}>
        {save.isPending ? "Saving…" : "Save bin"}
      </Button>
    </form>
  );
}
function BinHistory({
  scope,
  route,
  queryKey,
  id,
  onClose,
}: {
  scope: SupplyRegistryScope;
  route: string;
  queryKey: unknown[];
  id: string;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<SupplyRegistryRevision<SupplyBin>>(
    [...queryKey, "history", id],
    `${route}/${id}/revisions`,
    { request: { headers: supplyHeaders(scope) } },
  );
  return (
    <section aria-label="Bin revision history" className="space-y-4 rounded-xl border p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h6 className="font-semibold">Bin revision history</h6>
        <Button variant="ghost" onClick={onClose}>
          Close bin history
        </Button>
      </div>
      {history.isError ? (
        <RequestFailure message="Could not load retained bin history." retry={history.retry} />
      ) : history.isPending ? (
        <p role="status">Loading bin history…</p>
      ) : (
        <>
          {history.items.length === 0 ? (
            <p>No revisions recorded.</p>
          ) : (
            <ol className="divide-y">
              {history.items.map((row) => (
                <li key={row.id} className="space-y-1 py-3 text-sm">
                  <p className="font-medium">
                    Version {row.version} · {row.snapshot.code} · {row.snapshot.name}
                  </p>
                  <p>
                    {label(row.snapshot.status)} · {label(row.snapshot.storage_class)} ·{" "}
                    {[row.snapshot.aisle, row.snapshot.rack, row.snapshot.shelf]
                      .filter(Boolean)
                      .join(" / ") || "Position not specified"}
                  </p>
                  <p className="break-words text-muted-foreground">
                    {formatDateTime(row.created_at)} · {row.actor_subject}
                  </p>
                </li>
              ))}
            </ol>
          )}
          <LoadMore
            hasMore={history.hasMore}
            isLoading={history.isFetchingMore}
            onClick={history.loadMore}
          />
        </>
      )}
    </section>
  );
}
