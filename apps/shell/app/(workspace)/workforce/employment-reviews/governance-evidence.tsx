"use client";

import { useEffect, useId, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RefreshButton,
  RequestFailure,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  ApiError,
  EmploymentEvidenceAttachInputContract,
  attachGovernanceEmploymentEvidence,
  withdrawGovernanceEmploymentEvidence,
  downloadGovernanceEmploymentEvidence,
  governanceEmploymentBindingKey,
  newEmploymentPeriodId,
  onTenantTransition,
  tenantTransitionPending,
  useApiMutation,
  useGovernanceEmploymentDraft,
  useGovernanceEmploymentDocuments,
  useGovernanceEmploymentEvidence,
  useGovernanceEmploymentEvidenceHistory,
  type GovernanceEmploymentDocumentScope,
  type GovernanceEmploymentBindingScope,
  type EmploymentPeriodDraft,
  type EmploymentEvidenceBinding,
  type EmploymentEvidenceAttachInput,
  type EmploymentEvidenceWithdrawInput,
  type StaffDocument,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "../staff-access-cutoff";
import { EmploymentReviewPreparation } from "./preparation";

export function GovernanceEmploymentEvidence({
  scope,
  terms,
  onBusy,
  onClose,
  onAccessLost,
}: {
  scope: GovernanceEmploymentDocumentScope;
  terms: EmploymentPeriodDraft;
  onBusy: (busy: boolean) => void;
  onClose: () => void;
  onAccessLost: () => void;
}) {
  const current = useGovernanceEmploymentDraft(
    {
      tenantId: scope.tenantId,
      subject: scope.subject,
      hospitalId: scope.hospitalId,
      staffId: scope.staffId,
    },
    terms.id,
  );
  const cutoff = useStaffAccessCutoff([current.error], onAccessLost);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (current.isPending || current.isFetching)
    return <p role="status">Rechecking exact employment terms…</p>;
  if (current.isError)
    return (
      <>
        <RequestFailure
          message="Could not verify the selected terms revision."
          error={current.error}
          retry={current.refetch}
        />
        <Button variant="ghost" onClick={onClose}>
          Close terms evidence
        </Button>
      </>
    );
  if (
    current.data.version !== terms.version ||
    current.data.revision_id !== terms.revision_id ||
    current.data.status !== "draft"
  )
    return (
      <>
        <p role="alert">These terms changed. Close evidence and select the current revision.</p>
        <Button variant="ghost" onClick={onClose}>
          Close terms evidence
        </Button>
      </>
    );
  return (
    <BindingWorkspace
      scope={scope}
      terms={current.data}
      onBusy={onBusy}
      onClose={onClose}
      onAccessLost={onAccessLost}
    />
  );
}
type Action = { kind: "attach" } | { kind: "withdraw"; row: EmploymentEvidenceBinding };
function BindingWorkspace({
  scope,
  terms,
  onBusy,
  onClose,
  onAccessLost,
}: {
  scope: GovernanceEmploymentDocumentScope;
  terms: EmploymentPeriodDraft;
  onBusy: (busy: boolean) => void;
  onClose: () => void;
  onAccessLost: () => void;
}) {
  const bindingScope: GovernanceEmploymentBindingScope = {
    ...scope,
    periodId: terms.id,
    periodRevisionId: terms.revision_id,
    periodVersion: terms.version,
    periodStaffVersion: terms.staff_version,
    periodEmploymentEpoch: terms.employment_epoch,
  };
  const canPrepare =
    terms.staff_version === scope.staffVersion && terms.employment_epoch === scope.employmentEpoch;
  const files = useGovernanceEmploymentEvidence(bindingScope);
  const [action, setAction] = useState<Action | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const [review, setReview] = useState(false);
  const [notice, setNotice] = useState("");
  const controller = useRef<AbortController | null>(null);
  const urls = useRef(new Set<string>());
  useEffect(() => {
    const activeUrls = urls.current;
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) controller.current?.abort();
    });
    return () => {
      unsubscribe();
      controller.current?.abort();
      for (const url of activeUrls) URL.revokeObjectURL(url);
    };
  }, []);
  const download = useApiMutation(
    async (row: EmploymentEvidenceBinding) => {
      if (tenantTransitionPending()) throw new DOMException("Workspace changed", "AbortError");
      const active = new AbortController();
      controller.current = active;
      const blob = await downloadGovernanceEmploymentEvidence(bindingScope, row, active.signal);
      active.signal.throwIfAborted();
      const url = URL.createObjectURL(blob);
      urls.current.add(url);
      const link = document.createElement("a");
      link.href = url;
      link.download = row.filename;
      link.click();
      window.setTimeout(() => {
        URL.revokeObjectURL(url);
        urls.current.delete(url);
      }, 0);
    },
    { gcTime: 0, suppressErrorToast: true },
  );
  const busy = action !== null || review || download.isPending;
  useEffect(() => {
    onBusy(busy);
    return () => onBusy(false);
  }, [busy, onBusy]);
  const cutoff = useStaffAccessCutoff([files.error, download.error], onAccessLost);
  useEffect(() => {
    if (cutoff) controller.current?.abort();
  }, [cutoff]);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  const rows = files.data?.pages.flatMap((page) => page.data) ?? [];
  if (review)
    return (
      <EmploymentReviewPreparation
        authority="governance"
        scope={{
          tenantId: scope.tenantId,
          subject: scope.subject,
          hospitalId: scope.hospitalId,
          staffId: scope.staffId,
          hrIntake: true,
        }}
        terms={terms}
        evidence={rows}
        onClose={() => setReview(false)}
        onAccessLost={onAccessLost}
        onSubmitted={() => {
          setReview(false);
          setNotice(
            "Employment submitted for independent review. Staff and access remain unchanged.",
          );
        }}
      />
    );
  return (
    <Panel aria-label="Exact employment terms evidence" className="space-y-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h5 className="font-semibold">
          {terms.evidence_reference} · terms revision {terms.version}
        </h5>
        <Button variant="ghost" disabled={busy} onClick={onClose}>
          Close terms evidence
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        Bind a scanned employment file to this exact appointment or contract revision. Independent
        review is required before employment can be activated.
      </p>
      {notice && <p role="status">{notice}</p>}
      {!canPrepare && (
        <p role="status">
          The Staff employment basis changed. Existing evidence remains available; prepare current
          terms before attaching or submitting new evidence.
        </p>
      )}
      {!action && (
        <div className="flex flex-wrap gap-2">
          <RefreshButton
            busy={busy || files.isFetching}
            error={files.error}
            refresh={files.refetch}
          >
            Refresh terms evidence
          </RefreshButton>
          {canPrepare && (
            <Button
              disabled={busy || files.isPending || files.isFetching || files.isError}
              onClick={() => {
                setNotice("");
                setAction({ kind: "attach" });
              }}
            >
              Attach scanned employment file
            </Button>
          )}
          {canPrepare && rows.some((row) => row.status === "attached") && !files.hasNextPage && (
            <Button
              variant="outline"
              disabled={busy || files.isPending || files.isFetching || files.isError}
              onClick={() => setReview(true)}
            >
              Prepare independent employment review
            </Button>
          )}
        </div>
      )}
      {action && (
        <BindingCommand
          scope={bindingScope}
          documentsScope={scope}
          action={action}
          existing={rows}
          onAccessLost={onAccessLost}
          onClose={() => setAction(null)}
          onSaved={() => {
            setAction(null);
            setNotice("Employment evidence saved for this terms revision.");
          }}
        />
      )}
      {files.isPending || files.isFetching ? (
        <p role="status">Checking exact terms evidence…</p>
      ) : files.isError ? (
        <RequestFailure
          message="Could not verify terms evidence."
          error={files.error}
          retry={files.refetch}
        />
      ) : (
        <>
          {rows.length === 0 && <p>No evidence bound to this terms revision.</p>}
          <ul className="space-y-3">
            {rows.map((row) => (
              <li key={row.id} className="space-y-3 rounded-md border p-3">
                <p className="break-all font-medium">{row.filename}</p>
                <p className="text-sm text-muted-foreground">
                  {row.status === "attached" ? "Attached" : "Withdrawn"} · evidence revision{" "}
                  {row.version}
                </p>
                <p className="text-sm">{row.reason}</p>
                <div className="flex flex-wrap gap-2">
                  {row.status === "attached" && (
                    <>
                      <Button
                        variant="outline"
                        disabled={busy}
                        onClick={() => download.mutate(row)}
                      >
                        Download evidence
                      </Button>
                      <Button
                        variant="outline"
                        disabled={busy}
                        onClick={() => {
                          setNotice("");
                          setAction({ kind: "withdraw", row });
                        }}
                      >
                        Withdraw evidence
                      </Button>
                    </>
                  )}
                  <Button
                    variant="ghost"
                    disabled={busy}
                    aria-expanded={history === row.id}
                    onClick={() => setHistory(history === row.id ? null : row.id)}
                  >
                    Evidence history
                  </Button>
                </div>
                {history === row.id && (
                  <BindingHistory scope={bindingScope} id={row.id} onAccessLost={onAccessLost} />
                )}
              </li>
            ))}
          </ul>
          <LoadMore
            hasMore={files.hasNextPage}
            isLoading={files.isFetchingNextPage || busy}
            onClick={() => void files.fetchNextPage()}
          />
        </>
      )}
      {download.isError && (
        <RequestFailure
          message="Could not verify the protected evidence download. Recheck its current status before trying again."
          retry={() => download.variables && download.mutate(download.variables)}
          error={download.error}
        />
      )}
    </Panel>
  );
}
const commandSchema = z.object({
  document_id: z.string(),
  reason: EmploymentEvidenceAttachInputContract.shape.reason,
});
type Command =
  | { kind: "attach"; id: string; input: EmploymentEvidenceAttachInput; file: StaffDocument }
  | { kind: "withdraw"; row: EmploymentEvidenceBinding; input: EmploymentEvidenceWithdrawInput };
