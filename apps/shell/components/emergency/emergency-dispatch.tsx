"use client";

import { useState, useRef, useId } from "react";
import { Badge, Button, Input } from "@tesserix/web";
import {
  newEmergencyId,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  emergencyClassSupports,
  emergencyDispatchReservationSchema,
  emergencyDispatchCommandSchema,
  EMERGENCY_DISPATCH_STATES,
  type EmergencyDispatch,
  type EmergencyDispatchEligibility,
  type EmergencyDispatchReservationInput,
  type EmergencyDispatchCommandInput,
  type EmergencyVehicle,
} from "@helivanta/api";
import { LoadMore, formatTime } from "@helivanta/ui";
import { useEmergencyCommand, EmergencyCommandFailure } from "./emergency-command";

export function EmergencyDispatchPanel({ baseID, caseID }: { baseID: string; caseID: string }) {
  const { can } = usePermissions();
  const canManage = can("emergency.dispatch.manage");
  const canRead = can("emergency.case.read");
  const path = `/emergency/bases/${encodeURIComponent(baseID)}/cases/${encodeURIComponent(caseID)}`;
  const eligibility = useApiQuery<EmergencyDispatchEligibility>(
    ["emergency", "dispatch-eligibility", baseID, caseID],
    `${path}/dispatch-eligibility`,
    { enabled: canManage, gcTime: 0, refetchOnWindowFocus: true, request: { cache: "no-store" } },
  );
  const rows = useApiPagedQuery<EmergencyDispatch>(
    ["emergency", "dispatches", baseID, caseID],
    `${path}/dispatches`,
    { enabled: canRead, poll: true, request: { cache: "no-store" } },
  );
  const access = useApiQuery<{ level: string }>(
    ["emergency", "access", baseID],
    `/emergency/bases/${baseID}/access`,
    { enabled: canManage, gcTime: 0 },
  );
  if (!canRead && !canManage) return null;
  const active = rows.items.find((row) => row.state !== "cleared" && row.state !== "cancelled");
  return (
    <section
      aria-label="Dispatch and crew status"
      className="space-y-4 rounded-xl border bg-card p-5"
    >
      <h2 className="text-lg font-semibold">Dispatch and crew status</h2>
      <p className="text-sm text-muted-foreground">
        A reservation holds capacity. Crew acknowledgement and departure are recorded separately.
      </p>
      {rows.isError && (
        <p role="alert">
          {rows.error?.message} <Button onClick={rows.retry}>Refresh dispatches</Button>
        </p>
      )}
      {rows.items.map((row) => (
        <article key={row.id} className="space-y-3 rounded-lg border p-4">
          <Badge>{EMERGENCY_DISPATCH_STATES[row.state] ?? "Status requires review"}</Badge>
          <dl className="grid gap-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">Driver acknowledgement</dt>
              <dd>{row.driver_acknowledged ? "Recorded" : "Pending"}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Clinical crew acknowledgement</dt>
              <dd>{row.clinician_acknowledged ? "Recorded" : "Pending"}</dd>
            </div>
            {row.acknowledgement_deadline && (
              <div>
                <dt className="text-muted-foreground">Acknowledgement deadline</dt>
                <dd>{formatTime(row.acknowledgement_deadline)}</dd>
              </div>
            )}
          </dl>
          {canManage && row.state !== "cleared" && row.state !== "cancelled" && (
            <DispatchCancellation
              key={`${row.id}-${row.version}`}
              row={row}
              supervisor={can("emergency.case.close") && access.data?.level === "supervisor"}
            />
          )}
        </article>
      ))}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
      {canManage && eligibility.isError && (
        <p role="alert">
          {eligibility.error.message}{" "}
          <Button onClick={() => void eligibility.refetch()}>Refresh review eligibility</Button>
        </p>
      )}
      {canManage && eligibility.data && !active && !rows.isPending && !rows.isError && (
        <>
          {!eligibility.data.dispatch_enabled ? (
            <p role="status">Dispatch activation is not enabled for this service.</p>
          ) : !eligibility.data.eligible ? (
            <p role="status">
              A current, independently authorized transport review is required before reserving
              capacity.
            </p>
          ) : (
            <ReservationForm
              key={`${caseID}-${eligibility.data.assessment_id}`}
              baseID={baseID}
              eligibility={eligibility.data}
            />
          )}
        </>
      )}
    </section>
  );
}
function ReservationForm({
  baseID,
  eligibility,
}: {
  baseID: string;
  eligibility: EmergencyDispatchEligibility;
}) {
  const secondsID = useId();
  const [vehicleID, setVehicle] = useState("");
  const [seconds, setSeconds] = useState("");
  const [error, setError] = useState<string>();
  const [saved, setSaved] = useState(false);
  const commandID = useRef<string | null>(null);
  const command = useEmergencyCommand<EmergencyDispatchReservationInput, EmergencyDispatch>(
    baseID,
    () => `dispatches/${(commandID.current ??= newEmergencyId())}`,
    "PUT",
    "Reservation receipt saved",
    () => {
      commandID.current = null;
      setSaved(true);
    },
    [
      ["emergency", "dispatches", baseID],
      ["emergency", "dispatch-eligibility", baseID],
      ["emergency", "fleet", baseID],
    ],
  );
  const fleet = useApiPagedQuery<EmergencyVehicle>(
    ["emergency", "fleet", baseID],
    `/emergency/bases/${baseID}/fleet`,
    { poll: true, request: { cache: "no-store" } },
  );
  const vehicles = fleet.items.filter(
    (row) =>
      row.dispatchable &&
      row.duty_id &&
      emergencyClassSupports(row.effective_capability, eligibility.required_class),
  );
  async function reserve() {
    const vehicle = vehicles.find((row) => row.id === vehicleID);
    const result = emergencyDispatchReservationSchema.safeParse({
      case_id: eligibility.case_id,
      case_version: eligibility.case_version,
      assessment_id: eligibility.assessment_id,
      vehicle_id: vehicle?.id,
      duty_id: vehicle?.duty_id,
      acknowledgement_seconds: Number(seconds),
    });
    if (!result.success) {
      setError(
        "Choose a currently ready vehicle and an acknowledgement window from 15 to 300 seconds.",
      );
      return;
    }
    setError(undefined);
    try {
      await command.mutateAsync(result.data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reservation could not be confirmed");
    }
  }
  async function retry() {
    setError(undefined);
    try {
      if (command.variables) await command.mutateAsync(command.variables);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reservation could not be confirmed");
    }
  }
  return (
    <div className="space-y-4">
      {saved && (
        <p role="status">Reservation receipt saved. Follow the current crew status above.</p>
      )}
      <p className="text-sm text-muted-foreground">
        Choose a ready vehicle that satisfies the reviewed transport requirement. Current evidence
        is checked again when the reservation is saved and before crew departure.
      </p>
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault();
          void reserve();
        }}
      >
        <label className="space-y-2">
          Ready vehicle
          <select
            aria-label="Ready vehicle"
            className="min-h-11 w-full rounded-lg border bg-background px-3"
            value={vehicleID}
            onChange={(event) => setVehicle(event.target.value)}
            disabled={command.frozen || fleet.isPending || fleet.isError}
            required
          >
            <option value="">Choose a ready vehicle</option>
            {vehicles.map((row) => (
              <option key={row.id} value={row.id}>
                {row.code}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-2" htmlFor={secondsID}>
          Crew acknowledgement window (seconds)
          <Input
            id={secondsID}
            className="min-h-11"
            type="number"
            min={15}
            max={300}
            step={1}
            value={seconds}
            onChange={(event) => setSeconds(event.target.value)}
            disabled={command.frozen}
            required
          />
        </label>
        <div className="sm:col-span-2">
          <Button
            className="min-h-11"
            type="submit"
            disabled={command.frozen || fleet.isRefreshing || fleet.isPending || fleet.isError}
          >
            Reserve vehicle and crew
          </Button>
        </div>
      </form>
      {fleet.isError && (
        <p role="alert">
          {fleet.error?.message} <Button onClick={fleet.retry}>Refresh fleet</Button>
        </p>
      )}
      <LoadMore hasMore={fleet.hasMore} isLoading={fleet.isFetchingMore} onClick={fleet.loadMore} />
      <EmergencyCommandFailure message={error} uncertain={command.uncertain} />
      {command.uncertain && (
        <Button className="min-h-11" disabled={command.isPending} onClick={() => void retry()}>
          Retry unchanged reservation
        </Button>
      )}
    </div>
  );
}
function DispatchCancellation({
  row,
  supervisor,
}: {
  row: EmergencyDispatch;
  supervisor: boolean;
}) {
  const referenceID = useId();
  const [reference, setReference] = useState("");
  const [error, setError] = useState<string>();
  const commandID = useRef<string | null>(null);
  const command = useEmergencyCommand<EmergencyDispatchCommandInput>(
    row.base_id,
    () => `dispatches/${row.id}/commands/${(commandID.current ??= newEmergencyId())}`,
    "PUT",
    "Dispatch command recorded",
    () => {
      commandID.current = null;
    },
    [
      ["emergency", "dispatches", row.base_id],
      ["emergency", "dispatch-eligibility", row.base_id],
      ["emergency", "fleet", row.base_id],
    ],
  );
  const reconciling = row.state === "cancel_requested" || row.state === "reconciliation_required";
  const operation = reconciling ? "reconcile_cancelled" : "request_cancel";
  async function execute() {
    const parsed = emergencyDispatchCommandSchema.safeParse({
      version: row.version,
      operation,
      evidence_reference: reference,
    });
    if (!parsed.success) {
      setError("Use an operational confirmation reference without personal or clinical details.");
      return;
    }
    try {
      setError(undefined);
      await command.mutateAsync(parsed.data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Command could not be confirmed");
    }
  }
  if (reconciling && !supervisor)
    return (
      <p className="text-sm">
        Capacity remains held until a scoped supervisor confirms reconciliation.
      </p>
    );
  return (
    <div className="space-y-3 border-t pt-3">
      <p className="text-sm text-muted-foreground">
        {reconciling
          ? "Confirm the crew was contacted and this assignment can safely stand down before releasing capacity."
          : "Requesting cancellation keeps capacity held until a supervisor confirms the outcome with the crew."}
      </p>
      <label className="block space-y-2" htmlFor={referenceID}>
        Operational confirmation reference
        <Input
          className="min-h-11"
          id={referenceID}
          value={reference}
          onChange={(event) => setReference(event.target.value)}
          disabled={command.frozen}
        />
      </label>
      <Button className="min-h-11" disabled={command.frozen} onClick={() => void execute()}>
        {reconciling ? "Confirm reconciled cancellation" : "Request cancellation"}
      </Button>
      <EmergencyCommandFailure message={error} uncertain={command.uncertain} />
      {command.uncertain && (
        <Button
          disabled={command.isPending}
          onClick={() => {
            if (command.variables)
              void command
                .mutateAsync(command.variables)
                .catch((err: unknown) =>
                  setError(err instanceof Error ? err.message : "Command could not be confirmed"),
                );
          }}
        >
          Retry unchanged command
        </Button>
      )}
    </div>
  );
}
