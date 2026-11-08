"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  Panel,
  FormSelectField,
  RefreshButton,
  RequestFailure,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  ApiError,
  StaffPostingProposalInputContract,
  StaffPostingSupervisorChoiceContract,
  useStaffPostingSupervisorChoices,
  useStaffPostingTargets,
  useStaffPostingDrafts,
  useStaffPostingReviewSource,
  useStaffPostingDepartmentChoices,
  submitStaffPostingProposal,
  getStaffPostingProposalReceipt,
  newStaffPostingId,
  useApiMutation,
  onTenantTransition,
  tenantTransitionPending,
  type StaffPostingInboxScope,
  type StaffPostingProposalScope,
  type StaffPostingDraft,
  type StaffPostingTarget,
  type StaffPostingProposalInput,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

type Props = {
  scope: StaffPostingInboxScope;
  busy: boolean;
  admissionAvailable: boolean;
  onAdmissionRecheck: () => void;
  onBusy: (value: boolean) => void;
  onClose: () => void;
  onCompleted: () => void;
  onContextChanged: () => void;
};

export function PostingSubmissionWorkspace(props: Props) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<StaffPostingTarget | null>(null);
  const targets = useStaffPostingTargets(props.scope, cursor, props.busy);
  const cutoff = useStaffAccessCutoff([targets.error], props.onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <Panel className="space-y-5" aria-label="Prepare independent posting review">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-semibold">Submit a prepared posting</h2>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Choose a Staff member and an exact prepared draft for independent review. A proposal
            does not grant operating access.
          </p>
        </div>
        <Button variant="ghost" disabled={props.busy} onClick={props.onClose}>
          Back to proposals
        </Button>
      </div>
      {targets.isPending ? (
        <p role="status">Loading posting Staff choices…</p>
      ) : targets.isError ? (
        <RequestFailure
          message="Staff choices could not be verified."
          retry={targets.refetch}
          error={targets.error}
          busy={props.busy}
        />
      ) : (
        <>
          <div className="flex flex-wrap gap-3">
            {targets.data.data.map((row) => (
              <Button
                key={row.id}
                variant={selected?.id === row.id ? "default" : "outline"}
                disabled={props.busy || targets.isFetching}
                aria-label={`Choose ${row.display_name}`}
                onClick={() => setSelected(row)}
              >
                {row.display_name} · {row.employee_code}
              </Button>
            ))}
          </div>
          {targets.data.data.length === 0 && <p>No Staff choices on this page.</p>}
          <div className="flex flex-wrap gap-2">
            {targets.data.page.has_more && (
              <Button
                disabled={props.busy}
                onClick={() => {
                  setSelected(null);
                  setCursor(targets.data.page.next_cursor);
                }}
              >
                More submission Staff choices
              </Button>
            )}
            {cursor && (
              <Button
                disabled={props.busy}
                onClick={() => {
                  setSelected(null);
                  setCursor(null);
                }}
              >
                First submission Staff choices
              </Button>
            )}
          </div>
        </>
      )}
      {selected && (
        <SubmissionDrafts
          key={selected.id}
          {...props}
          scope={{ ...props.scope, staffId: selected.id }}
          staff={selected}
          contextAvailable={
            props.admissionAvailable && targets.isSuccess && (!targets.isFetching || props.busy)
          }
        />
      )}
    </Panel>
  );
}

