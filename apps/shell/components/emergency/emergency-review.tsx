"use client";

import { useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  emergencyClinicalSubjectSchema,
  emergencyClinicalGrantSchema,
  emergencyTransportReviewSchema,
  emergencyFleetChoices,
  emergencyLocalTime,
  emergencyTimeInput,
  newEmergencyId,
  useApiQuery,
  useApiPagedQuery,
  usePermissions,
  type EmergencyClinicalGrant,
  type EmergencyClinicalGrantInput,
  type EmergencyTransportReview,
  type EmergencyTransportReviewInput,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, formatTime, useZodForm } from "@helivanta/ui";
import { EmergencyCommandFailure, useEmergencyCommand } from "./emergency-command";

export function EmergencyClinicalAdmin({ base }: { base: string }) {
  const { can } = usePermissions();
  const form = useZodForm(emergencyClinicalSubjectSchema, { subject: "" });
  const [target, setTarget] = useState<{ subject: string; generation: number } | null>(null);
  if (!can("emergency.clinical.manage")) return null;
  return (
    <section className="min-w-0 space-y-4 border-t pt-4">
      <h3 className="font-semibold">Clinical review delegation</h3>
      <p className="text-sm text-muted-foreground">
        An independent verifier must approve access. Delegation does not validate a professional
        licence or grant access to cases. The reviewer also needs the relevant role and base grant.
      </p>
      <form
        noValidate
        className="space-y-3"
        onSubmit={form.handleSubmit(({ subject }) =>
          setTarget((previous) => ({ subject, generation: (previous?.generation ?? 0) + 1 })),
        )}
      >
        <Field
          id="clinical-subject"
          label="Clinical reviewer identity subject"
          error={form.formState.errors.subject?.message}
        >
          <Input id="clinical-subject" autoComplete="off" {...form.register("subject")} />
        </Field>
        <Button type="submit" variant="outline">
          Load clinical review access
        </Button>
      </form>
      {target && (
        <ClinicalAccessLoader
          key={`${target.subject}-${target.generation}`}
          base={base}
          subject={target.subject}
        />
      )}
    </section>
  );
}

function ClinicalAccessLoader({ base, subject }: { base: string; subject: string }) {
  const current = useApiQuery<EmergencyClinicalGrant>(
    ["emergency", "clinical", base, subject],
    `/emergency/bases/${base}/clinical-reviewers/${encodeURIComponent(subject)}`,
    { gcTime: 0, request: { cache: "no-store" }, refetchOnWindowFocus: true },
  );
  if (current.isPending) return <p role="status">Loading clinical review access…</p>;
  if (current.isError && !(current.error instanceof ApiError && current.error.status === 404))
    return <p role="alert">{current.error.message}. Reload access before making a change.</p>;
  return (
    <ClinicalAccessEditor
      key={current.data?.version ?? 0}
      base={base}
      subject={subject}
      current={current.isError ? undefined : current.data}
    />
  );
}

function ClinicalAccessEditor({
  base,
  subject,
  current,
}: {
  base: string;
  subject: string;
  current?: EmergencyClinicalGrant;
}) {
  const { subject: actor } = usePermissions();
  const form = useZodForm(
    emergencyClinicalGrantSchema,
    current
      ? { ...current, expires_at: emergencyTimeInput(current.expires_at) }
      : { version: 0, enabled: false, evidence_reference: "", expires_at: "" },
  );
  const save = useEmergencyCommand<EmergencyClinicalGrantInput, EmergencyClinicalGrant>(
    base,
    () => `clinical-reviewers/${encodeURIComponent(subject)}`,
    "PUT",
    "Clinical review access saved",
    undefined,
    [["emergency", "clinical", base]],
  );
  return (
    <form
      noValidate
      className="space-y-4 rounded-lg border p-4"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <p className="break-all text-sm">
        Reviewing {subject} · version {current?.version ?? 0}
      </p>
      {current && (
        <p className="text-sm text-muted-foreground">
          Verified by {current.verified_by} ·{" "}
          {current.currently_active ? "Active" : "Inactive or expired"}
        </p>
      )}
      <fieldset disabled={save.frozen} className="space-y-4">
        <Field
          id="clinical-evidence"
          label="Clinical privilege approval reference"
          error={form.formState.errors.evidence_reference?.message}
        >
          <Input id="clinical-evidence" {...form.register("evidence_reference")} />
        </Field>
        <Field
          id="clinical-expiry"
          label="Clinical review access expires at"
          error={form.formState.errors.expires_at?.message}
        >
          <Input
            id="clinical-expiry"
            type="datetime-local"
            {...form.register("expires_at", { setValueAs: emergencyLocalTime })}
          />
        </Field>
        <label className="flex min-h-11 items-center gap-3">
          <input type="checkbox" disabled={actor === subject} {...form.register("enabled")} />
          Clinical review access enabled
        </label>
      </fieldset>
      {actor === subject && <p role="status">You cannot enable or renew your own review access.</p>}
      <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
      <Button
        type="submit"
        disabled={save.isPending || (actor === subject && form.watch("enabled"))}
      >
        Save clinical review access
      </Button>
      {current?.enabled && save.data?.enabled !== false && (
        <Button
          type="button"
          variant="destructive"
          disabled={save.isPending}
          onClick={() =>
            save.mutate({
              version: current.version,
              enabled: false,
              evidence_reference: current.evidence_reference,
              expires_at: current.expires_at,
            })
          }
        >
          Revoke clinical review access
        </Button>
      )}
    </form>
  );
}

