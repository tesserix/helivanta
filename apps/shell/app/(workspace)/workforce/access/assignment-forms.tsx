"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, FormSelectField, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  useApiMutation,
  useApiQuery,
  newStaffAssignmentId,
  proposeStaffAssignment,
  decideStaffAssignment,
  staffAssignmentsKey,
  staffAssignmentInputContract,
  staffAssignmentDecisionContract,
  type StaffAssignment,
  type StaffAssignmentInput,
  type StaffAssignmentDecision,
  type StaffAssignmentScope,
  type Hospital,
  type Page,
  type StaffAssignmentFamily,
  staffAccessTemplates,
} from "@helivanta/api";

function failure(error: Error) {
  if (error instanceof ApiError && error.status === 409)
    return "The assignment or session changed. Close this form and refresh before trying again.";
  if (error instanceof ApiError && [400, 401, 403, 404].includes(error.status))
    return error.message;
  return "The request may have been recorded. Retry the same request or check history before starting another.";
}
function localTime(date: Date) {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
const dateTime = z
  .string()
  .min(1, "Choose a date and time.")
  .refine((v) => Number.isFinite(Date.parse(v)), "Choose a valid date and time.");
const proposalForm = z
  .object({
    hospital_id: staffAssignmentInputContract.shape.hospital_id,
    template_key: staffAssignmentInputContract.shape.template_key,
    subject: z
      .string()
      .trim()
      .min(1, "Enter an account ID.")
      .pipe(staffAssignmentInputContract.shape.subject),
    reason: z
      .string()
      .trim()
      .min(1, "Enter a reason.")
      .pipe(staffAssignmentInputContract.shape.reason),
    valid_from: dateTime,
    expires_at: dateTime,
  })
  .superRefine((value, ctx) => {
    const duration = Date.parse(value.expires_at) - Date.parse(value.valid_from);
    if (duration <= 0 || duration > 366 * 86400000)
      ctx.addIssue({
        code: "custom",
        path: ["expires_at"],
        message: "Choose an end after the start, within 366 days.",
      });
    if (Date.parse(value.expires_at) <= Date.now())
      ctx.addIssue({ code: "custom", path: ["expires_at"], message: "Choose a future expiry." });
  });

type FormProps = { scope: StaffAssignmentScope; onClose: () => void; onSaved: () => void };
export function AssignmentProposalForm({
  scope,
  onClose,
  onSaved,
  family = "leave",
  renewal,
}: FormProps & { family?: StaffAssignmentFamily; renewal?: StaffAssignment }) {
  const [id] = useState(() => newStaffAssignmentId());
  const [submitted, setSubmitted] = useState<StaffAssignmentInput | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const hospitals = useApiQuery<Page<Hospital>>(
    [...staffAssignmentsKey(scope), "hospitals", cursor],
    `/workforce/hospitals${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
    {
      enabled: !renewal,
      gcTime: 0,
      request: { cache: "no-store", headers: { "X-Helivanta-Expected-Tenant": scope.tenantId } },
    },
  );
  const renewalPrefix = renewal ? `Renewal of ${renewal.id} v${renewal.version}: ` : "";
  const [defaults] = useState(() => ({
    hospital_id: renewal?.input.hospital_id ?? "",
    template_key:
      renewal?.input.template_key ??
      staffAccessTemplates.find((template) => template.family === family)!.key,
    subject: renewal?.input.subject ?? "",
    reason: "",
    valid_from: renewal ? "" : localTime(new Date()),
    expires_at: renewal ? "" : localTime(new Date(Date.now() + 86400000)),
  }));
  const form = useZodForm(
    proposalForm.superRefine((value, ctx) => {
      if (renewal && value.reason.length + renewalPrefix.length > 500)
        ctx.addIssue({
          code: "custom",
          path: ["reason"],
          message: `Use at most ${500 - renewalPrefix.length} characters for the renewal reason.`,
        });
    }),
    defaults,
  );
  const save = useApiMutation(
    (input: StaffAssignmentInput) => proposeStaffAssignment(scope, id, input),
    {
      invalidate: [staffAssignmentsKey(scope)],
      onSuccess: onSaved,
      suppressErrorToast: true,
    },
  );
  const managers = hospitals.data?.data.filter((h) => h.access_level === "manager") ?? [];
  return (
    <form
      aria-label={renewal ? "Renew assignment" : "Propose assignment"}
      noValidate
      className="space-y-4 border-t border-border pt-4"
      onSubmit={form.handleSubmit((values) => {
        const command = submitted ?? {
          ...values,
          reason: renewalPrefix + values.reason,

          valid_from: new Date(values.valid_from).toISOString(),
          expires_at: new Date(values.expires_at).toISOString(),
        };
        setSubmitted(command);
        save.mutate(command);
      })}
    >
      <h2 className="text-lg font-semibold">
        {renewal ? "Renew staff access" : "Propose staff access"}
      </h2>
      {renewal && (
        <p>
          This creates a new proposal requiring hospital verification and independent review. It
          does not extend or reactivate the previous assignment. Choose new dates and give a fresh
          reason. Previous assignment: {renewal.id}, version {renewal.version}.
        </p>
      )}
      <FormSelectField
        control={form.control}
        name="template_key"
        id="assignment-template"
        label="Access template"
        disabled={!!submitted || !!renewal}
        options={staffAccessTemplates
          .filter((template) => template.family === family)
          .map((template) => ({ value: template.key, label: template.name }))}
      />
      {renewal ? (
        <p className="break-all">Hospital: {renewal.input.hospital_id}</p>
      ) : hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <p role="alert">
          Could not load hospitals.{" "}
          <Button type="button" onClick={() => void hospitals.refetch()}>
            Retry hospitals
          </Button>
        </p>
      ) : (
        <>
          {!managers.length && (
            <p>
              No hospital manager access on this page. Ask an existing hospital access administrator
              to review your access.
            </p>
          )}
          <FormSelectField
            control={form.control}
            name="hospital_id"
            id="assignment-hospital"
            label="Hospital"
            disabled={!!submitted}
            options={[
              { value: "", label: "Choose a hospital" },
              ...managers.map((h) => ({ value: h.id, label: h.name })),
            ]}
          />
          <div className="flex flex-wrap gap-2">
            {hospitals.data?.page.has_more && (
              <Button
                type="button"
                variant="outline"
                disabled={!!submitted}
                onClick={() => setCursor(hospitals.data?.page.next_cursor ?? null)}
              >
                More hospitals
              </Button>
            )}
            {cursor && (
              <Button
                type="button"
                variant="outline"
                disabled={!!submitted}
                onClick={() => setCursor(null)}
              >
                First hospitals
              </Button>
            )}
          </div>
        </>
      )}
      <Field
        id="assignment-subject"
        label="Account ID"
        error={form.formState.errors.subject?.message}
      >
        <Input
          id="assignment-subject"
          aria-invalid={!!form.formState.errors.subject}
          aria-describedby={form.formState.errors.subject ? "assignment-subject-error" : undefined}
          autoComplete="off"
          readOnly={!!renewal}
          disabled={!!submitted}
          {...form.register("subject")}
        />
      </Field>
      <p>
        The account must already be a member of this tenant. A staff record or account link does not
        establish membership.
      </p>
      <Field
        id="assignment-start"
        label="Starts at"
        error={form.formState.errors.valid_from?.message}
      >
        <Input
          id="assignment-start"
          aria-invalid={!!form.formState.errors.valid_from}
          aria-describedby={form.formState.errors.valid_from ? "assignment-start-error" : undefined}
          type="datetime-local"
          disabled={!!submitted}
          {...form.register("valid_from")}
        />
      </Field>
      <Field
        id="assignment-expiry"
        label="Expires at"
        error={form.formState.errors.expires_at?.message}
      >
        <Input
          id="assignment-expiry"
          aria-invalid={!!form.formState.errors.expires_at}
          aria-describedby={
            form.formState.errors.expires_at ? "assignment-expiry-error" : undefined
          }
          type="datetime-local"
          disabled={!!submitted}
          {...form.register("expires_at")}
        />
      </Field>
      <p>Times use your device timezone. Assignments must expire within 366 days of their start.</p>
      <Field id="assignment-reason" label="Reason" error={form.formState.errors.reason?.message}>
        <Input
          id="assignment-reason"
          aria-invalid={!!form.formState.errors.reason}
          aria-describedby={form.formState.errors.reason ? "assignment-reason-error" : undefined}
          disabled={!!submitted}
          {...form.register("reason")}
        />
      </Field>
      {save.isError && <p role="alert">{failure(save.error)}</p>}
      <div className="flex flex-wrap gap-2">
        <Button
          type="submit"
          disabled={save.isPending || (!renewal && (hospitals.isPending || hospitals.isError))}
        >
          {save.isPending
            ? "Saving…"
            : save.isError
              ? "Retry same request"
              : renewal
                ? "Propose renewal"
                : "Submit proposal"}
        </Button>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Close form
        </Button>
      </div>
    </form>
  );
}

const reasonForm = z.object({
  reason: z
    .string()
    .trim()
    .min(1, "Enter a decision reason.")
    .pipe(staffAssignmentDecisionContract.shape.reason),
});
export function AssignmentDecisionForm({
  scope,
  item,
  decision,
  onClose,
  onSaved,
}: FormProps & { item: StaffAssignment; decision: StaffAssignmentDecision["decision"] }) {
  const [submitted, setSubmitted] = useState<StaffAssignmentDecision | null>(null);
  const form = useZodForm(reasonForm, { reason: "" });
  const save = useApiMutation(
    (input: StaffAssignmentDecision) => decideStaffAssignment(scope, item.id, input),
    {
      invalidate: [staffAssignmentsKey(scope)],
      onSuccess: onSaved,
      suppressErrorToast: true,
    },
  );
  return (
    <form
      aria-label={`${decision} assignment`}
      noValidate
      className="space-y-4"
      onSubmit={form.handleSubmit((value) => {
        const command = submitted ?? { ...value, expected_version: item.version, decision };
        setSubmitted(command);
        save.mutate(command);
      })}
    >
      <p>
        Confirm {decision} for version {item.version}. Approval requires a fresh hospital check and
        completed activation.
      </p>
      <Field
        id={`decision-reason-${item.id}`}
        label="Decision reason"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id={`decision-reason-${item.id}`}
          aria-invalid={!!form.formState.errors.reason}
          aria-describedby={
            form.formState.errors.reason ? `decision-reason-${item.id}-error` : undefined
          }
          disabled={!!submitted}
          {...form.register("reason")}
        />
      </Field>
      {save.isError && <p role="alert">{failure(save.error)}</p>}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : save.isError ? "Retry same request" : `Confirm ${decision}`}
        </Button>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Close form
        </Button>
      </div>
    </form>
  );
}