function BindingCommand({
  scope,
  documentsScope,
  action,
  existing,
  onAccessLost,
  onClose,
  onSaved,
}: {
  scope: GovernanceEmploymentBindingScope;
  documentsScope: GovernanceEmploymentDocumentScope;
  action: Action;
  existing: EmploymentEvidenceBinding[];
  onAccessLost: () => void;
  onClose: () => void;
  onSaved: () => void;
}) {
  const field = useId();
  const documents = useGovernanceEmploymentDocuments(documentsScope, action.kind === "attach");
  const candidates = (documents.data?.pages.flatMap((page) => page.data) ?? []).filter(
    (file) =>
      file.status === "available" &&
      file.record_version === scope.periodStaffVersion &&
      !existing.some((row) => row.status === "attached" && row.document_id === file.id),
  );
  const form = useZodForm(commandSchema, { document_id: "", reason: "" });
  const [submitted, setSubmitted] = useState<Command | null>(null);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => {
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) controller.current?.abort();
    });
    return () => {
      unsubscribe();
      controller.current?.abort();
    };
  }, []);
  const mutation = useApiMutation(
    async (command: Command) => {
      if (tenantTransitionPending()) throw new DOMException("Workspace changed", "AbortError");
      const active = new AbortController();
      controller.current = active;
      return command.kind === "attach"
        ? attachGovernanceEmploymentEvidence(
            scope,
            command.id,
            command.input,
            command.file,
            active.signal,
          )
        : withdrawGovernanceEmploymentEvidence(scope, command.row, command.input, active.signal);
    },
    {
      gcTime: 0,
      suppressErrorToast: true,
      invalidate: [governanceEmploymentBindingKey(scope)],
      onSuccess: onSaved,
    },
  );
  const cutoff = useStaffAccessCutoff([mutation.error, documents.error], onAccessLost);
  useEffect(() => {
    if (cutoff) controller.current?.abort();
  }, [cutoff]);
  const { confirm } = useLeaveGuard(
    form.formState.isDirty || submitted !== null,
    "exact employment evidence",
    mutation.isPending,
  );
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <form
      noValidate
      aria-label={
        action.kind === "attach"
          ? "Attach exact employment evidence"
          : "Withdraw exact employment evidence"
      }
      className="space-y-4 border-t pt-4"
      onSubmit={form.handleSubmit((value) => {
        let command = submitted;
        if (!command) {
          if (action.kind === "attach") {
            const selected = candidates.find((file) => file.id === value.document_id);
            if (!selected) {
              form.setError("document_id", {
                message: "Choose an available scanned employment file.",
              });
              return;
            }
            command = {
              kind: "attach",
              id: newEmploymentPeriodId(),
              input: {
                command_id: newEmploymentPeriodId(),
                document_id: selected.id,
                reason: value.reason,
              },
              file: selected,
            };
          } else
            command = {
              kind: "withdraw",
              row: action.row,
              input: {
                command_id: newEmploymentPeriodId(),
                expected_version: action.row.version,
                reason: value.reason,
              },
            };
          setSubmitted(command);
        }
        mutation.mutate(command);
      })}
    >
      <h6 className="font-medium">
        {action.kind === "attach" ? "Attach exact scanned file" : `Withdraw ${action.row.filename}`}
      </h6>
      <fieldset
        disabled={mutation.isPending || submitted !== null}
        className="grid gap-4 md:grid-cols-2"
      >
        {action.kind === "attach" && (
          <FormSelectField
            control={form.control}
            name="document_id"
            id={`${field}-file`}
            label="Scanned employment file"
            options={[
              { value: "", label: "Choose a scanned employment file" },
              ...candidates.map((file) => ({ value: file.id, label: file.filename })),
            ]}
          />
        )}
        <Field id={`${field}-reason`} label="Reason" error={form.formState.errors.reason?.message}>
          <Input id={`${field}-reason`} maxLength={500} {...form.register("reason")} />
        </Field>
      </fieldset>
      {action.kind === "attach" &&
        (documents.isPending || documents.isFetching ? (
          <p role="status">Checking scanned files…</p>
        ) : documents.isError ? (
          <RequestFailure
            message="Could not verify scanned employment files."
            error={documents.error}
            retry={documents.refetch}
          />
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              Only released employment images for this Staff version can be attached. Prepare files
              from the Staff selection screen, then reopen evidence.
            </p>
            <LoadMore
              hasMore={documents.hasNextPage}
              isLoading={documents.isFetchingNextPage || submitted !== null}
              onClick={() => void documents.fetchNextPage()}
            />
          </>
        ))}
      {mutation.isError && (
        <p role="alert">
          {mutation.error instanceof ApiError && mutation.error.status === 409
            ? "Evidence or terms changed. Close evidence and reselect the current revision before a new command."
            : "Could not confirm the binding. Retry the same command to recover its result."}
        </p>
      )}
      <div className="flex flex-wrap justify-end gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={mutation.isPending}
          onClick={() => confirm(onClose)}
        >
          Cancel evidence command
        </Button>
        <Button
          type="submit"
          disabled={
            mutation.isPending ||
            (mutation.error instanceof ApiError && mutation.error.status === 409) ||
            (submitted === null &&
              action.kind === "attach" &&
              (documents.isPending || documents.isFetching || documents.isError))
          }
        >
          {mutation.isPending
            ? "Saving evidence…"
            : submitted
              ? "Retry same evidence command"
              : action.kind === "attach"
                ? "Attach evidence"
                : "Confirm withdrawal"}
        </Button>
      </div>
    </form>
  );
}
function BindingHistory({
  scope,
  id,
  onAccessLost,
}: {
  scope: GovernanceEmploymentBindingScope;
  id: string;
  onAccessLost: () => void;
}) {
  const history = useGovernanceEmploymentEvidenceHistory(scope, id);
  const cutoff = useStaffAccessCutoff([history.error], onAccessLost);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (history.isPending || history.isFetching)
    return <p role="status">Checking evidence history…</p>;
  if (history.isError)
    return (
      <RequestFailure
        message="Could not verify exact evidence history."
        error={history.error}
        retry={history.refetch}
      />
    );
  return (
    <div className="space-y-3 border-t pt-3">
      <ol className="space-y-2">
        {history.data.pages
          .flatMap((page) => page.data)
          .map((row) => (
            <li key={row.revision_id}>
              Revision {row.version} · {row.status === "attached" ? "Attached" : "Withdrawn"} ·{" "}
              {row.reason}
            </li>
          ))}
      </ol>
      <LoadMore
        hasMore={history.hasNextPage}
        isLoading={history.isFetchingNextPage}
        onClick={() => void history.fetchNextPage()}
      />
    </div>
  );
}
