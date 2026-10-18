"use client";

import { useRef, useState, useId } from "react";
import { Badge, Button, Input } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  newEmergencyId,
  emergencyDispatchCommandSchema,
  EMERGENCY_DISPATCH_STATES,
  type EmergencyDispatch,
  type EmergencyCrewDispatch,
  type EmergencyCaseDetail,
  type EmergencyDispatchCommandInput,
  type EmergencyDispatchCommandReceipt,
} from "@helivanta/api";
import { LoadMore, formatTime } from "@helivanta/ui";
import { EmergencyCommandFailure } from "./emergency-command";

export function EmergencyCrewWorkspace() {
  const { can } = usePermissions();
  const allowed = can("emergency.crew.read");
  const rows = useApiPagedQuery<EmergencyDispatch>(
    ["emergency", "crew", "assignments"],
    "/emergency/crew/assignments",
    {
      enabled: allowed,
      poll: true,
      request: { cache: "no-store" },
    },
  );
  const [selected, select] = useState<EmergencyDispatch | null>(null);
  if (!allowed) return <p>Assigned crew access is required.</p>;
  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <header>
        <h1 className="text-2xl font-semibold">My crew assignments</h1>
        <p className="mt-2 text-muted-foreground">
          Confirm each action when it happens. Both crew members must acknowledge before departure.
        </p>
      </header>
      <Button variant="outline" onClick={rows.retry}>
        Refresh assignments
      </Button>
      {rows.isPending && <p role="status">Checking assigned work…</p>}
      {rows.isError ? (
        <p role="alert">Assignments unavailable. Refresh before acting.</p>
      ) : (
        <ul className="space-y-3">
          {!rows.isPending && rows.items.length === 0 && <li>No offered assignments.</li>}
          {rows.items.map((row) => (
            <li
              key={row.id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-card p-4"
            >
              <Badge>{EMERGENCY_DISPATCH_STATES[row.state]}</Badge>
              <Button className="min-h-11" onClick={() => select(row)}>
                Open assignment
              </Button>
            </li>
          ))}
        </ul>
      )}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
      {selected && (
        <CrewAssignment
          key={selected.id}
          baseID={selected.base_id}
          id={selected.id}
          canAct={can("emergency.crew.act")}
          close={() => select(null)}
        />
      )}
    </div>
  );
}

function CrewAssignment({
  baseID,
  id,
  canAct,
  close,
}: {
  baseID: string;
  id: string;
  canAct: boolean;
  close: () => void;
}) {
  const path = `/emergency/crew/bases/${encodeURIComponent(baseID)}/dispatches/${encodeURIComponent(id)}`;
  const row = useApiQuery<EmergencyCrewDispatch>(["emergency", "crew", baseID, id], path, {
    poll: true,
    gcTime: 0,
    refetchOnWindowFocus: true,
    request: { cache: "no-store" },
  });
  const incident = useApiQuery<EmergencyCaseDetail>(
    ["emergency", "crew", baseID, id, "incident"],
    `${path}/incident`,
    {
      enabled: !!row.data && !row.isError,
      poll: true,
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { cache: "no-store" },
    },
  );
  return (
    <section
      aria-label="Current crew assignment"
      className="space-y-4 rounded-xl border bg-card p-5"
    >
      <Button variant="outline" onClick={close}>
        Close assignment
      </Button>
      <Button
        variant="outline"
        onClick={() => {
          void row.refetch();
          void incident.refetch();
        }}
      >
        Refresh current assignment
      </Button>
      {row.isError || incident.isError ? (
        <div role="alert">
          <p>
            Assignment or incident access is unavailable. Contact dispatch and refresh before
            acting.
          </p>
          <Button
            onClick={() => {
              void row.refetch();
              void incident.refetch();
            }}
          >
            Refresh access
          </Button>
        </div>
      ) : !row.data || !incident.data ? (
        <p role="status">Checking current assignment and protected incident…</p>
      ) : (
        <>
          <h2 className="text-xl font-semibold">{EMERGENCY_DISPATCH_STATES[row.data.state]}</h2>
          <dl className="grid gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">Reported location</dt>
              <dd className="whitespace-pre-wrap break-words">
                {incident.data.incident.location_text || "Not supplied"}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Reported situation</dt>
              <dd className="whitespace-pre-wrap break-words">
                {incident.data.incident.patient_description || "Not supplied"}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Callback</dt>
              <dd>{incident.data.contact?.callback_phone || "Not available"}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Acknowledgement deadline</dt>
              <dd>
                {row.data.acknowledgement_deadline
                  ? formatTime(row.data.acknowledgement_deadline)
                  : "Not offered"}
              </dd>
            </div>
          </dl>
          {row.data.own_acknowledged && <p role="status">Your acknowledgement is recorded.</p>}
          {["cancel_requested", "reconciliation_required"].includes(row.data.state) && (
            <p role="status">
              Contact dispatch for reconciliation. Capacity remains held until stand-down is
              confirmed.
            </p>
          )}
        </>
      )}
      {canAct && (
        <CrewAction
          path={path}
          baseID={baseID}
          row={row.data}
          visible={!!row.data && !row.isError && !!incident.data && !incident.isError}
        />
      )}
    </section>
  );
}

