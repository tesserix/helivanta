"use client";
import { useEffect, useId, useRef, useState } from "react";
import { Button, Badge, Input } from "@tesserix/web";
import {
  usePermissions,
  useApiPagedQuery,
  useApiQuery,
  newEmergencyId,
  EMERGENCY_RECEIVING_STATES,
  EMERGENCY_RECEIVING_LINK_STATES,
  emergencyReceivingFacilitySchema,
  emergencyReceivingAccessSchema,
  emergencyReceivingCommandSchema,
  emergencyReceivingLinkCommandSchema,
  type EmergencyReceivingFacility,
  type EmergencyReceivingFacilityInput,
  type EmergencyReceivingAccessInput,
  type EmergencyReceivingRequest,
  type EmergencyReceivingLink,
  type EmergencyReceivingCommandInput,
  type EmergencyReceivingLinkCommandInput,
  type EmergencyReceivingReceipt,
} from "@helivanta/api";
import { formatTime, LoadMore, PageHeading, SelectControl } from "@helivanta/ui";
import { EmergencyCommandFailure, useEmergencyRequest } from "./emergency-command";
const root = "/emergency/receiving/facilities";
const invalidates = [
  ["emergency", "receiving"],
  ["emergency", "crew"],
  ["emergency", "dispatches"],
];
const expiryDefault = () => new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString().slice(0, 16);
function timestamp(value: string) {
  const time = Date.parse(`${value}Z`);
  return Number.isFinite(time) ? new Date(time).toISOString() : "";
}

