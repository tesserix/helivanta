"use client";
import { useEffect, useId, useRef, useState } from "react";
import { z } from "zod";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";
import { Button, Input } from "@tesserix/web";
import { Panel, Field, FormSelectField, LoadMore, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  withStaffIntakeScope,
  EmploymentEvidenceAttachInputContract,
  EmploymentEvidenceBindingContract,
  attachEmploymentEvidence,
  downloadEmploymentEvidence,
  employmentEvidenceKey,
  employmentEvidenceMatches,
  employmentEvidencePath,
  employmentPeriodRequest,
  newEmploymentPeriodId,
  staffDocumentSchema,
  useApiMutation,
  useApiPagedQuery,
  withdrawEmploymentEvidence,
  type EmploymentEvidenceAttachInput,
  type EmploymentEvidenceBinding,
  type EmploymentEvidenceScope,
  type EmploymentEvidenceWithdrawInput,
  type StaffDocument,
} from "@helivanta/api";
const reason = EmploymentEvidenceAttachInputContract.shape.reason;
const formSchema = z.object({ document_id: z.string(), reason });
type Action = { kind: "attach" } | { kind: "withdraw"; file: EmploymentEvidenceBinding };
export function EmploymentEvidencePanel({
  scope,
  staffVersion,
  canAttach,
  onClose,
  onContextChanged,
}: {
  scope: EmploymentEvidenceScope;
  staffVersion: number;
  canAttach: boolean;
  onClose: () => void;
  onContextChanged?: () => void;
}) {
  const [action, setAction] = useState<Action | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [accessChanged, setAccessChanged] = useState(false);
  const loseAccess = () => {
    setAccessChanged(true);
    onContextChanged?.();
  };
  const files = useApiPagedQuery<EmploymentEvidenceBinding>(
    employmentEvidenceKey(scope),
    withStaffIntakeScope(employmentEvidencePath(scope), scope),
    { gcTime: 0, request: employmentPeriodRequest(scope) },
  );
  const parsed = EmploymentEvidenceBindingContract.array().safeParse(files.items);
  const valid = parsed.success && parsed.data.every((row) => employmentEvidenceMatches(scope, row));
  const cutoff = useStaffAccessCutoff([files.error], loseAccess);
  if (accessChanged || cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <Panel
      aria-label={`Employment evidence for terms revision ${scope.periodVersion}`}
      className="space-y-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h5 className="font-medium">
            Employment evidence · terms revision {scope.periodVersion}
          </h5>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Attach a scanned PNG or JPEG from this Staff member’s Documents. Each binding preserves
            its exact terms revision and file generation. Evidence does not approve employment or
            grant access.
          </p>
        </div>
        <Button variant="ghost" disabled={action !== null} onClick={onClose}>
          Close evidence
        </Button>
      </div>
      {!action && canAttach && valid && (
        <Button
          variant="outline"
          onClick={() => {
            setSaved(false);
            setAction({ kind: "attach" });
          }}
        >
          Attach scanned employment file
        </Button>
      )}
      {!canAttach && (
        <p className="text-sm text-muted-foreground">
          New evidence requires current draft terms and an unchanged Staff employment basis.
          Historical bindings remain available for review or withdrawal.
        </p>
      )}
      {action && (
        <EvidenceCommand
          key={action.kind === "attach" ? "attach" : action.file.id}
          scope={scope}
          staffVersion={staffVersion}
          action={action}
          onAccessChanged={loseAccess}
          onClose={() => setAction(null)}
          onSaved={() => {
            setAction(null);
            setSaved(true);
            files.retry();
          }}
        />
      )}
      {saved && <p role="status">Employment evidence saved.</p>}
      {files.isPending ? (
        <p role="status">Loading employment evidence…</p>
      ) : files.isError || !valid ? (
        <div role="alert">
          Could not verify employment evidence.{" "}
          <Button variant="ghost" onClick={files.retry}>
            Retry evidence
          </Button>
        </div>
      ) : parsed.data.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No employment evidence attached to this revision.
        </p>
      ) : (
        <ul className="divide-y">
          {parsed.data.map((file) => (
            <li key={file.id} className="space-y-3 py-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p className="font-medium break-all">{file.filename}</p>
                  <p className="text-xs text-muted-foreground">
                    {file.status === "attached" ? "Attached" : "Withdrawn"} · binding revision{" "}
                    {file.version} · scanned {new Date(file.scanned_at).toLocaleString()}
                  </p>
                  <p className="mt-1 text-sm">{file.reason}</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {file.status === "attached" && (
                    <>
                      <EvidenceDownload scope={scope} file={file} onAccessChanged={loseAccess} />
                      <Button
                        variant="outline"
                        disabled={action !== null}
                        onClick={() => {
                          setSaved(false);
                          setAction({ kind: "withdraw", file });
                        }}
                      >
                        Withdraw evidence
                      </Button>
                    </>
                  )}
                  <Button
                    variant="ghost"
                    aria-expanded={history === file.id}
                    onClick={() => setHistory(history === file.id ? null : file.id)}
                  >
                    Evidence history
                  </Button>
                </div>
              </div>
              {history === file.id && (
                <EvidenceHistory scope={scope} id={file.id} onAccessChanged={loseAccess} />
              )}
            </li>
          ))}
        </ul>
      )}
      <LoadMore hasMore={files.hasMore} isLoading={files.isFetchingMore} onClick={files.loadMore} />
    </Panel>
  );
}
function EvidenceCommand({
  scope,
  staffVersion,
  action,
  onClose,
  onSaved,
  onAccessChanged,
}: {
  scope: EmploymentEvidenceScope;
  staffVersion: number;
  action: Action;
  onClose: () => void;
  onSaved: () => void;
  onAccessChanged: () => void;
}) {
  const fieldId = useId();
  const [submitted, setSubmitted] = useState<{
    id: string;
    input: EmploymentEvidenceAttachInput | EmploymentEvidenceWithdrawInput;
  } | null>(null);
  const documents = useApiPagedQuery<StaffDocument>(
    [...employmentEvidenceKey(scope), "eligible-documents"],
    withStaffIntakeScope(
      `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/documents`,
      scope,
    ),
    {
      enabled: action.kind === "attach",
      gcTime: 0,

      request: employmentPeriodRequest(scope),
    },
  );
  const parsed = staffDocumentSchema.array().safeParse(documents.items);
  const valid =
    parsed.success &&
    parsed.data.every(
      (file) => file.hospital_id === scope.hospitalId && file.staff_id === scope.staffId,
    );
  const eligible = valid
    ? parsed.data.filter(
        (file) =>
          file.record_kind === "enrollment" &&
          file.record_id === scope.staffId &&
          file.record_version === staffVersion &&
          file.category === "employment_evidence" &&
          file.status === "available",
      )
    : [];
  const form = useZodForm(formSchema, { document_id: "", reason: "" });
  const save = useApiMutation(
    (command: NonNullable<typeof submitted>) =>
      action.kind === "attach"
        ? attachEmploymentEvidence(
            scope,
            command.id,
            command.input as EmploymentEvidenceAttachInput,
          )
        : withdrawEmploymentEvidence(
            scope,
            command.id,
            command.input as EmploymentEvidenceWithdrawInput,
          ),
    { invalidate: [employmentEvidenceKey(scope)], onSuccess: onSaved, suppressErrorToast: true },
  );
  const { confirm } = useLeaveGuard(
    form.formState.isDirty || submitted !== null,
    "employment evidence",
    save.isPending,
  );
  const cutoff = useStaffAccessCutoff([save.error, documents.error], onAccessChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <form
      aria-label={
        action.kind === "attach" ? "Attach employment evidence" : "Withdraw employment evidence"
      }
      className="space-y-4 border-t pt-4"
      noValidate
      onSubmit={form.handleSubmit((value) => {
        if (
          !submitted &&
          action.kind === "attach" &&
          !eligible.some((file) => file.id === value.document_id)
        ) {
          form.setError("document_id", { message: "Choose an available scanned employment file." });
          return;
        }
        const command = submitted ?? {
          id: action.kind === "attach" ? newEmploymentPeriodId() : action.file.id,
          input:
            action.kind === "attach"
              ? {
                  command_id: newEmploymentPeriodId(),
                  document_id: value.document_id,
                  reason: value.reason,
                }
              : {
                  command_id: newEmploymentPeriodId(),
                  expected_version: action.file.version,
                  reason: value.reason,
                },
        };
        setSubmitted(command);
        save.mutate(command);
      })}
    >
      <h6 className="font-medium">
        {action.kind === "attach" ? "Attach scanned file" : `Withdraw ${action.file.filename}`}
      </h6>
      <fieldset
        disabled={save.isPending || submitted !== null}
        className="grid gap-4 md:grid-cols-2"
      >
        {action.kind === "attach" && (
          <div>
            <FormSelectField
              control={form.control}
              name="document_id"
              id={`${fieldId}-file`}
              label="Scanned employment file"
              disabled={save.isPending || submitted !== null}
              options={[
                { value: "", label: "Choose scanned employment file" },
                ...eligible.map((file) => ({ value: file.id, label: file.filename })),
              ]}
            />
            {documents.isPending && <p role="status">Loading scanned files…</p>}
            {(documents.isError || !valid) && <p role="alert">Could not verify Staff documents.</p>}
            {!documents.isPending && valid && eligible.length === 0 && (
              <p className="mt-2 text-sm text-muted-foreground">
                No eligible file on this page. Upload employment evidence in Documents and wait for
                scanning, then reload this form.
              </p>
            )}
          </div>
        )}
        <Field
          id={`${fieldId}-reason`}
          label="Reason"
          error={form.formState.errors.reason?.message}
        >
          <Input id={`${fieldId}-reason`} maxLength={500} {...form.register("reason")} />
        </Field>
      </fieldset>
      {action.kind === "attach" && (
        <LoadMore
          hasMore={documents.hasMore}
          isLoading={documents.isFetchingMore}
          onClick={documents.loadMore}
        />
      )}
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error instanceof ApiError && save.error.status === 409
            ? "Evidence or terms changed. Check the current record before preparing a new command."
            : save.error.message}
        </p>
      )}
      <div className="flex flex-wrap justify-end gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={save.isPending}
          onClick={async () => {
            confirm(onClose);
          }}
        >
          Cancel evidence command
        </Button>
        <Button
          type="submit"
          disabled={save.isPending || (save.error instanceof ApiError && save.error.status === 409)}
        >
          {save.isPending
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
function EvidenceHistory({
  scope,
  id,
  onAccessChanged,
}: {
  scope: EmploymentEvidenceScope;
  id: string;
  onAccessChanged: () => void;
}) {
  const query = useApiPagedQuery<EmploymentEvidenceBinding>(
    [...employmentEvidenceKey(scope), id, "history"],
    withStaffIntakeScope(`${employmentEvidencePath(scope)}/${id}/history`, scope),
    { gcTime: 0, request: employmentPeriodRequest(scope) },
  );
  const parsed = EmploymentEvidenceBindingContract.array().safeParse(query.items);
  const cutoff = useStaffAccessCutoff([query.error], onAccessChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (query.isPending) return <p role="status">Loading evidence history…</p>;
  if (
    query.isError ||
    !parsed.success ||
    parsed.data.some((row) => row.id !== id || !employmentEvidenceMatches(scope, row))
  )
    return <p role="alert">Could not verify evidence history.</p>;
  return (
    <div className="space-y-3 border-t pt-3">
      <ol className="space-y-2 text-sm">
        {parsed.data.map((row) => (
          <li key={row.revision_id}>
            Revision {row.version} · {row.status} · {new Date(row.updated_at).toLocaleString()}
            <p>{row.reason}</p>
          </li>
        ))}
      </ol>
      <LoadMore hasMore={query.hasMore} isLoading={query.isFetchingMore} onClick={query.loadMore} />
    </div>
  );
}
function EvidenceDownload({
  scope,
  file,
  onAccessChanged,
}: {
  scope: EmploymentEvidenceScope;
  file: EmploymentEvidenceBinding;
  onAccessChanged: () => void;
}) {
  const controller = useRef<AbortController | null>(null);
  const urls = useRef(new Set<string>());
  useEffect(
    () => () => {
      controller.current?.abort();
      for (const url of urls.current) URL.revokeObjectURL(url);
    },
    [],
  );
  const download = useApiMutation(
    async () => {
      controller.current?.abort();
      const active = new AbortController();
      controller.current = active;
      const blob = await downloadEmploymentEvidence(scope, file, active.signal);
      active.signal.throwIfAborted();
      const url = URL.createObjectURL(blob);
      urls.current.add(url);
      const link = document.createElement("a");
      link.href = url;
      link.download = file.filename;
      link.click();
      window.setTimeout(() => {
        URL.revokeObjectURL(url);
        urls.current.delete(url);
      }, 0);
    },
    { suppressErrorToast: true },
  );
  const cutoff = useStaffAccessCutoff([download.error], onAccessChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <div>
      <Button variant="outline" disabled={download.isPending} onClick={() => download.mutate()}>
        Download evidence
      </Button>
      {download.error && (
        <p role="alert" className="text-sm text-destructive">
          Could not release evidence. It may have been withdrawn or your access changed.
        </p>
      )}
    </div>
  );
}
