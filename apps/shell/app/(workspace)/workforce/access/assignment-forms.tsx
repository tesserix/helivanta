"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  FormSelectField,
  useZodForm,
  type UseFormReturn,
  RequestFailure,
} from "@helivanta/ui";
import { useNow } from "@/lib/use-now";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "../staff-access-cutoff";
import {
  ApiError,
  useApiMutation,
  useApiQuery,
  useOrganizationDepartments,
  newStaffAssignmentId,
  proposeStaffAssignment,
  decideStaffAssignment,
  decideGovernanceAssignment,
  isGovernanceAssignmentAccessChanged,
  governanceAssignmentsKey,
  type GovernanceAssignmentScope,
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
    applies_to: z.enum(["hospital", "department"]),
    department_id: z.string(),
    valid_from: dateTime,
    expires_at: dateTime,
  })
  .superRefine((value, ctx) => {
    if (value.applies_to === "department" && !value.department_id)
      ctx.addIssue({ code: "custom", path: ["department_id"], message: "Choose a department." });
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
type AppliesTo = StaffAssignment["scope_kind"];
export function AssignmentProposalForm({
  scope,
  onClose,
  onSaved,
  family = "leave",
  templateKeys,
  renewal,
}: FormProps & {
  family?: StaffAssignmentFamily;
  templateKeys: readonly string[];
  renewal?: StaffAssignment;
}) {
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
  // Only templates the verified catalog confirms for the chosen scope are offered.
  const offered = (kind: AppliesTo) =>
    staffAccessTemplates.filter(
      (template) =>
        template.family === family &&
        templateKeys.includes(template.key) &&
        (template.scope_kinds as readonly AppliesTo[]).includes(kind),
    );
  const departmentCapable = offered("department").length > 0;
  const [defaults] = useState(() => ({
    hospital_id: renewal?.input.hospital_id ?? "",
    template_key:
      renewal?.input.template_key ??
      staffAccessTemplates.find((template) => template.family === family)!.key,
    applies_to: renewal?.scope_kind ?? ("hospital" as AppliesTo),
    department_id: renewal?.input.department_id ?? "",
    subject: renewal?.input.subject ?? "",
    reason: "",
    valid_from: renewal ? "" : localTime(new Date()),
    expires_at: renewal ? "" : localTime(new Date(Date.now() + 86400000)),
  }));
  const form = useZodForm(
    proposalForm.superRefine((value, ctx) => {
      const template = staffAccessTemplates.find((row) => row.key === value.template_key);
      if (template && !(template.scope_kinds as readonly AppliesTo[]).includes(value.applies_to))
        ctx.addIssue({
          code: "custom",
          path: ["template_key"],
          message: "Choose a template that allows this scope.",
        });
      if (!templateKeys.includes(value.template_key))
        ctx.addIssue({
          code: "custom",
          path: ["template_key"],
          message: "This access template is not confirmed by the current server catalog.",
        });
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
  const templateSupported = templateKeys.includes(
    submitted?.template_key ?? form.watch("template_key"),
  );
  const managers = hospitals.data?.data.filter((h) => h.access_level === "manager") ?? [];
  const appliesTo = form.watch("applies_to");
  const hospitalId = form.watch("hospital_id");
  return (
    <form
      aria-label={renewal ? "Renew assignment" : "Propose assignment"}
      noValidate
      className="space-y-4 border-t border-border pt-4"
      onSubmit={form.handleSubmit((values) => {
        const command = submitted ?? {
          hospital_id: values.hospital_id,
          ...(values.applies_to === "department" ? { department_id: values.department_id } : {}),
          template_key: values.template_key,
          subject: values.subject,
          reason: renewalPrefix + values.reason,
          valid_from: new Date(values.valid_from).toISOString(),
          expires_at: new Date(values.expires_at).toISOString(),
        };
        if (!templateKeys.includes(command.template_key)) {
          form.setError("template_key", {
            message: "This access template is not confirmed by the current server catalog.",
          });
          return;
        }
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
      {departmentCapable && !renewal && (
        <FormSelectField
          control={form.control}
          name="applies_to"
          id="assignment-applies-to"
          label="Applies to"
          disabled={!!submitted}
          options={[
            { value: "hospital", label: "Whole hospital" },
            { value: "department", label: "One department and its sub-units" },
          ]}
          onValueChange={(value) => {
            const choices = offered(value as AppliesTo);
            if (!choices.some((template) => template.key === form.getValues("template_key")))
              form.setValue("template_key", choices[0]?.key ?? "");
            form.setValue("department_id", "");
          }}
        />
      )}
      <FormSelectField
        control={form.control}
        name="template_key"
        id="assignment-template"
        label="Access template"
        disabled={!!submitted || !!renewal}
        options={(renewal
          ? staffAccessTemplates.filter((template) => template.key === renewal.input.template_key)
          : offered(appliesTo)
        ).map((template) => ({ value: template.key, label: template.name }))}
      />
      {renewal ? (
        <>
          <p className="break-all">Hospital: {renewal.input.hospital_id}</p>
          {renewal.input.department_id && (
            <p className="break-all">
              Applies to: department {renewal.input.department_id} and its current sub-units
            </p>
          )}
        </>
      ) : hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <RequestFailure
          message="Could not load hospitals."
          retry={hospitals.refetch}
          error={hospitals.error}
        />
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
            onValueChange={() => form.setValue("department_id", "")}
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
          {appliesTo === "department" &&
            (hospitalId ? (
              <AssignmentDepartmentField
                key={hospitalId}
                scope={scope}
                hospital={hospitalId}
                control={form.control}
                disabled={!!submitted}
              />
            ) : (
              <p>Choose a hospital before choosing its department.</p>
            ))}
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
      {!templateSupported && (
        <p role="alert">
          This access template is unavailable. Refresh its verified catalog before submitting or
          retrying.
        </p>
      )}
      {save.isError && <p role="alert">{failure(save.error)}</p>}
      <div className="flex flex-wrap gap-2">
        <Button
          type="submit"
          disabled={
            !templateSupported ||
            save.isPending ||
            (!renewal && (hospitals.isPending || hospitals.isError))
          }
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

function AssignmentDepartmentField({
  scope,
  hospital,
  control,
  disabled,
}: {
  scope: StaffAssignmentScope;
  hospital: string;
  control: UseFormReturn<
    z.input<typeof proposalForm>,
    unknown,
    z.output<typeof proposalForm>
  >["control"];
  disabled: boolean;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const departments = useOrganizationDepartments(scope, hospital, cursor);
  const now = useNow();
  if (departments.isPending) return <p role="status">Loading departments…</p>;
  if (departments.error instanceof ApiError && departments.error.status === 403)
    return (
      <p role="alert">
        You cannot read this hospital&apos;s departments. Ask a tenant administrator for hospital
        structure access.
      </p>
    );
  if (departments.isError)
    return (
      <RequestFailure
        message="Could not load departments."
        retry={departments.refetch}
        error={departments.error}
      />
    );
  // Organization verifies the department again before review; retired or scheduled nodes are never offered.
  const active = departments.data.data.filter(
    (row) => row.status === "active" && Date.parse(row.effective_at) <= now,
  );
  return (
    <>
      <FormSelectField
        control={control}
        name="department_id"
        id="assignment-department"
        label="Department"
        disabled={disabled}
        options={[
          { value: "", label: "Choose a department" },
          ...active.map((row) => ({ value: row.id, label: row.name })),
        ]}
      />
      <p>The assignment also covers the department&apos;s current sub-units.</p>
      <div className="flex flex-wrap gap-2">
        {departments.data.page.has_more && (
          <Button
            type="button"
            variant="outline"
            disabled={disabled}
            onClick={() => setCursor(departments.data.page.next_cursor)}
          >
            More departments
          </Button>
        )}
        {cursor && (
          <Button
            type="button"
            variant="outline"
            disabled={disabled}
            onClick={() => setCursor(null)}
          >
            First departments
          </Button>
        )}
      </div>
    </>
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
  nativeScope,
  onAccessLost,
}: FormProps & {
  item: StaffAssignment;
  decision: StaffAssignmentDecision["decision"];
  nativeScope?: GovernanceAssignmentScope;
  onAccessLost?: () => void;
}) {
  const [submitted, setSubmitted] = useState<StaffAssignmentDecision | null>(null);
  const form = useZodForm(reasonForm, { reason: "" });
  const save = useApiMutation(
    (input: StaffAssignmentDecision) =>
      nativeScope
        ? decideGovernanceAssignment(nativeScope, item.id, input)
        : decideStaffAssignment(scope, item.id, input),
    {
      invalidate: [
        nativeScope ? governanceAssignmentsKey(nativeScope) : staffAssignmentsKey(scope),
      ],
      onSuccess: onSaved,
      suppressErrorToast: true,
    },
  );
  const cutoff = useStaffAccessCutoff(
    nativeScope ? [save.error] : [],
    onAccessLost,
    isGovernanceAssignmentAccessChanged,
  );
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
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