export function EmergencyReceivingWorkspace() {
  const { can } = usePermissions();
  const allowed = can("emergency.receiving.read"),
    manage = can("emergency.receiving.manage");
  const facilities = useApiPagedQuery<EmergencyReceivingFacility>(
    ["emergency", "receiving", "facilities"],
    root,
    { enabled: allowed, poll: true, request: { cache: "no-store" } },
  );
  const memberships = useApiQuery<{ data: Array<{ tenant_id: string; current: boolean }> }>(
    ["iam", "me", "tenants"],
    "/iam/me/tenants",
    { enabled: allowed },
  );
  const tenant = memberships.data?.data?.find?.((item) => item.current)?.tenant_id;
  const [selected, select] = useState<string | null>(null);
  const facility = facilities.items.find((item) => item.id === selected);
  if (!allowed) return <p>Receiving-facility access is required.</p>;
  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <PageHeading description="Review pre-alerts, confirm receiving capacity and record physical handover through your authorized campus." />
      {facilities.isPending && <p role="status">Checking campus access…</p>}
      {facilities.isError ? (
        <div role="alert">
          Campus access is unavailable.<Button onClick={facilities.retry}>Refresh campuses</Button>
        </div>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {facilities.items.map((item) => (
            <li key={item.id} className="space-y-3 rounded-xl border bg-card p-5">
              <h2 className="font-semibold">{item.name}</h2>
              <Badge>{item.enabled ? "Configured" : "Disabled"}</Badge>
              <p className="text-sm text-muted-foreground">
                {item.access_level ?? "Configuration access"} · policy expires{" "}
                {formatTime(item.expires_at)}
              </p>
              <Button className="min-h-11" onClick={() => select(item.id)}>
                Open {item.name}
              </Button>
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        hasMore={facilities.hasMore}
        isLoading={facilities.isFetchingMore}
        onClick={facilities.loadMore}
      />
      {manage && <FacilityEditor key="new-facility" />}
      {facility && !facilities.isError && (
        <section className="space-y-5">
          <h2 className="text-xl font-semibold">{facility.name}</h2>
          <div className="space-y-1 rounded-xl border p-4 text-sm">
            <p>Share these identifiers with the provider supervisor to propose a service link.</p>
            {tenant && <p className="break-all">Receiving tenant: {tenant}</p>}
            <p className="break-all">Campus: {facility.id}</p>
          </div>
          {manage && (
            <>
              <FacilityEditor key={facility.id} facility={facility} />
              <ReceivingGrant facility={facility} />
            </>
          )}
          {facility.access_level && (
            <ReceivingInbox facility={facility} canAct={can("emergency.receiving.act")} />
          )}
        </section>
      )}
    </div>
  );
}
function FacilityEditor({ facility }: { facility?: EmergencyReceivingFacility }) {
  const prefix = useId(),
    id = useRef(facility?.id ?? newEmergencyId());
  const [name, setName] = useState(facility?.name ?? ""),
    [policy, setPolicy] = useState(facility?.policy_reference ?? ""),
    [expires, setExpires] = useState(facility?.expires_at?.slice(0, 16) ?? expiryDefault()),
    [enabled, setEnabled] = useState(facility?.enabled ?? true),
    [validation, setValidation] = useState<string>();
  const save = useEmergencyRequest<EmergencyReceivingFacilityInput, EmergencyReceivingFacility>(
    () => `${root}/${id.current}`,
    "PUT",
    "Receiving campus saved",
    undefined,
    invalidates,
  );
  async function submit() {
    const parsed = emergencyReceivingFacilitySchema.safeParse({
      version: facility?.version ?? 0,
      name,
      enabled,
      policy_reference: policy,
      expires_at: timestamp(expires),
    });
    if (!parsed.success) {
      setValidation("Enter a campus name, approved policy reference and expiry.");
      return;
    }
    setValidation(undefined);
    save.mutate(parsed.data);
  }
  return (
    <details className="rounded-xl border bg-card p-5">
      <summary className="cursor-pointer font-semibold">
        {facility ? "Configure this campus" : "Add receiving campus"}
      </summary>
      <form
        className="mt-4 grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <label htmlFor={`${prefix}-name`} className="space-y-2">
          Campus name
          <Input
            id={`${prefix}-name`}
            disabled={save.frozen}
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={150}
          />
        </label>
        <label htmlFor={`${prefix}-policy`} className="space-y-2">
          Approved receiving policy reference
          <Input
            id={`${prefix}-policy`}
            disabled={save.frozen}
            value={policy}
            onChange={(event) => setPolicy(event.target.value)}
            maxLength={120}
          />
        </label>
        <label htmlFor={`${prefix}-expires`} className="space-y-2">
          Policy expiry (UTC)
          <Input
            id={`${prefix}-expires`}
            type="datetime-local"
            disabled={save.frozen}
            value={expires}
            onChange={(event) => setExpires(event.target.value)}
          />
        </label>
        <label htmlFor={`${prefix}-enabled`} className="flex items-center gap-2">
          <input
            id={`${prefix}-enabled`}
            type="checkbox"
            checked={enabled}
            disabled={save.frozen}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Campus enabled
        </label>
        {validation && <p role="alert">{validation}</p>}
        <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
        <Button className="min-h-11" type="submit" disabled={save.isPending}>
          {save.uncertain ? "Retry unchanged campus revision" : "Save campus revision"}
        </Button>
      </form>
    </details>
  );
}
function ReceivingGrant({ facility }: { facility: EmergencyReceivingFacility }) {
  const prefix = useId();
  const [subject, setSubject] = useState(""),
    [selected, select] = useState(""),
    [level, setLevel] = useState<"receiver" | "manager">("receiver"),
    [enabled, setEnabled] = useState(true),
    [policy, setPolicy] = useState(""),
    [expires, setExpires] = useState(expiryDefault),
    [validation, setValidation] = useState<string>();
  const current = useApiQuery<EmergencyReceivingAccessInput>(
    ["emergency", "receiving", "access", facility.id, selected],
    `${root}/${facility.id}/access/${encodeURIComponent(selected)}`,
    { enabled: !!selected, gcTime: 0, request: { cache: "no-store" } },
  );
  const save = useEmergencyRequest<EmergencyReceivingAccessInput>(
    () => `${root}/${facility.id}/access/${encodeURIComponent(selected)}`,
    "PUT",
    "Receiving delegation recorded",
    undefined,
    invalidates,
  );
  const missing = current.isError && "status" in current.error && current.error.status === 404;
  async function submit() {
    if (!selected || current.isPending || (current.isError && !missing)) {
      setValidation("Load the verified member's current delegation before changing it.");
      return;
    }
    const parsed = emergencyReceivingAccessSchema.safeParse({
      version: missing ? 0 : current.data?.version,
      level,
      enabled,
      policy_reference: policy,
      expires_at: timestamp(expires),
    });
    if (!parsed.success) {
      setValidation("Choose an authority level, approved verification reference and expiry.");
      return;
    }
    setValidation(undefined);
    save.mutate(parsed.data);
  }
  return (
    <details className="rounded-xl border bg-card p-5">
      <summary className="cursor-pointer font-semibold">Delegate receiving authority</summary>
      <p className="mt-3 text-sm text-muted-foreground">
        Assign the Emergency hospital receiver role to a verified member first. Campus verification
        must be independent; you cannot enable your own delegation.
      </p>
      <div className="my-4 flex flex-wrap items-end gap-3">
        <label htmlFor={`${prefix}-subject`} className="min-w-0 flex-1 space-y-2">
          Verified member subject ID
          <Input
            id={`${prefix}-subject`}
            value={subject}
            disabled={save.frozen}
            onChange={(event) => setSubject(event.target.value)}
            maxLength={200}
          />
        </label>
        <Button
          type="button"
          variant="outline"
          disabled={save.frozen || !subject.trim()}
          onClick={() => select(subject.trim())}
        >
          Load current delegation
        </Button>
      </div>
      {selected && (
        <form
          className="grid gap-4 sm:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          {current.isPending ? (
            <p role="status">Checking current authority…</p>
          ) : current.isError && !missing ? (
            <p role="alert">Authority could not be checked.</p>
          ) : (
            <p>Current revision: {missing ? 0 : current.data?.version}</p>
          )}
          <label htmlFor={`${prefix}-level`} className="space-y-2">
            Campus authority
            <SelectControl
              id={`${prefix}-level`}
              aria-label="Campus authority"
              className="min-h-11 w-full"
              value={level}
              disabled={save.frozen}
              onValueChange={(next) => setLevel(next === "manager" ? "manager" : "receiver")}
              options={[
                { value: "receiver", label: "Receiving clinician" },
                { value: "manager", label: "Receiving manager" },
              ]}
            />
          </label>
          <label htmlFor={`${prefix}-policy`} className="space-y-2">
            Verification reference
            <Input
              id={`${prefix}-policy`}
              value={policy}
              disabled={save.frozen}
              onChange={(event) => setPolicy(event.target.value)}
            />
          </label>
          <label htmlFor={`${prefix}-expires`} className="space-y-2">
            Delegation expiry (UTC)
            <Input
              id={`${prefix}-expires`}
              type="datetime-local"
              value={expires}
              disabled={save.frozen}
              onChange={(event) => setExpires(event.target.value)}
            />
          </label>
          <label htmlFor={`${prefix}-enabled`} className="flex items-center gap-2">
            <input
              id={`${prefix}-enabled`}
              type="checkbox"
              checked={enabled}
              disabled={save.frozen}
              onChange={(event) => setEnabled(event.target.checked)}
            />
            Authority enabled
          </label>
          {validation && <p role="alert">{validation}</p>}
          <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
          <Button className="min-h-11" disabled={save.isPending} type="submit">
            {save.uncertain ? "Retry unchanged delegation" : "Record delegation revision"}
          </Button>
        </form>
      )}
    </details>
  );
}
function ReceivingInbox({
  facility,
  canAct,
}: {
  facility: EmergencyReceivingFacility;
  canAct: boolean;
}) {
  const links = useApiPagedQuery<EmergencyReceivingLink>(
    ["emergency", "receiving", "links", facility.id],
    `${root}/${facility.id}/links`,
    { poll: true, request: { cache: "no-store" } },
  );
  const requests = useApiPagedQuery<EmergencyReceivingRequest>(
    ["emergency", "receiving", "requests", facility.id],
    `${root}/${facility.id}/requests`,
    { poll: true, request: { cache: "no-store" } },
  );
  const [selected, select] = useState<EmergencyReceivingRequest | null>(null);
  const path = selected
    ? `${root}/${facility.id}/requests/${selected.provider_tenant_id}/${selected.id}`
    : "";
  const detail = useApiQuery<EmergencyReceivingRequest>(
    ["emergency", "receiving", "request", facility.id, selected?.id],
    path,
    {
      enabled: !!selected,
      poll: true,
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { cache: "no-store" },
    },
  );
  return (
    <>
      <section className="space-y-3">
        <h3 className="text-lg font-semibold">Provider service links</h3>
        {links.isError ? (
          <p role="alert">Service links unavailable.</p>
        ) : (
          links.items.map((link) => (
            <div className="space-y-3 rounded-xl border bg-card p-4" key={link.id}>
              <Badge>{EMERGENCY_RECEIVING_LINK_STATES[link.status]}</Badge>
              <p className="break-all text-sm">
                Provider {link.provider_tenant_id} · base {link.base_id}
              </p>
              <p className="text-sm">Provider policy: {link.policy_reference}</p>
              {canAct &&
                facility.access_level === "manager" &&
                ["proposed", "approved"].includes(link.status) && (
                  <LinkDecision link={link} facility={facility} />
                )}
            </div>
          ))
        )}
        <LoadMore
          hasMore={links.hasMore}
          isLoading={links.isFetchingMore}
          onClick={links.loadMore}
        />
      </section>
      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h3 className="text-lg font-semibold">Hospital pre-alerts</h3>
          <Button variant="outline" onClick={requests.retry}>
            Refresh pre-alerts
          </Button>
        </div>
        {requests.isPending && <p role="status">Checking directed pre-alerts…</p>}
        {requests.isError ? (
          <p role="alert">Pre-alerts unavailable. Refresh before acting.</p>
        ) : (
          requests.items.map((request) => (
            <div
              key={request.id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-card p-4"
            >
              <div>
                <Badge>{EMERGENCY_RECEIVING_STATES[request.status]}</Badge>
                <p className="mt-2 text-sm">
                  {request.required_class.toUpperCase()} ·{" "}
                  {request.superseded ? "Superseded" : "Latest destination request"}
                </p>
              </div>
              <Button className="min-h-11" onClick={() => select(request)}>
                Open pre-alert
              </Button>
            </div>
          ))
        )}
        <LoadMore
          hasMore={requests.hasMore}
          isLoading={requests.isFetchingMore}
          onClick={requests.loadMore}
        />
      </section>
      {selected && (
        <section className="space-y-4 rounded-xl border bg-card p-5">
          <div className="flex flex-wrap gap-3">
            <Button variant="outline" onClick={() => select(null)}>
              Close pre-alert
            </Button>
            <Button variant="outline" onClick={() => void detail.refetch()}>
              Refresh current pre-alert
            </Button>
          </div>
          {detail.isError ? (
            <p role="alert">Current receiving access is unavailable.</p>
          ) : !detail.data ? (
            <p role="status">Checking current pre-alert…</p>
          ) : (
            <>
              <h3 className="text-xl font-semibold">
                {EMERGENCY_RECEIVING_STATES[detail.data.status]}
              </h3>
              <p>Required transport class: {detail.data.required_class.toUpperCase()}</p>
              <p className="text-sm">
                Response deadline: {formatTime(detail.data.response_deadline)}
              </p>
              {detail.data.acceptance_valid_until && (
                <p>Acceptance expires {formatTime(detail.data.acceptance_valid_until)}</p>
              )}
              <p className="text-muted-foreground">
                Complete the agreed clinical pre-alert with dispatch before accepting. Acceptance
                confirms current receiving capacity; physical handover is recorded separately.
              </p>
            </>
          )}
          {canAct && (
            <ReceivingDecision
              key={selected.id}
              request={detail.data}
              path={path}
              visible={!!detail.data && !detail.isError}
            />
          )}
        </section>
      )}
    </>
  );
}
function ReceivingDecision({
  request,
  path,
  visible,
}: {
  request?: EmergencyReceivingRequest;
  path: string;
  visible: boolean;
}) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, []);
  const prefix = useId(),
    command = useRef<string | null>(null);
  const [reference, setReference] = useState(""),
    [minutes, setMinutes] = useState(30),
    [validation, setValidation] = useState<string>();
  const save = useEmergencyRequest<EmergencyReceivingCommandInput, EmergencyReceivingReceipt>(
    () => `${path}/commands/${(command.current ??= newEmergencyId())}`,
    "PUT",
    "Hospital decision recorded",
    () => {
      command.current = null;
      setReference("");
    },
    invalidates,
  );
  if (!visible || !request) return null;
  if (save.data && save.data.version > request.version)
    return (
      <p role="status">Decision recorded. Refresh the current pre-alert before another action.</p>
    );
  const pending =
    request.status === "pending" &&
    !request.superseded &&
    Date.parse(request.response_deadline) > now;
  const accepted = request.status === "accepted" && !request.superseded;
  if (!pending && !accepted && !save.uncertain)
    return <p>No receiving action is available for this request.</p>;
  async function submit(operation: EmergencyReceivingCommandInput["operation"]) {
    if (!request) return;
    if (
      operation === "accept" &&
      !save.uncertain &&
      (!Number.isInteger(minutes) || minutes < 1 || minutes > 120)
    ) {
      setValidation("Choose an acceptance window of 1–120 minutes.");
      return;
    }
    const body = save.uncertain
      ? save.variables
      : {
          version: request.version,
          operation,
          evidence_reference: reference,
          ...(operation === "accept"
            ? { valid_until: new Date(Date.now() + minutes * 60000).toISOString() }
            : {}),
        };
    const parsed = emergencyReceivingCommandSchema.safeParse(body);
    if (!parsed.success || !Number.isInteger(minutes) || minutes < 1 || minutes > 120) {
      setValidation(
        "Enter a communication reference without personal details and an acceptance window of 1–120 minutes.",
      );
      return;
    }
    setValidation(undefined);
    save.mutate(parsed.data);
  }
  return (
    <div className="space-y-3">
      <label htmlFor={`${prefix}-reference`} className="block space-y-2">
        Clinical communication reference
        <Input
          id={`${prefix}-reference`}
          value={reference}
          maxLength={120}
          disabled={save.frozen}
          onChange={(event) => setReference(event.target.value)}
        />
      </label>
      {pending && (
        <label htmlFor={`${prefix}-minutes`} className="block space-y-2">
          Acceptance window (minutes)
          <Input
            id={`${prefix}-minutes`}
            type="number"
            min={1}
            max={120}
            value={minutes}
            disabled={save.frozen}
            onChange={(event) => setMinutes(Number(event.target.value))}
          />
        </label>
      )}
      {validation && <p role="alert">{validation}</p>}
      <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
      <div className="flex flex-col gap-3 sm:flex-row">
        {save.uncertain ? (
          <Button
            className="min-h-11"
            disabled={save.isPending}
            onClick={() => void submit("accept")}
          >
            Retry unchanged decision
          </Button>
        ) : (
          <>
            {pending && (
              <>
                <Button
                  className="min-h-11"
                  disabled={save.isPending}
                  onClick={() => void submit("accept")}
                >
                  Confirm hospital acceptance
                </Button>
                <Button
                  variant="outline"
                  className="min-h-11"
                  disabled={save.isPending}
                  onClick={() => void submit("reject")}
                >
                  Decline pre-alert
                </Button>
              </>
            )}
            {accepted && (
              <>
                <Button
                  className="min-h-11"
                  disabled={save.isPending || !request.arrival_recorded}
                  onClick={() => void submit("handover_received")}
                >
                  Confirm physical receipt
                </Button>
                <Button
                  variant="outline"
                  className="min-h-11"
                  disabled={save.isPending}
                  onClick={() => void submit("revoke")}
                >
                  Withdraw acceptance
                </Button>
              </>
            )}
          </>
        )}
      </div>
      {accepted && !request.arrival_recorded && (
        <p className="text-sm text-muted-foreground">
          Waiting for the crew's hospital arrival record before physical receipt can be confirmed.
        </p>
      )}
    </div>
  );
}
function LinkDecision({
  link,
  facility,
}: {
  link: EmergencyReceivingLink;
  facility: EmergencyReceivingFacility;
}) {
  const prefix = useId(),
    command = useRef<string | null>(null);
  const [reference, setReference] = useState(""),
    [validation, setValidation] = useState<string>();
  const save = useEmergencyRequest<EmergencyReceivingLinkCommandInput, EmergencyReceivingReceipt>(
    () =>
      `${root}/${facility.id}/links/${link.provider_tenant_id}/${link.id}/commands/${(command.current ??= newEmergencyId())}`,
    "PUT",
    "Service link decision recorded",
    () => {
      command.current = null;
      setReference("");
    },
    invalidates,
  );
  async function submit(operation: EmergencyReceivingLinkCommandInput["operation"]) {
    const parsed = emergencyReceivingLinkCommandSchema.safeParse(
      save.uncertain
        ? save.variables
        : { version: link.version, operation, evidence_reference: reference },
    );
    if (!parsed.success) {
      setValidation("Use an approved agreement reference without personal details.");
      return;
    }
    setValidation(undefined);
    save.mutate(parsed.data);
  }
  return (
    <div className="space-y-3">
      <label htmlFor={prefix} className="block space-y-2">
        Agreement verification reference
        <Input
          id={prefix}
          disabled={save.frozen}
          value={reference}
          onChange={(event) => setReference(event.target.value)}
        />
      </label>
      {validation && <p role="alert">{validation}</p>}
      <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
      <div className="flex flex-wrap gap-3">
        {save.uncertain ? (
          <Button disabled={save.isPending} onClick={() => void submit("approve")}>
            Retry unchanged link decision
          </Button>
        ) : link.status === "proposed" ? (
          <>
            <Button disabled={save.isPending} onClick={() => void submit("approve")}>
              Approve service link
            </Button>
            <Button
              variant="outline"
              disabled={save.isPending}
              onClick={() => void submit("reject")}
            >
              Decline service link
            </Button>
          </>
        ) : (
          <Button variant="outline" disabled={save.isPending} onClick={() => void submit("revoke")}>
            Revoke service link
          </Button>
        )}
      </div>
    </div>
  );
}
