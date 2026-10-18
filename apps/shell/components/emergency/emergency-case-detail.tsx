"use client";

import { useState } from "react";
import Link from "next/link";
import { Badge, Button, Textarea } from "@tesserix/web";
import {
  apiFetch,
  emergencyChoices,
  emergencyClosureSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type EmergencyCase,
  type EmergencyCaseDetail,
  type EmergencyClosureInput,
  type EmergencyTimeline,
} from "@helivanta/api";
import {
  ConfirmDialog,
  Field,
  FormSelectField,
  LoadMore,
  formatTime,
  useZodForm,
} from "@helivanta/ui";

export function EmergencyCaseDetailPanel({ baseID, caseID }: { baseID: string; caseID: string }) {
  const path = `/emergency/bases/${encodeURIComponent(baseID)}/cases/${encodeURIComponent(caseID)}`;
  const detail = useApiQuery<EmergencyCaseDetail>(["emergency", "detail", baseID, caseID], path, {
    gcTime: 0,
    refetchOnWindowFocus: true,
    request: { cache: "no-store" },
  });
  if (detail.isError)
    return (
      <div role="alert" className="space-y-3">
        <p>{detail.error.message}</p>
        <Button
          onClick={() => {
            void detail.refetch();
          }}
        >
          Retry case
        </Button>
      </div>
    );
  if (!detail.data) return <p role="status">Loading protected case…</p>;
  const row = detail.data;
  return (
    <div className="mx-auto max-w-4xl space-y-5">
      <Link className="text-primary underline" href="/emergency">
        Back to operating bases
      </Link>
      <header className="space-y-2">
        <h1 className="break-all text-2xl font-semibold">{row.case_number}</h1>
        <div className="flex gap-2">
          <Badge variant="secondary">{row.status}</Badge>
          <Badge variant="outline">Untriaged</Badge>
        </div>
        <p className="text-muted-foreground">
          This record confirms staff intake only. It does not confirm an ambulance, crew or
          receiving hospital.
        </p>
      </header>
      <section className="space-y-3 rounded-xl border bg-card p-5">
        <h2 className="font-semibold">Protected report</h2>
        <dl className="grid gap-4 sm:grid-cols-2">
          <div>
            <dt className="text-sm text-muted-foreground">Caller</dt>
            <dd>
              {row.contact_expired
                ? "Caller contact expired under policy"
                : row.contact?.caller_name || "Unknown caller"}
            </dd>
          </div>
          <div>
            <dt className="text-sm text-muted-foreground">Callback phone</dt>
            <dd>{row.contact?.callback_phone || "Not available"}</dd>
          </div>
          <div>
            <dt className="text-sm text-muted-foreground">Reported location</dt>
            <dd className="whitespace-pre-wrap break-words">
              {row.incident.location_text || "Not supplied"}
            </dd>
          </div>
          <div>
            <dt className="text-sm text-muted-foreground">Reported situation</dt>
            <dd className="whitespace-pre-wrap break-words">
              {row.incident.patient_description || "Not supplied"}
            </dd>
          </div>
          <div>
            <dt className="text-sm text-muted-foreground">Processing policy</dt>
            <dd>
              {row.policy_reference} · {row.processing_basis}
            </dd>
          </div>
          <div>
            <dt className="text-sm text-muted-foreground">Requested service</dt>
            <dd>
              {emergencyChoices.services[
                row.requested_service as keyof typeof emergencyChoices.services
              ] ?? row.requested_service}
            </dd>
          </div>
        </dl>
        {row.closure_explanation && (
          <p className="whitespace-pre-wrap">Closure review: {row.closure_explanation}</p>
        )}
      </section>
      {row.status === "received" && <EmergencyClose key={row.version} row={row} path={path} />}
      <EmergencyHistory baseID={baseID} caseID={caseID} path={path} />
    </div>
  );
}
function EmergencyClose({ row, path }: { row: EmergencyCase; path: string }) {
  const { can } = usePermissions();
  const access = useApiQuery<{ level: string }>(
    ["emergency", "access", row.base_id],
    `/emergency/bases/${row.base_id}/access`,
    { enabled: can("emergency.case.close"), gcTime: 0 },
  );
  const form = useZodForm(emergencyClosureSchema, {
    version: row.version,
    reason: "entered_in_error",
    explanation: "",
  });
  const [pending, setPending] = useState<EmergencyClosureInput | null>(null);
  const close = useApiMutation(
    (values: EmergencyClosureInput) =>
      apiFetch<EmergencyCase>(`${path}/close`, { method: "POST", body: JSON.stringify(values) }),
    {
      successToast: "Report closed",
      invalidate: [
        ["emergency", "detail", row.base_id, row.id],
        ["emergency", "cases", row.base_id],
        ["emergency", "timeline", row.base_id, row.id],
      ],
      onSuccess: () => setPending(null),
    },
  );
  if (!can("emergency.case.close") || access.isError || access.data?.level !== "supervisor")
    return null;
  return (
    <section className="space-y-4 rounded-xl border bg-card p-5">
      <h2 className="font-semibold">Supervisor review and closure</h2>
      <p className="text-sm text-muted-foreground">
        Only close an unassigned intake after review. This cannot cancel or stand down a crew.
      </p>
      <form
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => setPending(values))}
      >
        <FormSelectField
          control={form.control}
          name="reason"
          id="close-reason"
          label="Closure reason"
          options={Object.entries(emergencyChoices.closure_reasons).map(([value, label]) => ({
            value,
            label,
          }))}
        />
        <Field
          id="close-explanation"
          label="Review explanation"
          error={form.formState.errors.explanation?.message}
        >
          <Textarea id="close-explanation" autoComplete="off" {...form.register("explanation")} />
        </Field>
        {close.isError && (
          <p role="alert">
            {close.error.message}. A version conflict requires a fresh case review.
          </p>
        )}
        <Button type="submit" variant="destructive" disabled={close.isPending}>
          Close report
        </Button>
      </form>
      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) setPending(null);
        }}
        title="Close this unassigned report?"
        description="The case and review history are retained. This action does not arrange alternative care or cancel any ambulance."
        confirmLabel="Confirm close"
        busy={close.isPending}
        onConfirm={() => {
          if (pending) close.mutate(pending);
        }}
      />
    </section>
  );
}
function EmergencyHistory({
  baseID,
  caseID,
  path,
}: {
  baseID: string;
  caseID: string;
  path: string;
}) {
  const history = useApiPagedQuery<EmergencyTimeline>(
    ["emergency", "timeline", baseID, caseID],
    `${path}/timeline`,
  );
  return (
    <section className="space-y-3 rounded-xl border bg-card p-5">
      <h2 className="font-semibold">Case history</h2>
      {history.isPending && <p role="status">Loading case history…</p>}
      {history.isError ? (
        <div role="alert">
          {history.error?.message}
          <Button onClick={history.retry}>Retry history</Button>
        </div>
      ) : (
        <>
          <ol className="space-y-3">
            {history.items.map((event) => (
              <li key={event.id} className="text-sm">
                <span className="font-medium">{event.state}</span> · {formatTime(event.occurred_at)}{" "}
                · version {event.version}
                {event.reason && ` · ${event.reason}`}
              </li>
            ))}
          </ol>
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