function SubmissionDrafts({
  scope,
  staff,
  contextAvailable,
  ...props
}: Omit<Props, "scope"> & {
  scope: StaffPostingProposalScope;
  staff: StaffPostingTarget;
  contextAvailable: boolean;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<StaffPostingDraft | null>(null);
  const drafts = useStaffPostingDrafts(scope, cursor, props.busy);
  const cutoff = useStaffAccessCutoff([drafts.error], props.onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  const current =
    selected &&
    drafts.data?.data.find(
      (row) => row.id === selected.id && row.revision_id === selected.revision_id,
    );
  return (
    <section className="space-y-4" aria-label="Choose a prepared draft">
      <h3 className="font-medium">{staff.display_name} · Prepared drafts</h3>
      {drafts.isPending ? (
        <p role="status">Loading prepared posting drafts…</p>
      ) : drafts.isError ? (
        <RequestFailure
          message="Prepared drafts could not be verified."
          retry={drafts.refetch}
          error={drafts.error}
          busy={props.busy}
        />
      ) : (
        <>
          {drafts.data.data.map((row) => (
            <div
              key={row.revision_id}
              className="flex flex-wrap items-center justify-between gap-3 border-b border-border py-3 text-sm"
            >
              <div>
                <p className="font-medium">Draft version {row.version}</p>
                <p className="break-words text-muted-foreground">
                  {row.starts_at} {row.ends_at ? `to ${row.ends_at}` : "· ongoing"}
                </p>
              </div>
              <Button
                variant="outline"
                disabled={props.busy || drafts.isFetching || !contextAvailable}
                onClick={() => setSelected(row)}
              >
                Submit draft version {row.version}
              </Button>
            </div>
          ))}
          {drafts.data.data.length === 0 && <p>No prepared posting drafts on this page.</p>}
          <div className="flex flex-wrap gap-2">
            {drafts.data.page.has_more && (
              <Button
                disabled={props.busy}
                onClick={() => {
                  setSelected(null);
                  setCursor(drafts.data.page.next_cursor);
                }}
              >
                More prepared drafts
              </Button>
            )}
            {cursor && (
              <Button
                disabled={props.busy}
                onClick={() => {
                  setSelected(null);
                  setCursor(null);
                }}
              >
                First prepared drafts
              </Button>
            )}
          </div>
        </>
      )}
      {selected && (
        <PostingSubmission
          key={`${selected.id}:${selected.revision_id}`}
          {...props}
          scope={scope}
          draft={selected}
          contextAvailable={
            contextAvailable && drafts.isSuccess && (!drafts.isFetching || props.busy) && !!current
          }
          onContextRecheck={() => {
            void drafts.refetch();
            props.onAdmissionRecheck();
          }}
        />
      )}
    </section>
  );
}

const formSchema = z.object({
  reason: StaffPostingProposalInputContract.shape.reason,
  sources_examined: StaffPostingProposalInputContract.shape.sources_examined,
  responsibility: z.enum([
    "unspecified",
    "hospital_management",
    "department_supervisor",
    "department_member",
  ]),
  supervisor: StaffPostingSupervisorChoiceContract.nullable(),
});
type Command = { proposal: string; command: string; input: StaffPostingProposalInput };

function PostingSubmission({
  scope,
  draft,
  contextAvailable,
  onContextRecheck,
  onBusy,
  onCompleted,
  onContextChanged,
}: {
  scope: StaffPostingProposalScope;
  draft: StaffPostingDraft;
  contextAvailable: boolean;
  onContextRecheck: () => void;
  onBusy: Props["onBusy"];
  onCompleted: Props["onCompleted"];
  onContextChanged: Props["onContextChanged"];
}) {
  const [retained, setRetained] = useState<Command | null>(null);
  const original = useRef<Command | null>(null);
  const active = useRef<AbortController | null>(null);
  const form = useZodForm(formSchema, {
    reason: "",
    sources_examined: false,
    responsibility: "unspecified",
    supervisor: null,
  });
  const dirty = form.formState.isDirty || retained !== null;
  const responsibility = form.watch("responsibility");
  const needsSupervisor =
    responsibility === "department_supervisor" || responsibility === "department_member";
  const selectedSupervisor = form.watch("supervisor");
  const [supervisorCursor, setSupervisorCursor] = useState<string | null>(null);
  const supervisors = useStaffPostingSupervisorChoices(scope, "submission", supervisorCursor, {
    enabled: needsSupervisor,
    busy: dirty,
  });
  const currentSupervisor = supervisors.data?.data.find(
    (row) =>
      row.id === selectedSupervisor?.id &&
      row.revision_id === selectedSupervisor?.revision_id &&
      row.staff.id === selectedSupervisor?.staff.id &&
      row.staff.id !== scope.staffId,
  );
  const supervisorAvailable =
    !needsSupervisor ||
    (supervisors.isSuccess && !supervisors.isFetching && currentSupervisor !== undefined);

  const source = useStaffPostingReviewSource(scope, dirty);
  const [departmentCursor, setDepartmentCursor] = useState<string | null>(null);
  const departments = useStaffPostingDepartmentChoices(scope, departmentCursor);
  const save = useApiMutation(
    async ({ command, recover }: { command: Command; recover: boolean }) => {
      const controller = new AbortController();
      active.current = controller;
      const row = await (recover ? getStaffPostingProposalReceipt : submitStaffPostingProposal)(
        scope,
        command.proposal,
        command.command,
        command.input,
        draft,
        controller.signal,
      );
      controller.signal.throwIfAborted();
      return row;
    },
    {
      suppressErrorToast: true,
      networkMode: "always",
      onError: (error) => {
        if (error instanceof ApiError && error.status === 404 && error.code === "not_found") {
          onContextRecheck();
          void source.refetch();
          void departments.refetch();
        }
      },
      onSuccess: () => {
        original.current = null;
        setRetained(null);
        form.reset();
        onBusy(false);
        onCompleted();
      },
    },
  );
  useLeaveGuard(
    dirty,
    "posting submission; an unconfirmed proposal may already be recorded",
    save.isPending,
  );
  useEffect(() => {
    onBusy(dirty || save.isPending);
  }, [dirty, save.isPending, onBusy]);
  useEffect(() => {
    const stop = onTenantTransition(() => {
      if (tenantTransitionPending()) active.current?.abort();
    });
    return () => {
      stop();
      active.current?.abort();
      onBusy(false);
    };
  }, [onBusy]);
  const missingReceipt =
    save.variables?.recover &&
    save.error instanceof ApiError &&
    save.error.status === 404 &&
    save.error.code === "not_found";
  const cutoff = useStaffAccessCutoff(
    [source.error, departments.error, supervisors.error, missingReceipt ? null : save.error],
    onContextChanged,
  );
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  const matches =
    source.isSuccess &&
    !source.isFetching &&
    ["current", "not_started"].includes(source.data.generation_state) &&
    source.data.generation_id !== null &&
    source.data.staff_version === draft.staff_version &&
    source.data.employment_epoch === draft.employment_epoch;
  const department = departments.data?.data.find(
    (row) =>
      row.id === draft.department_id &&
      row.revision_id === draft.department_revision_id &&
      row.status === "active",
  );
  const available =
    contextAvailable && matches && departments.isSuccess && !departments.isFetching && !!department;
  const editingDisabled = !available || save.isPending || retained !== null;
  return (
    <section
      className="space-y-4 border-t border-border pt-4"
      aria-label="Selected posting submission"
    >
      <h4 className="font-medium">Selected draft version {draft.version}</h4>
      {departments.isPending || departments.isFetching ? (
        <p role="status">Checking the current department…</p>
      ) : departments.isError ? (
        <p role="alert">The current department could not be verified.</p>
      ) : department ? (
        <p>
          Current department: {department.code} · {department.name}
        </p>
      ) : (
        <p role="alert">
          The exact active department revision is not on this page. Check further pages, or correct
          the draft if the department changed.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {!department && departments.data?.page.has_more && (
          <Button
            disabled={dirty || save.isPending}
            onClick={() => setDepartmentCursor(departments.data!.page.next_cursor)}
          >
            More submission departments
          </Button>
        )}
        {!department && departmentCursor && (
          <Button disabled={dirty || save.isPending} onClick={() => setDepartmentCursor(null)}>
            First submission departments
          </Button>
        )}
        <RefreshButton
          refresh={departments.refetch}
          error={departments.error}
          busy={retained !== null || save.isPending || departments.isFetching}
        >
          Recheck submission department
        </RefreshButton>
      </div>
      <p className="text-sm">
        {draft.starts_at} {draft.ends_at ? `to ${draft.ends_at}` : "· ongoing"}
      </p>
      {source.isPending || source.isFetching ? (
        <p role="status">Checking current employment source…</p>
      ) : source.isError ? (
        <RequestFailure
          message="The current employment source could not be verified."
          retry={source.refetch}
          error={source.error}
          busy={retained !== null}
        />
      ) : (
        <p>
          {matches
            ? "The reviewed employment source matches this draft."
            : "The employment source changed or is not eligible. Reopen the current prepared draft."}{" "}
          Source state: {source.data.generation_state.replaceAll("_", " ")} · Checked{" "}
          {source.data.checked_at} (UTC)
        </p>
      )}
      {!contextAvailable && (
        <p role="alert">
          The selected draft could not be confirmed as current. Recover any retained submission
          before leaving.
        </p>
      )}
      <form
        noValidate
        aria-label="Posting submission"
        className="space-y-4"
        onSubmit={(event) => {
          void form.handleSubmit((values) => {
            if (
              original.current ||
              save.isPending ||
              !available ||
              !supervisorAvailable ||
              !source.data?.generation_id
            )
              return;
            const parsed = StaffPostingProposalInputContract.safeParse({
              reason: values.reason,
              sources_examined: values.sources_examined,
              ...(values.responsibility === "hospital_management"
                ? { supervision: { kind: "hospital_management" } }
                : (values.responsibility === "department_supervisor" ||
                      values.responsibility === "department_member") &&
                    currentSupervisor
                  ? {
                      supervision: {
                        kind: values.responsibility,
                        supervisor_staff_id: currentSupervisor.staff.id,
                        supervisor_proposal_id: currentSupervisor.id,
                        supervisor_revision_id: currentSupervisor.revision_id,
                      },
                    }
                  : {}),
              expected_version: 0,
              draft_id: draft.id,
              draft_revision_id: draft.revision_id,
              draft_version: draft.version,
              generation_id: source.data.generation_id,
              staff_version: draft.staff_version,
              employment_epoch: draft.employment_epoch,
            });
            if (!parsed.success) {
              parsed.error.issues.forEach((issue) => {
                const key = issue.path[0];
                if (key === "reason" || key === "sources_examined")
                  form.setError(key, { message: issue.message });
              });
              return;
            }
            const command = {
              proposal: newStaffPostingId(),
              command: newStaffPostingId(),
              input: parsed.data,
            };
            original.current = command;
            setRetained(command);
            save.mutate({ command, recover: false });
          })(event);
        }}
      >
        <fieldset disabled={editingDisabled} className="space-y-4">
          <Field
            id="posting-submission-reason"
            label="Submission rationale"
            error={form.formState.errors.reason?.message}
          >
            <Input id="posting-submission-reason" {...form.register("reason")} />
          </Field>
          <FormSelectField
            control={form.control}
            name="responsibility"
            id="posting-submission-responsibility"
            label="Posting responsibility"
            disabled={editingDisabled}
            options={[
              { value: "unspecified", label: "Preparation only" },
              { value: "hospital_management", label: "Hospital management" },
              { value: "department_supervisor", label: "Department supervisor" },
              { value: "department_member", label: "Department member" },
            ]}
            onValueChange={() => {
              form.setValue("supervisor", null, { shouldDirty: true });
              setSupervisorCursor(null);
            }}
          />
          <p className="text-sm text-muted-foreground">
            {responsibility === "hospital_management"
              ? "An independent management reviewer must approve this responsibility. It does not grant operating or clinical access."
              : needsSupervisor
                ? "Choose an approved supervisor reference. Your reviewer will check their current employment and department scope."
                : "Supervision is not specified. This proposal cannot be approved."}
          </p>
          {needsSupervisor && (
            <section
              aria-label="Approved supervisor references"
              className="space-y-3 rounded-md border border-border p-4"
            >
              <h3 className="font-medium">Choose a supervisor</h3>
              {supervisors.isPending || supervisors.isFetching ? (
                <p role="status">Loading approved supervisor references…</p>
              ) : supervisors.isError ? (
                <RequestFailure
                  message="Supervisor references could not be verified."
                  retry={() => {
                    form.setValue("supervisor", null, { shouldDirty: true });
                    return supervisors.refetch();
                  }}
                  error={supervisors.error}
                />
              ) : (
                <>
                  {supervisors.data.data
                    .filter((row) => row.staff.id !== scope.staffId)
                    .map((row) => (
                      <div
                        key={row.id}
                        className="space-y-2 border-b border-border pb-3 last:border-b-0 last:pb-0"
                      >
                        <div className="flex flex-wrap items-start justify-between gap-3">
                          <div>
                            <p className="font-medium">
                              {row.staff.display_name} · {row.staff.employee_code}
                            </p>
                            <p className="text-sm text-muted-foreground">
                              {row.kind === "hospital_management"
                                ? "Hospital management"
                                : "Department supervisor"}{" "}
                              · {row.starts_at} {row.ends_at ? `to ${row.ends_at}` : "· ongoing"}
                            </p>
                          </div>
                          <Button
                            type="button"
                            variant={selectedSupervisor?.id === row.id ? "default" : "outline"}
                            aria-pressed={selectedSupervisor?.id === row.id}
                            aria-label={`Choose supervisor ${row.staff.display_name}`}
                            onClick={() =>
                              form.setValue("supervisor", row, {
                                shouldDirty: true,
                                shouldValidate: true,
                              })
                            }
                          >
                            Choose supervisor
                          </Button>
                        </div>
                        <details className="text-sm text-muted-foreground">
                          <summary>Reviewed appointment details</summary>
                          <p className="break-all">
                            Posting reference: {row.id} · Revision: {row.revision_id}
                          </p>
                          <p className="break-all">
                            Reviewed department reference: {row.department_id} · Revision:{" "}
                            {row.department_revision_id}
                          </p>
                          <p>Reviewed source cutoff: {row.eligibility_until} (UTC)</p>
                        </details>
                      </div>
                    ))}
                  {supervisors.data.data.every((row) => row.staff.id === scope.staffId) && (
                    <p>No other Staff supervisor references on this page.</p>
                  )}
                  <div className="flex flex-wrap gap-2">
                    {supervisors.data.page.has_more && (
                      <Button
                        type="button"
                        variant="outline"
                        onClick={() => {
                          form.setValue("supervisor", null, { shouldDirty: true });
                          setSupervisorCursor(supervisors.data.page.next_cursor);
                        }}
                      >
                        More supervisor references
                      </Button>
                    )}
                    {supervisorCursor && (
                      <Button
                        type="button"
                        variant="outline"
                        onClick={() => {
                          form.setValue("supervisor", null, { shouldDirty: true });
                          setSupervisorCursor(null);
                        }}
                      >
                        First supervisor references
                      </Button>
                    )}
                  </div>
                </>
              )}
              {!supervisorAvailable && (
                <p className="text-sm text-muted-foreground">
                  Choose a supervisor reference before requesting independent review.
                </p>
              )}
            </section>
          )}
          <Field
            id="posting-submission-examined"
            label="I examined this draft and the current employment source."
            error={form.formState.errors.sources_examined?.message}
          >
            <input
              id="posting-submission-examined"
              type="checkbox"
              {...form.register("sources_examined")}
            />
          </Field>
        </fieldset>
        {save.error && (
          <p role="alert">
            The submission outcome could not be confirmed. Recover its receipt or retry the exact
            submission. A missing receipt may still belong to an in-flight request.
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          {retained ? (
            <>
              <Button
                type="button"
                disabled={save.isPending}
                onClick={() => save.mutate({ command: retained, recover: false })}
              >
                Retry exact submission
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={save.isPending}
                onClick={() => save.mutate({ command: retained, recover: true })}
              >
                Recover submission receipt
              </Button>
            </>
          ) : (
            <Button type="submit" disabled={!available || !supervisorAvailable || save.isPending}>
              Request independent review
            </Button>
          )}
        </div>
      </form>
    </section>
  );
}