function CrewAction({
  path,
  baseID,
  row,
  visible,
}: {
  path: string;
  baseID: string;
  row?: EmergencyCrewDispatch;
  visible: boolean;
}) {
  const referenceID = useId();
  const [reference, setReference] = useState("");
  const [validation, setValidation] = useState<string>();
  const attempt = useRef<{ id: string; body: EmergencyDispatchCommandInput } | null>(null);
  const mutation = useApiMutation(
    async (input: EmergencyDispatchCommandInput) => {
      attempt.current ??= { id: newEmergencyId(), body: structuredClone(input) };
      try {
        return await apiFetch<EmergencyDispatchCommandReceipt>(
          `${path}/commands/${attempt.current.id}`,
          {
            method: "PUT",
            body: JSON.stringify(attempt.current.body),
          },
        );
      } catch (error) {
        if (error instanceof ApiError && [400, 413].includes(error.status)) attempt.current = null;
        throw error;
      }
    },
    {
      networkMode: "always",
      suppressErrorToast: true,
      invalidate: [
        ["emergency", "crew"],
        ["emergency", "dispatches", baseID],
      ],
      onSuccess: () => {
        attempt.current = null;
        setReference("");
      },
    },
  );
  if (!row || !visible) return null;
  const currentVersion = row.version;
  const operation =
    row.state === "offered" && !row.own_acknowledged
      ? "acknowledge"
      : row.state === "acknowledged"
        ? "en_route"
        : row.state === "en_route"
          ? "on_scene"
          : null;
  const uncertain =
    mutation.isError &&
    !(mutation.error instanceof ApiError && [400, 413].includes(mutation.error.status));
  const frozen = mutation.isPending || uncertain;
  const labels = {
    acknowledge: "Acknowledge assignment",
    en_route: "Record departure",
    on_scene: "Record arrival on scene",
  };
  if (mutation.data && mutation.data.version > row.version) {
    return (
      <p role="status">Command recorded. Refresh the current assignment before another action.</p>
    );
  }
  if (!operation && !uncertain) return null;
  async function submit() {
    if (!navigator.onLine) {
      setValidation(
        "You are offline. Contact dispatch; actions are not queued for later delivery.",
      );
      return;
    }
    const body = uncertain
      ? attempt.current?.body
      : { version: currentVersion, operation, evidence_reference: reference };
    const parsed = emergencyDispatchCommandSchema.safeParse(body);
    if (!parsed.success) {
      setValidation("Enter an operational reference without patient or contact details.");
      return;
    }
    setValidation(undefined);
    try {
      await mutation.mutateAsync(parsed.data);
    } catch {
      /* Error is rendered with the retained command. */
    }
  }
  return (
    <form
      className="space-y-3"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <label className="block space-y-2" htmlFor={referenceID}>
        <span>Operational evidence reference</span>
        <Input
          id={referenceID}
          value={reference}
          maxLength={120}
          disabled={frozen}
          onChange={(event) => setReference(event.target.value)}
        />
      </label>
      {validation && <p role="alert">{validation}</p>}
      <EmergencyCommandFailure message={mutation.error?.message} uncertain={uncertain} />
      <Button className="min-h-11 w-full sm:w-auto" disabled={mutation.isPending} type="submit">
        {uncertain
          ? "Retry unchanged command"
          : operation
            ? labels[operation]
            : "Refresh current assignment"}
      </Button>
      <p className="text-sm text-muted-foreground">
        These actions require a connection. If connectivity fails, contact dispatch and retry the
        original command when connected.
      </p>
    </form>
  );
}
