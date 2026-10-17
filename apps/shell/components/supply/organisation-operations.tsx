"use client";

import { useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  newSupplyId,
  supplyHeaders,
  supplyKey,
  supplyDeliveryTaskFieldsSchema,
  supplyDeliveryTaskInputSchema,
  supplyDeliveryActionInputSchema,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  type SupplyRegistryScope,
  type SupplyOperatingLocation,
  type SupplyDeliveryTask,
  type SupplyAssignedDelivery,
  type SupplyDeliveryTaskInput,
  type SupplyDeliveryActionInput,
  type SupplyRegistryRevision,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, useZodForm } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";
import { WarehouseBins } from "./warehouse-bins";
import { dateTimeInputISO, dateTimeInputValue, formatAbsoluteTime } from "./time-input";

const baseFor = (scope: SupplyRegistryScope) => `/supply/parties/${scope.partyId}`;
const keyFor = (scope: SupplyRegistryScope, ...extra: unknown[]) =>
  supplyKey(scope, "party", scope.partyId, "operations", ...extra);
export function OrganisationOperations({
  scope,
  roles,
}: {
  scope: SupplyRegistryScope;
  roles: string[];
}) {
  const { can } = usePermissions();
  if (
    !can("supply.operations.read") &&
    !can("supply.delivery.read") &&
    !can("supply.delivery.assigned.read")
  )
    return null;
  return <OperationsPanel scope={scope} roles={roles} />;
}
function OperationsPanel({ scope, roles }: { scope: SupplyRegistryScope; roles: string[] }) {
  const { can } = usePermissions();
  const [selection, setSelection] = useState<{
    id: string;
    view: "bins" | "control" | "assigned";
  } | null>(null);
  const target = useRef<HTMLDivElement>(null);
  const locations = useApiPagedQuery<SupplyOperatingLocation>(
    keyFor(scope, "locations"),
    `${baseFor(scope)}/operating-locations`,
    { poll: true, request: { headers: supplyHeaders(scope) } },
  );
  const control =
    can("supply.delivery.read") &&
    roles.some((role) => ["administrator", "dispatcher", "auditor", "regulator"].includes(role));
  const assigned = can("supply.delivery.assigned.read") && roles.includes("driver");
  const bins =
    can("supply.operations.read") &&
    roles.some((role) =>
      [
        "administrator",
        "auditor",
        "warehouse",
        "inventory",
        "picker",
        "packer",
        "dispatcher",
        "regulator",
      ].includes(role),
    );
  const writableBins =
    can("supply.operations.write") &&
    roles.some((role) => ["administrator", "inventory"].includes(role));
  const selected = locations.items.find((row) => row.id === selection?.id);
  function select(id: string, view: "bins" | "control" | "assigned") {
    setSelection({ id, view });
    requestAnimationFrame(() => {
      target.current?.scrollIntoView({ block: "start", behavior: "instant" });
      target.current?.querySelector<HTMLElement>("h5")?.focus();
    });
  }
  return (
    <section aria-label="Organisation operations" className="space-y-5 rounded-xl border p-5">
      <div>
        <h4 className="font-semibold">Location operations</h4>
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
          Work with the locations and tasks explicitly assigned to your role. Operational access
          does not include private organisation, finance or clinical records.
        </p>
      </div>
      {locations.isError ? (
        <RequestFailure
          message="Operating locations are unavailable with your current access. Refresh before continuing."
          retry={locations.retry}
        />
      ) : locations.isPending ? (
        <p role="status">Loading operating locations…</p>
      ) : (
        <>
          {locations.items.length === 0 && (
            <p className="text-sm text-muted-foreground">
              No operating locations are currently assigned.
            </p>
          )}
          <ul className="divide-y">
            {locations.items.map((row) => (
              <li className="flex flex-wrap items-start justify-between gap-3 py-4" key={row.id}>
                <div className="min-w-0">
                  <p className="break-words font-medium">
                    {row.code} · {row.name}
                  </p>
                  <p className="text-sm text-muted-foreground">
                    {row.kind.replaceAll("_", " ")} · {row.country_code} · {row.status}
                  </p>
                  {row.status === "inactive" && (
                    <p className="mt-2 text-xs font-medium text-muted-foreground">
                      Archived · Read-only
                    </p>
                  )}
                </div>
                <div className="flex flex-wrap gap-2">
                  {row.status === "active" &&
                    bins &&
                    row.allowed_actions.includes("read_bins") &&
                    ["warehouse", "depot"].includes(row.kind) && (
                      <Button
                        variant="outline"
                        aria-label={`Bins ${row.code}`}
                        onClick={() => select(row.id, "bins")}
                      >
                        Storage bins
                      </Button>
                    )}
                  {control && row.allowed_actions.includes("read_deliveries") && (
                    <Button
                      variant="outline"
                      aria-label={`${row.status === "active" && row.allowed_actions.includes("manage_deliveries") ? "Manage" : "View"} deliveries ${row.code}`}
                      onClick={() => select(row.id, "control")}
                    >
                      {row.status === "active" && row.allowed_actions.includes("manage_deliveries")
                        ? "Manage deliveries"
                        : "View deliveries"}
                    </Button>
                  )}
                  {row.status === "active" &&
                    assigned &&
                    row.allowed_actions.includes("read_assigned_deliveries") && (
                      <Button
                        variant="outline"
                        aria-label={`My deliveries ${row.code}`}
                        onClick={() => select(row.id, "assigned")}
                      >
                        My deliveries
                      </Button>
                    )}
                </div>
              </li>
            ))}
          </ul>
          <LoadMore
            hasMore={locations.hasMore}
            isLoading={locations.isFetchingMore}
            onClick={locations.loadMore}
          />
        </>
      )}
      <div ref={target} className="scroll-mt-32">
        {!locations.isError &&
          selected &&
          selection &&
          (selected.status !== "active" && selection.view !== "control" ? (
            <p role="status" className="text-sm text-muted-foreground">
              This location is archived. Only delivery history is available.
            </p>
          ) : selection.view === "bins" ? (
            <WarehouseBins
              key={`${scope.tenantId}:${scope.subject}:${selected.id}`}
              scope={scope}
              premise={selected}
              roles={roles}
              operational={{
                read: bins && selected.allowed_actions.includes("read_bins"),
                write: writableBins && selected.allowed_actions.includes("manage_bins"),
              }}
              onClose={() => setSelection(null)}
            />
          ) : (
            <DeliveryPanel
              key={`${scope.tenantId}:${scope.subject}:${selected.id}:${selection.view}`}
              scope={scope}
              location={selected}
              assigned={selection.view === "assigned"}
              writable={
                selected.status === "active" &&
                can("supply.delivery.write") &&
                control &&
                selected.allowed_actions.includes("manage_deliveries")
              }
              onClose={() => setSelection(null)}
            />
          ))}
      </div>
    </section>
  );
}
function DeliveryPanel({
  scope,
  location,
  assigned,
  writable,
  onClose,
}: {
  scope: SupplyRegistryScope;
  location: SupplyOperatingLocation;
  assigned: boolean;
  writable: boolean;
  onClose: () => void;
}) {
  const { can } = usePermissions();
  const [editing, setEditing] = useState<{
    row: SupplyDeliveryTask | null;
    action: "edit" | "reassign" | "cancel";
  } | null>(null);
  const [action, setAction] = useState<{
    row: SupplyAssignedDelivery;
    action: SupplyDeliveryActionInput["action"];
  } | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const target = useRef<HTMLDivElement>(null);
  const route = `${baseFor(scope)}/premises/${location.id}/${assigned ? "my-deliveries" : "delivery-tasks"}`;
  const queryKey = keyFor(scope, location.id, assigned ? "my-deliveries" : "delivery-tasks");
  const rows = useApiPagedQuery<SupplyDeliveryTask | SupplyAssignedDelivery>(queryKey, route, {
    request: { headers: supplyHeaders(scope) },
  });
  const currentAction =
    action &&
    rows.items.find(
      (row) =>
        row.id === action.row.id &&
        row.version === action.row.version &&
        row.assignment_generation === action.row.assignment_generation &&
        row.allowed_actions.includes(action.action),
    );
  function focus() {
    requestAnimationFrame(() => {
      target.current?.scrollIntoView({ block: "start", behavior: "instant" });
      target.current?.querySelector<HTMLElement>("input,[role=combobox]")?.focus();
    });
  }
  return (
    <section aria-label="Delivery tasks" className="space-y-4 rounded-xl border p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h5 tabIndex={-1} className="font-medium">
            {assigned
              ? "My delivery tasks"
              : writable
                ? "Delivery task management"
                : "Delivery task review"}{" "}
            · {location.name}
          </h5>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Manual logistics scheduling and handoff tracking. These tasks do not move stock, create
            invoices or approve medicines for dispatch.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {!assigned && writable && !rows.isError && (
            <Button
              variant="outline"
              onClick={() => {
                setEditing({ row: null, action: "edit" });
                focus();
              }}
            >
              Schedule delivery task
            </Button>
          )}
          <Button variant="ghost" onClick={rows.retry}>
            Refresh delivery tasks
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Close delivery tasks
          </Button>
        </div>
      </div>
      {rows.isError ? (
        <RequestFailure
          message="Delivery tasks are unavailable. Your assignment or access may have changed."
          retry={rows.retry}
        />
      ) : rows.isPending ? (
        <p role="status">Loading delivery tasks…</p>
      ) : (
        <>
          {rows.items.length === 0 && (
            <p className="text-sm text-muted-foreground">
              No delivery tasks are currently available.
            </p>
          )}
          <ul className="space-y-3">
            {rows.items.map((row) => (
              <li className="space-y-3 rounded-lg border p-4" key={row.id}>
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="break-words font-medium">
                      {row.code} · {row.destination_business_name}
                    </p>
                    <p className="text-sm capitalize">
                      {row.status.replaceAll("_", " ")} · {row.package_count} packages · Version{" "}
                      {row.version}
                    </p>
                    <p className="mt-1 break-words text-sm text-muted-foreground">
                      {[
                        row.destination_line1,
                        row.destination_line2,
                        row.destination_city,
                        row.destination_region,
                        row.destination_postal_code,
                        row.country_code,
                      ]
                        .filter(Boolean)
                        .join(", ")}
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {formatAbsoluteTime(row.window_start)} to {formatAbsoluteTime(row.window_end)}
                    </p>
                    {(row.business_contact_name || row.business_contact_phone) && (
                      <p className="mt-1 break-words text-sm">
                        Business contact:{" "}
                        {[row.business_contact_name, row.business_contact_phone]
                          .filter(Boolean)
                          .join(" · ")}
                      </p>
                    )}
                    {row.handling_notes && (
                      <p className="mt-1 break-words text-sm">Handling: {row.handling_notes}</p>
                    )}
                    {!assigned && "driver_subject" in row && (
                      <p className="mt-1 break-words text-sm text-muted-foreground">
                        Driver: {row.driver_subject || "Not assigned"}
                      </p>
                    )}
                    {row.assignment_expires_at && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        Assignment expires {formatAbsoluteTime(row.assignment_expires_at)}
                      </p>
                    )}
                  </div>
                  <div className="flex flex-wrap items-start gap-2">
                    {assigned ? (
                      can("supply.delivery.assigned.action") &&
                      row.allowed_actions
                        .filter((value) => ["start", "deliver", "exception"].includes(value))
                        .map((value) => (
                          <Button
                            variant="outline"
                            key={value}
                            aria-label={`${value} ${row.code}`}
                            onClick={() => {
                              setAction({
                                row: row as SupplyAssignedDelivery,
                                action: value as SupplyDeliveryActionInput["action"],
                              });
                              focus();
                            }}
                          >
                            {value === "start"
                              ? "Start delivery"
                              : value === "deliver"
                                ? "Confirm delivery"
                                : "Report exception"}
                          </Button>
                        ))
                    ) : (
                      <>
                        {writable &&
                          row.allowed_actions
                            .filter((value) => ["edit", "reassign", "cancel"].includes(value))
                            .map((value) => (
                              <Button
                                key={value}
                                variant="outline"
                                aria-label={`${value} task ${row.code}`}
                                onClick={() => {
                                  setEditing({
                                    row: row as SupplyDeliveryTask,
                                    action: value as "edit" | "reassign" | "cancel",
                                  });
                                  focus();
                                }}
                              >
                                {value === "edit"
                                  ? "Edit task"
                                  : value === "reassign"
                                    ? "Reassign driver"
                                    : "Cancel task"}
                              </Button>
                            ))}
                        <Button
                          variant="ghost"
                          aria-label={`History delivery ${row.code}`}
                          onClick={() => setHistory(history === row.id ? null : row.id)}
                        >
                          History
                        </Button>
                      </>
                    )}
                  </div>
                </div>
                {!assigned && history === row.id && (
                  <DeliveryHistory scope={scope} location={location.id} id={row.id} />
                )}
              </li>
            ))}
          </ul>
          <LoadMore
            hasMore={rows.hasMore}
            isLoading={rows.isFetchingMore}
            onClick={rows.loadMore}
          />
        </>
      )}
      <div ref={target} className="scroll-mt-32">
        {!rows.isError && editing && !assigned && writable && (
          <DeliveryEditor
            key={`${editing.row?.id ?? "new"}:${editing.row?.version ?? 0}:${editing.action}`}
            scope={scope}
            location={location}
            row={editing.row}
            mode={editing.action}
            onDone={() => setEditing(null)}
          />
        )}
        {!rows.isError &&
          action &&
          currentAction &&
          assigned &&
          can("supply.delivery.assigned.action") && (
            <DeliveryAction
              key={`${action.row.id}:${action.row.version}:${action.action}`}
              scope={scope}
              location={location.id}
              row={action.row}
              action={action.action}
              onDone={() => setAction(null)}
            />
          )}
      </div>
    </section>
  );
}
function DeliveryEditor({
  scope,
  location,
  row,
  mode,
  onDone,
}: {
  scope: SupplyRegistryScope;
  location: SupplyOperatingLocation;
  row: SupplyDeliveryTask | null;
  mode: "edit" | "reassign" | "cancel";
  onDone: () => void;
}) {
  const id = useRef(row?.id ?? newSupplyId());
  const status =
    mode === "cancel"
      ? "cancelled"
      : row?.status === "draft"
        ? "draft"
        : row
          ? "assigned"
          : "draft";
  const initial = row
    ? {
        ...supplyDeliveryTaskFieldsSchema.strip().parse({ ...row, status }),
        expected_version: row.version,
        change_reason: "",
      }
    : {
        expected_version: 0,
        code: "",
        source_reference: "",
        destination_business_name: "",
        destination_line1: "",
        destination_line2: "",
        destination_city: "",
        destination_region: "",
        destination_postal_code: "",
        country_code: location.country_code,
        business_contact_name: "",
        business_contact_phone: "",
        window_start: "",
        window_end: "",
        package_count: 1,
        handling_notes: "",
        driver_subject: "",
        assignment_expires_at: "",
        status: "draft" as const,
        change_reason: "",
      };
  const form = useZodForm(supplyDeliveryTaskInputSchema, initial);
  const drivers = useApiPagedQuery<{ subject: string }>(
    keyFor(scope, location.id, "delivery-drivers"),
    `${baseFor(scope)}/premises/${location.id}/delivery-drivers`,
    { poll: true, request: { headers: supplyHeaders(scope) } },
  );
  const driverOptions = [
    ...new Set([
      ...drivers.items.map((value) => value.subject),
      ...(row?.driver_subject ? [row.driver_subject] : []),
    ]),
  ];
  const save = useApiMutation(
    (value: SupplyDeliveryTaskInput) =>
      apiFetch<SupplyDeliveryTask>(
        `${baseFor(scope)}/premises/${location.id}/delivery-tasks/${id.current}`,
        { method: "PUT", headers: supplyHeaders(scope), body: JSON.stringify(value) },
      ),
    {
      invalidate: [keyFor(scope, location.id, "delivery-tasks")],
      successToast: "Delivery task recorded",
      onSuccess: onDone,
      suppressErrorToast: true,
    },
  );
  const error = (name: keyof SupplyDeliveryTaskInput) => form.formState.errors[name]?.message;
  const cancelled = mode === "cancel";
  return (
    <form
      noValidate
      aria-label="Delivery task form"
      className="space-y-4 rounded-xl bg-muted/30 p-4"
      onSubmit={form.handleSubmit((value) => {
        if (!save.isPending && (!drivers.isError || cancelled)) save.mutate(value);
      })}
    >
      <h6 className="font-medium">
        {cancelled
          ? "Cancel delivery task"
          : mode === "reassign"
            ? "Reassign delivery driver"
            : row
              ? "Edit delivery task"
              : "New delivery task"}
      </h6>
      <div className="grid gap-4 sm:grid-cols-2">
        {(
          [
            ["code", "Task code"],
            ["source_reference", "Business handoff reference"],
            ["destination_business_name", "Destination business"],
            ["destination_line1", "Destination address"],
            ["destination_line2", "Address line 2"],
            ["destination_city", "Destination city"],
            ["destination_region", "Destination state or province"],
            ["destination_postal_code", "Destination postal code"],
            ["business_contact_name", "Business contact name"],
            ["business_contact_phone", "Business contact phone"],
            ["handling_notes", "Handling instructions"],
          ] as const
        ).map(([name, title]) => (
          <Field key={name} id={`delivery-${name}`} label={title} error={error(name)}>
            <Input
              id={`delivery-${name}`}
              {...form.register(name)}
              readOnly={!!row && ["code", "source_reference"].includes(name)}
              disabled={save.isPending || cancelled}
              aria-invalid={!!error(name)}
              aria-describedby={error(name) ? `delivery-${name}-error` : undefined}
            />
          </Field>
        ))}
        <Field id="delivery-packages" label="Package count" error={error("package_count")}>
          <Input
            id="delivery-packages"
            type="number"
            min={1}
            {...form.register("package_count", { valueAsNumber: true })}
            disabled={save.isPending || cancelled}
            aria-invalid={!!error("package_count")}
            aria-describedby={error("package_count") ? "delivery-packages-error" : undefined}
          />
        </Field>
      </div>
      <p className="text-sm text-muted-foreground">
        Origin: {location.code} · Country: {location.country_code}. Use a business delivery
        destination; clinical and patient records are not part of this task.
      </p>
      <div className="grid gap-4 sm:grid-cols-2">
        {(["window_start", "window_end", "assignment_expires_at"] as const).map((name) => (
          <Field
            key={name}
            id={`delivery-${name}`}
            label={
              name === "window_start"
                ? "Delivery window starts"
                : name === "window_end"
                  ? "Delivery window ends"
                  : "Driver assignment expires"
            }
            error={error(name)}
          >
            <Input
              id={`delivery-${name}`}
              type="datetime-local"
              disabled={save.isPending || cancelled}
              value={dateTimeInputValue(form.watch(name))}
              onChange={(event) =>
                form.setValue(name, dateTimeInputISO(event.target.value), {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              aria-invalid={!!error(name)}
              aria-describedby={error(name) ? `delivery-${name}-error` : undefined}
            />
          </Field>
        ))}
      </div>
      <FormSelectField
        control={form.control}
        name="driver_subject"
        id="delivery-driver"
        label="Assigned driver"
        disabled={save.isPending || cancelled || drivers.isPending || drivers.isError}
        options={[
          { value: "", label: "No driver assigned" },
          ...driverOptions.map((subject) => ({ value: subject, label: subject })),
        ]}
      />
      <LoadMore
        hasMore={drivers.hasMore}
        isLoading={drivers.isFetchingMore}
        onClick={drivers.loadMore}
      />
      {drivers.isError && (
        <RequestFailure
          message="Could not load eligible drivers. Refresh before assigning or reassigning this task."
          retry={drivers.retry}
        />
      )}
      <FormSelectField
        control={form.control}
        name="status"
        id="delivery-status"
        label="Task state"
        disabled={save.isPending || cancelled || mode === "reassign"}
        options={
          cancelled
            ? [{ value: "cancelled", label: "Cancelled" }]
            : mode === "reassign"
              ? [{ value: "assigned", label: "Assigned to replacement driver" }]
              : [
                  { value: "draft", label: "Draft" },
                  { value: "assigned", label: "Assigned" },
                ]
        }
      />
      <Field
        id="delivery-change-reason"
        label="Reason for scheduling or change"
        error={error("change_reason")}
      >
        <Input
          id="delivery-change-reason"
          {...form.register("change_reason")}
          disabled={save.isPending}
          aria-invalid={!!error("change_reason")}
          aria-describedby={error("change_reason") ? "delivery-change-reason-error" : undefined}
        />
      </Field>
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message} Close this form and reload tasks if the assignment or version
          changed.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={save.isPending || (drivers.isError && !cancelled)}>
          {save.isPending
            ? "Recording…"
            : cancelled
              ? "Confirm task cancellation"
              : "Save delivery task"}
        </Button>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onDone}>
          Close task form
        </Button>
      </div>
    </form>
  );
}
function DeliveryAction({
  scope,
  location,
  row,
  action,
  onDone,
}: {
  scope: SupplyRegistryScope;
  location: string;
  row: SupplyAssignedDelivery;
  action: SupplyDeliveryActionInput["action"];
  onDone: () => void;
}) {
  const id = useRef(newSupplyId());
  const form = useZodForm(supplyDeliveryActionInputSchema, {
    expected_version: row.version,
    expected_assignment_generation: row.assignment_generation,
    action,
    reason: "",
  });
  const save = useApiMutation(
    (value: SupplyDeliveryActionInput) =>
      apiFetch<SupplyAssignedDelivery>(
        `${baseFor(scope)}/premises/${location}/my-deliveries/${row.id}/actions/${id.current}`,
        { method: "PUT", headers: supplyHeaders(scope), body: JSON.stringify(value) },
      ),
    {
      invalidate: [keyFor(scope, location, "my-deliveries")],
      successToast: "Delivery update recorded",
      onSuccess: onDone,
      suppressErrorToast: true,
    },
  );
  return (
    <form
      noValidate
      aria-label="Driver delivery update"
      className="space-y-3 rounded-xl bg-muted/30 p-4"
      onSubmit={form.handleSubmit((value) => {
        if (row.allowed_actions.includes(value.action) && !save.isPending) save.mutate(value);
      })}
    >
      <h6 className="font-medium">
        {action === "start"
          ? "Start delivery"
          : action === "deliver"
            ? "Confirm delivery"
            : "Report delivery exception"}{" "}
        · {row.code}
      </h6>
      <Field
        id="delivery-action-reason"
        label="Handoff or exception note"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="delivery-action-reason"
          {...form.register("reason")}
          disabled={save.isPending}
          aria-invalid={!!form.formState.errors.reason}
        />
      </Field>
      <p className="text-xs text-muted-foreground">
        This update applies to your current assignment. A reassignment, expiry or revoked role
        prevents submission.
      </p>
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message} Reload your assigned tasks before continuing.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record delivery update"}
        </Button>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onDone}>
          Cancel delivery update
        </Button>
      </div>
    </form>
  );
}
function DeliveryHistory({
  scope,
  location,
  id,
}: {
  scope: SupplyRegistryScope;
  location: string;
  id: string;
}) {
  const rows = useApiPagedQuery<SupplyRegistryRevision<SupplyDeliveryTask>>(
    keyFor(scope, location, "delivery-tasks", id, "revisions"),
    `${baseFor(scope)}/premises/${location}/delivery-tasks/${id}/revisions`,
    { request: { headers: supplyHeaders(scope) } },
  );
  if (rows.isError)
    return <RequestFailure message="Could not load delivery task history." retry={rows.retry} />;
  if (rows.isPending) return <p role="status">Loading delivery history…</p>;
  return (
    <section aria-label="Delivery task history" className="space-y-2 border-t pt-3">
      {rows.items.map((row) => (
        <p className="break-words text-sm" key={row.id}>
          Version {row.version} · {formatAbsoluteTime(row.created_at)} · Actor {row.actor_subject} ·{" "}
          {row.snapshot.status} · Driver {row.snapshot.driver_subject || "Not assigned"}
        </p>
      ))}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
    </section>
  );
}
