"use client";

import { useEffect, useId, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, FormSelectField, RefreshButton, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  StaffDepartmentBindingProposalInputContract,
  StaffDepartmentBindingCommandInputContract,
  newPharmacyStaffBindingId,
  preparePharmacyStaffBinding,
  decidePharmacyStaffBinding,
  recoverPharmacyStaffBindingReceipt,
  usePharmacyBindingDepartments,
  useApiMutation,
  type PharmacyStaffBindingScope,
  type PharmacyStaffBindingAttempt,
  type StaffDepartmentBinding,
  type StaffDepartmentBindingCommandInput,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

export type PharmacyBindingIntent =
  | { kind: "proposal"; replacement: StaffDepartmentBinding | null }
  | {
      kind: "decision";
      binding: StaffDepartmentBinding;
      operation: StaffDepartmentBindingCommandInput["operation"];
    };
type Props = {
  scope: PharmacyStaffBindingScope;
  intent: PharmacyBindingIntent;
  onBusy: (busy: boolean) => void;
  onCompleted: () => void;
  onContextChanged: () => void;
};
const schema = z.object({
  department: z.string(),
  reason: StaffDepartmentBindingProposalInputContract.shape.reason,
  attested: z.boolean(),
});
const labels = {
  approve: "Approve relationship",
  reject: "Reject relationship",
  withdraw: "Withdraw proposal",
  end: "End relationship",
};

export function PharmacyBindingCommand({
  scope,
  intent,
  onBusy,
  onCompleted,
  onContextChanged,
}: Props) {
  const prefix = useId();
  const proposal = intent.kind === "proposal";
  const approval = intent.kind === "decision" && intent.operation === "approve";
  const needsDepartment = proposal || approval;
  const [cursor, setCursor] = useState<string | null>(null);
  const [retained, setRetained] = useState<PharmacyStaffBindingAttempt | null>(null);
  const departments = usePharmacyBindingDepartments(scope, cursor, {
    busy: true,
    enabled: needsDepartment && retained === null,
  });
  const form = useZodForm(schema, { department: "", reason: "", attested: false });
  const original = useRef<PharmacyStaffBindingAttempt | null>(null);
  const active = useRef<AbortController | null>(null);
  const examined = useRef<string | null>(null);
  const currentDepartment =
    intent.kind === "decision"
      ? departments.data?.data.find(
          (row) =>
            row.id === intent.binding.department_id &&
            row.revision_id === intent.binding.department_revision_id,
        )
      : null;
  const observation =
    approval && currentDepartment
      ? JSON.stringify([
          intent.binding.id,
          intent.binding.version,
          intent.binding.replaces_binding_id,
          intent.binding.replaces_binding_version,
          currentDepartment.id,
          currentDepartment.revision_id,
        ])
      : null;
  const save = useApiMutation(
    async ({ attempt, recover }: { attempt: PharmacyStaffBindingAttempt; recover: boolean }) => {
      const controller = new AbortController();
      active.current = controller;
      if (recover) return recoverPharmacyStaffBindingReceipt(scope, attempt, controller.signal);
      return attempt.kind === "proposal"
        ? preparePharmacyStaffBinding(scope, attempt.bindingId, attempt.input, controller.signal)
        : decidePharmacyStaffBinding(scope, attempt.bindingId, attempt.input, controller.signal);
    },
    {
      suppressErrorToast: true,
      networkMode: "always",
      onSuccess: () => {
        original.current = null;
        form.reset();
      },
    },
  );
  useEffect(() => {
    onBusy(true);
    return () => {
      active.current?.abort();
      onBusy(false);
    };
  }, [onBusy]);
  const { confirm } = useLeaveGuard(
    !save.data && (form.formState.isDirty || retained !== null),
    "pharmacy relationship; an unconfirmed command may already be recorded",
    save.isPending,
  );
  const missing =
    save.variables?.recover &&
    save.error instanceof ApiError &&
    save.error.status === 404 &&
    save.error.code === "not_found";
  const cutoff = useStaffAccessCutoff(
    [needsDepartment ? departments.error : null, missing ? null : save.error],
    onContextChanged,
  );
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (save.data)
    return (
      <section className="space-y-4" aria-label="Retained relationship receipt">
        <p role="status">
          {save.data.operation === "propose"
            ? "Retained proposal receipt verified"
            : "Retained " + save.data.operation + " receipt verified"}
        </p>
        <p>
          Recorded {save.data.binding.status} · Version {save.data.binding.version}. This receipt
          grants no operating authority.
        </p>
        <p className="break-all">Command reference: {save.data.id}</p>
        {save.data.replaced_binding_id && (
          <p className="break-all">
            Previous relationship ended: {save.data.replaced_binding_id} · Version{" "}
            {save.data.replaced_binding_version}
          </p>
        )}
        <Button onClick={onCompleted}>Return to relationships</Button>
      </section>
    );
  const sourceAvailable =
    !needsDepartment ||
    (departments.isSuccess && !departments.isFetching && (!approval || !!currentDepartment));
  const invalidActor =
    intent.kind === "decision" &&
    (intent.operation === "withdraw"
      ? intent.binding.proposed_by_subject !== scope.subject
      : intent.binding.proposed_by_subject === scope.subject);
  const invalidState =
    intent.kind === "decision" &&
    intent.binding.status !== (intent.operation === "end" ? "approved" : "pending");
  const available = sourceAvailable && !invalidActor && !invalidState;
  return (
    <section className="space-y-4" aria-label="Pharmacy relationship command">
      <h4 className="font-semibold">
        {proposal ? "Prepare a department relationship" : labels[intent.operation]}
      </h4>
      <p>This records departmental responsibility. It does not grant permission to dispense.</p>
      {proposal && <p>A separate reviewer must approve this proposal.</p>}
      {proposal && intent.replacement && (
        <p className="break-all">
          Proposed replacement of {intent.replacement.id} · Version {intent.replacement.version}
        </p>
      )}
      {!proposal && (
        <p className="break-all">
          Examined relationship {intent.binding.id} · Version {intent.binding.version}
        </p>
      )}
      {!proposal && (
        <dl className="grid gap-3 sm:grid-cols-2">
          <div>
            <dt className="text-sm text-muted-foreground">Submitted reason</dt>
            <dd>{intent.binding.reason}</dd>
          </div>
          <div>
            <dt className="text-sm text-muted-foreground">Submitted department revision</dt>
            <dd className="break-all">{intent.binding.department_revision_id}</dd>
          </div>
          {intent.binding.decision_reason && (
            <div>
              <dt className="text-sm text-muted-foreground">Previous decision reason</dt>
              <dd>{intent.binding.decision_reason}</dd>
            </div>
          )}
        </dl>
      )}
      {approval && intent.binding.replaces_binding_id !== null && (
        <p className="break-all">
          Approval will end the previous relationship {intent.binding.replaces_binding_id} · Version{" "}
          {intent.binding.replaces_binding_version} in the same transaction.
        </p>
      )}
      {invalidActor && (
        <p role="alert">
          An independent reviewer is required. Only the original proposer may withdraw.
        </p>
      )}
      {invalidState && (
        <p role="alert">
          The recorded status does not permit this command. Reload the relationship.
        </p>
      )}
      {needsDepartment && (
        <div className="space-y-3">
          {approval && sourceAvailable && (
            <div>
              <p>Current department revision verified</p>
              <p>
                {currentDepartment?.code} · {currentDepartment?.name}
              </p>
              <p className="break-all">Examined revision: {currentDepartment?.revision_id}</p>
            </div>
          )}
          {!sourceAvailable && (
            <p role={departments.isError ? "alert" : "status"}>
              The exact current department revision could not yet be verified. Check further pages
              or recheck its source.
            </p>
          )}
          <div className="flex flex-wrap gap-3">
            <RefreshButton
              busy={save.isPending || retained !== null || departments.isFetching}
              error={departments.error}
              refresh={() => {
                examined.current = null;
                form.setValue("attested", false);
                return departments.refetch();
              }}
            >
              Recheck department source
            </RefreshButton>
            {cursor !== null && (
              <Button
                type="button"
                variant="outline"
                disabled={retained !== null || departments.isFetching}
                onClick={() => setCursor(null)}
              >
                First department page
              </Button>
            )}
            {departments.data?.page.next_cursor && (
              <Button
                type="button"
                variant="outline"
                disabled={retained !== null || departments.isFetching}
                onClick={() => setCursor(departments.data.page.next_cursor)}
              >
                Next department page
              </Button>
            )}
          </div>
        </div>
      )}
      <form
        noValidate
        className="space-y-4"
        onSubmit={(event) => {
          void form.handleSubmit((values) => {
            if (original.current || save.isPending || !available) return;
            let attempt: PharmacyStaffBindingAttempt;
            if (proposal) {
              const department = departments.data?.data.find((row) => row.id === values.department);
              if (!department) {
                form.setError("department", {
                  message: "Choose a department on the verified current page.",
                });
                return;
              }
              const input = StaffDepartmentBindingProposalInputContract.parse({
                department_id: department.id,
                department_revision_id: department.revision_id,
                replaces_binding_id: intent.replacement?.id ?? null,
                replaces_binding_version: intent.replacement?.version ?? null,
                reason: values.reason,
              });
              attempt = { kind: "proposal", bindingId: newPharmacyStaffBindingId(), input };
            } else {
              if (
                approval &&
                (!values.attested || observation === null || examined.current !== observation)
              ) {
                form.setError("attested", {
                  message: "Confirm the independent department responsibility review.",
                });
                return;
              }
              const input = StaffDepartmentBindingCommandInputContract.parse({
                id: newPharmacyStaffBindingId(),
                expected_version: intent.binding.version,
                operation: intent.operation,
                reason: values.reason,
                attest_department_responsibility: approval,
              });
              attempt = { kind: "decision", bindingId: intent.binding.id, input };
            }
            original.current = attempt;
            setRetained(attempt);
            save.mutate({ attempt, recover: false });
          })(event);
        }}
      >
        <fieldset
          disabled={!available || retained !== null || save.isPending}
          className="space-y-4"
        >
          {proposal && (
            <FormSelectField
              control={form.control}
              name="department"
              id={prefix + "-department"}
              label="Department"
              options={[
                { value: "", label: "Choose a department" },
                ...(departments.data?.data.map((row) => ({
                  value: row.id,
                  label: row.code + " · " + row.name,
                })) ?? []),
              ]}
            />
          )}
          <Field
            id={prefix + "-reason"}
            label={proposal ? "Proposal reason" : "Decision reason"}
            error={form.formState.errors.reason?.message}
          >
            <Input id={prefix + "-reason"} maxLength={500} {...form.register("reason")} />
          </Field>
          {approval && (
            <Field
              id={prefix + "-attested"}
              label={
                intent.kind === "decision" && intent.binding.replaces_binding_id !== null
                  ? "I independently verified this department's responsibility and the replacement of the previous relationship."
                  : "I independently verified this department is responsible for this outlet."
              }
              error={form.formState.errors.attested?.message}
            >
              <input
                id={prefix + "-attested"}
                type="checkbox"
                {...form.register("attested")}
                onChange={(event) => {
                  examined.current = event.target.checked ? observation : null;
                  void form.register("attested").onChange(event);
                }}
              />
            </Field>
          )}
        </fieldset>
        {!retained && (
          <Button type="submit" disabled={!available || save.isPending}>
            {proposal ? "Record relationship proposal" : labels[intent.operation]}
          </Button>
        )}
      </form>
      {retained && (
        <section className="space-y-3" aria-label="Retained command recovery">
          <p className="break-all">
            Retained command reference:{" "}
            {retained.kind === "proposal" ? retained.bindingId : retained.input.id}
          </p>
          {missing && (
            <p role="status">
              No retained receipt is visible yet. The original request may still be in flight.
            </p>
          )}
          {save.error && (
            <p role="alert">
              The outcome could not be confirmed. Recover its receipt or retry the exact command. A
              missing receipt may still belong to an in-flight request.
            </p>
          )}
          <div className="flex flex-wrap gap-3">
            <Button
              disabled={save.isPending}
              onClick={() => save.mutate({ attempt: retained, recover: true })}
            >
              Recover command receipt
            </Button>
            <Button
              variant="outline"
              disabled={save.isPending}
              onClick={() => save.mutate({ attempt: retained, recover: false })}
            >
              Retry exact command
            </Button>
          </div>
        </section>
      )}
      <Button variant="outline" disabled={save.isPending} onClick={() => confirm(onCompleted)}>
        Close command editor
      </Button>
    </section>
  );
}
