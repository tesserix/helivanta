"use client";
import { useId, useRef, useState } from "react";
import { Badge, Button, Input } from "@tesserix/web";
import {
  usePermissions,
  useApiPagedQuery,
  useApiQuery,
  newEmergencyId,
  emergencyReceivingLinkSchema,
  emergencyReceivingLinkCommandSchema,
  emergencyReceivingRequestSchema,
  EMERGENCY_RECEIVING_STATES,
  EMERGENCY_RECEIVING_LINK_STATES,
  type EmergencyBase,
  type EmergencyDispatch,
  type EmergencyReceivingLink,
  type EmergencyReceivingLinkInput,
  type EmergencyReceivingLinkCommandInput,
  type EmergencyReceivingRequest,
  type EmergencyReceivingRequestInput,
  type EmergencyReceivingReceipt,
} from "@helivanta/api";
import { LoadMore, formatTime } from "@helivanta/ui";
import { useEmergencyCommand, EmergencyCommandFailure } from "./emergency-command";
const invalidates = [
  ["emergency", "receiving"],
  ["emergency", "crew"],
  ["emergency", "dispatches"],
];
const selectClass = "min-h-11 w-full rounded-md border bg-background px-3 py-2";
export function useEmergencyReceivingTrip(
  baseID: string,
  dispatchID: string,
  crew = false,
  enabled = true,
) {
  const prefix = `/emergency/${crew ? "crew/" : ""}bases/${encodeURIComponent(baseID)}/dispatches/${encodeURIComponent(dispatchID)}/receiving`;
  const records = useApiPagedQuery<EmergencyReceivingRequest>(
    ["emergency", "receiving", "trip", prefix],
    prefix,
    { enabled, poll: true, request: { cache: "no-store" } },
  );
  const latest = records.items.reduce<EmergencyReceivingRequest | undefined>(
    (current, item) => (!current || item.sequence > current.sequence ? item : current),
    undefined,
  );
  const current = useApiQuery<EmergencyReceivingRequest>(
    ["emergency", "receiving", "current", prefix, latest?.id],
    `${prefix}/${latest?.id ?? ""}`,
    {
      enabled: enabled && !!latest && !records.isError,
      poll: true,
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { cache: "no-store" },
    },
  );
  return { records, current, latest };
}
export function ReceivingTripStatus({ request }: { request?: EmergencyReceivingRequest }) {
  if (!request)
    return (
      <p className="text-sm text-muted-foreground">
        No hospital destination has been accepted for this trip.
      </p>
    );
  return (
    <div className="space-y-2 rounded-lg border p-4">
      <Badge>{EMERGENCY_RECEIVING_STATES[request.status]}</Badge>
      <p className="font-medium">{request.destination_name ?? "Receiving decision pending"}</p>
      <p className="break-all text-sm text-muted-foreground">Campus: {request.facility_id}</p>
      {request.acceptance_valid_until && (
        <p className="text-sm">Acceptance lease: {formatTime(request.acceptance_valid_until)}</p>
      )}
      {request.status === "accepted" && !request.usable && (
        <p role="status" className="text-sm">
          Acceptance is not currently actionable. Contact dispatch before transport.
        </p>
      )}
      {request.superseded && <p role="status">This pre-alert was superseded.</p>}
    </div>
  );
}
export function EmergencyReceivingDispatch({
  baseID,
  row,
}: {
  baseID: string;
  row: EmergencyDispatch;
}) {
  const { can } = usePermissions();
  const carePhase = [
    "on_scene",
    "transporting",
    "arrived",
    "handover_complete",
    "cleared",
  ].includes(row.state);
  const allowed = can("emergency.dispatch.manage") && carePhase;
  const receiving = useEmergencyReceivingTrip(baseID, row.id, false, allowed);
  const links = useApiPagedQuery<EmergencyReceivingLink>(
    ["emergency", "receiving", "provider-links", baseID],
    `/emergency/bases/${baseID}/receiving-links`,
    { enabled: allowed, poll: true, request: { cache: "no-store" } },
  );
  if (!allowed) return null;
  const failed = receiving.records.isError || receiving.current.isError;
  const checking =
    receiving.records.isPending || (!!receiving.latest && receiving.current.isPending);
  const current = failed ? undefined : receiving.current.data;
  const canRequest =
    ["on_scene", "transporting", "arrived"].includes(row.state) &&
    current?.status !== "handover_received";
  return (
    <section
      aria-label="Receiving hospital plan"
      className="space-y-4 rounded-xl border bg-card p-4"
    >
      <h3 className="font-semibold">Receiving hospital plan</h3>
      <p className="text-sm text-muted-foreground">
        A service link permits a pre-alert. Only a current hospital acceptance permits transport.
      </p>
      {failed ? (
        <div role="alert">
          Receiving status is unavailable.
          <Button
            variant="outline"
            onClick={() => {
              receiving.records.retry();
              void receiving.current.refetch();
            }}
          >
            Refresh receiving plan
          </Button>
        </div>
      ) : checking ? (
        <p role="status">Checking the current destination…</p>
      ) : (
        <ReceivingTripStatus request={current} />
      )}
      {canRequest && !failed && !checking && (
        <PreAlertForm
          baseID={baseID}
          row={row}
          previous={receiving.latest}
          links={links.items}
          unavailable={links.isPending || links.isError}
          observed={receiving.records.items}
        />
      )}
      <LoadMore hasMore={links.hasMore} isLoading={links.isFetchingMore} onClick={links.loadMore} />
    </section>
  );
}
function PreAlertForm({
  baseID,
  row,
  previous,
  links,
  unavailable,
  observed,
}: {
  baseID: string;
  row: EmergencyDispatch;
  previous?: EmergencyReceivingRequest;
  links: EmergencyReceivingLink[];
  unavailable: boolean;
  observed: EmergencyReceivingRequest[];
}) {
  const prefix = useId(),
    id = useRef(newEmergencyId());
  const [link, setLink] = useState(""),
    [reference, setReference] = useState(""),
    [seconds, setSeconds] = useState(120),
    [validation, setValidation] = useState<string>();
  const save = useEmergencyCommand<EmergencyReceivingRequestInput, EmergencyReceivingRequest>(
    baseID,
    () => `dispatches/${row.id}/receiving/${id.current}`,
    "PUT",
    "Hospital pre-alert recorded",
    () => {
      id.current = newEmergencyId();
      setReference("");
    },
    invalidates,
  );
  async function submit() {
    const parsed = emergencyReceivingRequestSchema.safeParse({
      version: row.version,
      link_id: link,
      ...(previous ? { previous_request_id: previous.id } : {}),
      response_seconds: seconds,
      evidence_reference: reference,
    });
    if (!parsed.success) {
      setValidation(
        "Select an approved partner and enter a communication reference and response window of 15–300 seconds.",
      );
      return;
    }
    setValidation(undefined);
    try {
      await save.mutateAsync(parsed.data);
    } catch {
      /* The retained request and error are rendered below. */
    }
  }
  if (save.data && !observed.some((request) => request.id === save.data?.id))
    return <p role="status">Pre-alert recorded. Refreshing the current hospital plan…</p>;
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
      className="space-y-3"
    >
      <label htmlFor={`${prefix}-partner`} className="block space-y-2">
        Approved receiving partner
        {/* eslint-disable-next-line no-restricted-syntax -- replaced in Task 6 (#1255) */}
        <select
          id={`${prefix}-partner`}
          className={selectClass}
          value={link}
          disabled={save.frozen || unavailable}
          onChange={(event) => setLink(event.target.value)}
        >
          <option value="">Choose a receiving campus</option>
          {links
            .filter((item) => item.status === "approved")
            .map((item) => (
              <option key={item.id} value={item.id}>
                {item.destination_name ?? item.facility_id}
              </option>
            ))}
        </select>
      </label>
      <label htmlFor={`${prefix}-reference`} className="block space-y-2">
        Pre-alert communication reference
        <Input
          id={`${prefix}-reference`}
          maxLength={120}
          value={reference}
          disabled={save.frozen || unavailable}
          onChange={(event) => setReference(event.target.value)}
        />
      </label>
      <label htmlFor={`${prefix}-seconds`} className="block space-y-2">
        Hospital response window (seconds)
        <Input
          id={`${prefix}-seconds`}
          type="number"
          min={15}
          max={300}
          value={seconds}
          disabled={save.frozen || unavailable}
          onChange={(event) => setSeconds(Number(event.target.value))}
        />
      </label>
      {previous && (
        <p className="text-sm text-muted-foreground">
          A replacement supersedes the previous request. Coordinate any diversion with the crew; a
          new request does not carry forward the old acceptance.
        </p>
      )}
      {validation && <p role="alert">{validation}</p>}
      <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
      <Button className="min-h-11" disabled={save.isPending || unavailable} type="submit">
        {save.uncertain
          ? "Retry unchanged pre-alert"
          : previous
            ? "Request alternate destination"
            : "Send hospital pre-alert"}
      </Button>
    </form>
  );
}
export function EmergencyReceivingPartners({ base }: { base: EmergencyBase }) {
  const { can } = usePermissions();
  const allowed = can("emergency.dispatch.manage") && !!base.access_level;
  const links = useApiPagedQuery<EmergencyReceivingLink>(
    ["emergency", "receiving", "provider-links", base.id],
    `/emergency/bases/${base.id}/receiving-links`,
    { enabled: allowed, poll: true, request: { cache: "no-store" } },
  );
  if (!allowed) return null;
  const supervisor = base.access_level === "supervisor";
  return (
    <section className="space-y-4 rounded-xl border bg-card p-5">
      <h3 className="text-lg font-semibold">Receiving service links</h3>
      {links.isError ? (
        <p role="alert">Service links unavailable. Refresh before changing them.</p>
      ) : (
        links.items.map((link) => (
          <div key={link.id} className="space-y-2 rounded-lg border p-4">
            <Badge>{EMERGENCY_RECEIVING_LINK_STATES[link.status]}</Badge>
            <p>{link.destination_name ?? "Receiving manager confirmation pending"}</p>
            <p className="break-all text-sm text-muted-foreground">
              Campus {link.facility_id} · expires {formatTime(link.valid_until)}
            </p>
            {supervisor && ["approved", "proposed"].includes(link.status) && (
              <ProviderLinkRevoke baseID={base.id} link={link} />
            )}
          </div>
        ))
      )}
      <LoadMore hasMore={links.hasMore} isLoading={links.isFetchingMore} onClick={links.loadMore} />
      {supervisor && <LinkProposal baseID={base.id} />}
    </section>
  );
}
function LinkProposal({ baseID }: { baseID: string }) {
  const prefix = useId(),
    id = useRef(newEmergencyId());
  const [tenant, setTenant] = useState(""),
    [facility, setFacility] = useState(""),
    [policy, setPolicy] = useState(""),
    [expires, setExpires] = useState(() =>
      new Date(Date.now() + 24 * 3600000).toISOString().slice(0, 16),
    ),
    [validation, setValidation] = useState<string>();
  const save = useEmergencyCommand<EmergencyReceivingLinkInput, EmergencyReceivingLink>(
    baseID,
    () => `receiving-links/${id.current}`,
    "PUT",
    "Service link proposed",
    () => {
      id.current = newEmergencyId();
      setFacility("");
      setPolicy("");
    },
    invalidates,
  );
  async function submit() {
    const date = Date.parse(`${expires}Z`);
    const parsed = emergencyReceivingLinkSchema.safeParse({
      destination_tenant_id: tenant,
      facility_id: facility,
      policy_reference: policy,
      valid_until: Number.isFinite(date) ? new Date(date).toISOString() : "",
    });
    if (!parsed.success) {
      setValidation(
        "Enter the receiving tenant and campus identifiers, approved provider policy reference and expiry.",
      );
      return;
    }
    setValidation(undefined);
    try {
      await save.mutateAsync(parsed.data);
    } catch {
      /* The request remains available for explicit retry. */
    }
  }
  return (
    <details className="rounded-lg border p-4">
      <summary className="cursor-pointer font-medium">Propose a receiving service link</summary>
      <form
        className="mt-4 grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <label htmlFor={`${prefix}-tenant`} className="space-y-2">
          Receiving tenant ID
          <Input
            id={`${prefix}-tenant`}
            value={tenant}
            disabled={save.frozen}
            onChange={(event) => setTenant(event.target.value)}
          />
        </label>
        <label htmlFor={`${prefix}-facility`} className="space-y-2">
          Receiving campus ID
          <Input
            id={`${prefix}-facility`}
            value={facility}
            disabled={save.frozen}
            onChange={(event) => setFacility(event.target.value)}
          />
        </label>
        <label htmlFor={`${prefix}-policy`} className="space-y-2">
          Provider agreement reference
          <Input
            id={`${prefix}-policy`}
            value={policy}
            maxLength={120}
            disabled={save.frozen}
            onChange={(event) => setPolicy(event.target.value)}
          />
        </label>
        <label htmlFor={`${prefix}-expires`} className="space-y-2">
          Agreement expiry (UTC)
          <Input
            id={`${prefix}-expires`}
            type="datetime-local"
            value={expires}
            disabled={save.frozen}
            onChange={(event) => setExpires(event.target.value)}
          />
        </label>
        {validation && <p role="alert">{validation}</p>}
        <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
        <Button className="min-h-11" disabled={save.isPending} type="submit">
          {save.uncertain ? "Retry unchanged proposal" : "Send service link proposal"}
        </Button>
      </form>
    </details>
  );
}
function ProviderLinkRevoke({ baseID, link }: { baseID: string; link: EmergencyReceivingLink }) {
  const prefix = useId(),
    id = useRef(newEmergencyId());
  const [reference, setReference] = useState(""),
    [validation, setValidation] = useState<string>();
  const save = useEmergencyCommand<EmergencyReceivingLinkCommandInput, EmergencyReceivingReceipt>(
    baseID,
    () => `receiving-links/${link.id}/commands/${id.current}`,
    "PUT",
    "Service link revoked",
    undefined,
    invalidates,
  );
  async function submit() {
    const parsed = emergencyReceivingLinkCommandSchema.safeParse({
      version: link.version,
      operation: "revoke",
      evidence_reference: reference,
    });
    if (!parsed.success) {
      setValidation("Enter a revocation reference without personal details.");
      return;
    }
    setValidation(undefined);
    try {
      await save.mutateAsync(parsed.data);
    } catch {
      /* Preserve the attributed revocation command for retry. */
    }
  }
  if (save.data && save.data.version > link.version)
    return <p role="status">Revocation recorded. Refreshing the link…</p>;
  return (
    <form
      className="space-y-3"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <label htmlFor={prefix} className="block space-y-2">
        Service link revocation reference
        <Input
          id={prefix}
          value={reference}
          disabled={save.frozen}
          onChange={(event) => setReference(event.target.value)}
        />
      </label>
      {validation && <p role="alert">{validation}</p>}
      <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
      <Button className="min-h-11" variant="outline" disabled={save.isPending} type="submit">
        {save.uncertain ? "Retry unchanged revocation" : "Revoke provider service link"}
      </Button>
    </form>
  );
}