type ReviewCase = { id: string; base_id: string; version: number; status: string };

export function EmergencyTransportReviewPanel({ row }: { row: ReviewCase }) {
  const { can } = usePermissions();
  const own = useApiQuery<EmergencyClinicalGrant>(
    ["emergency", "clinical", row.base_id, "self"],
    `/emergency/bases/${row.base_id}/clinical-reviewer`,
    {
      enabled: can("emergency.case.assess") && row.status === "received",
      gcTime: 0,
      request: { cache: "no-store" },
      refetchOnWindowFocus: true,
    },
  );
  const history = useApiPagedQuery<EmergencyTransportReview>(
    ["emergency", "assessments", row.base_id, row.id],
    `/emergency/bases/${row.base_id}/cases/${row.id}/assessments`,
    { enabled: can("emergency.case.read"), request: { cache: "no-store" } },
  );
  if (!can("emergency.case.read")) return null;
  return (
    <section className="min-w-0 space-y-4 rounded-xl border bg-card p-5">
      <h2 className="font-semibold">Reviewed transport requirement</h2>
      <p className="text-sm text-muted-foreground">
        An attributed transport review records the required class under an approved policy. Review
        history is not a current eligibility decision or a reservation.
      </p>
      <Button
        variant="outline"
        onClick={() => {
          history.retry();
          if (can("emergency.case.assess") && row.status === "received") void own.refetch();
        }}
      >
        Refresh transport reviews
      </Button>
      {row.status === "received" &&
        can("emergency.case.assess") &&
        (own.isPending ? (
          <p role="status">Checking clinical review access…</p>
        ) : own.isError || !own.data?.currently_active ? (
          <p role="status">
            Active independently verified review access is required before recording a transport
            requirement. Refresh access after verification.
          </p>
        ) : (
          <TransportReviewForm key={`${row.version}-${own.data.version}`} row={row} />
        ))}
      {history.isPending && <p role="status">Loading transport review history…</p>}
      {history.isError ? (
        <p role="alert">{history.error?.message}</p>
      ) : (
        <ol className="space-y-3">
          {history.items.map((review) => (
            <li key={review.id} className="break-words text-sm">
              Review {review.sequence} ·{" "}
              {
                emergencyFleetChoices.classes[
                  review.required_class as keyof typeof emergencyFleetChoices.classes
                ]
              }{" "}
              · case version {review.case_version} · policy {review.policy_reference} · valid until{" "}
              {formatTime(review.valid_until)} · recorded {formatTime(review.created_at)}
            </li>
          ))}
        </ol>
      )}
      <LoadMore
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </section>
  );
}

function TransportReviewForm({ row }: { row: ReviewCase }) {
  const id = useRef(newEmergencyId());
  const form = useZodForm(emergencyTransportReviewSchema, {
    case_version: row.version,
    required_class: "",
    policy_reference: "",
    valid_until: "",
  });
  const save = useEmergencyCommand<EmergencyTransportReviewInput, EmergencyTransportReview>(
    row.base_id,
    () => `cases/${row.id}/assessments/${id.current}`,
    "PUT",
    "Transport requirement recorded",
    () => {
      id.current = newEmergencyId();
      form.reset();
    },
    [["emergency", "assessments", row.base_id, row.id]],
  );
  return (
    <form
      noValidate
      className="space-y-4"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <fieldset disabled={save.frozen} className="space-y-4">
        <FormSelectField
          control={form.control}
          name="required_class"
          id="transport-class"
          label="Reviewed transport class"
          options={Object.entries(emergencyFleetChoices.classes).map(([value, label]) => ({
            value,
            label,
          }))}
        />
        <Field
          id="transport-policy"
          label="Reviewed transport policy"
          error={form.formState.errors.policy_reference?.message}
        >
          <Input id="transport-policy" {...form.register("policy_reference")} />
        </Field>
        <Field
          id="transport-validity"
          label="Transport review valid until"
          error={form.formState.errors.valid_until?.message}
        >
          <Input
            id="transport-validity"
            type="datetime-local"
            {...form.register("valid_until", { setValueAs: emergencyLocalTime })}
          />
        </Field>
      </fieldset>
      <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
      <Button type="submit" disabled={save.isPending}>
        Record transport requirement
      </Button>
      {save.data && (
        <p role="status">
          Transport requirement recorded. No vehicle has been reserved or dispatched.
        </p>
      )}
    </form>
  );
}
