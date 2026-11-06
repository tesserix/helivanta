"use client";
import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  FormSelectField,
  Panel,
  RequestFailure,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  useStaffPostingDraftSource,
  useStaffPostingDrafts,
  useStaffPostingHistory,
  useStaffPostingDepartmentChoices,
  StaffPostingDraftInputContract,
  staffPostingInstantMicros,
  saveStaffPostingDraft,
  getStaffPostingDraftReceipt,
  newStaffPostingId,
  staffPostingsKey,
  useApiMutation,
  onTenantTransition,
  tenantTransitionPending,
  type StaffPostingScope,
  type StaffPostingDraft,
  type StaffPostingDraftInput,
  type EmploymentPeriodSource,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

type Props = {
  scope: StaffPostingScope;
  staffLabel: string;
  canManage: boolean;
  onClose: () => void;
  onContextChanged?: () => void;
};
export function StaffPostingDraftPanel({
  scope,
  staffLabel,
  canManage,
  onClose,
  onContextChanged,
}: Props) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [editor, setEditor] = useState<StaffPostingDraft | "new" | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const [historyCursor, setHistoryCursor] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [editorAccessChanged, setEditorAccessChanged] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const confirmLeave = useConfirmLeave();
  useEffect(() => {
    heading.current?.focus();
  }, []);
  const source = useStaffPostingDraftSource(scope);
  const drafts = useStaffPostingDrafts(scope, cursor);
  const revisions = useStaffPostingHistory(scope, history, historyCursor);
  const cutoff = useStaffAccessCutoff(
    [source.error, drafts.error, revisions.error],
    onContextChanged,
  );
  if (cutoff || editorAccessChanged) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  const available = source.data && !source.isError && !source.isFetching;
  return (
    <Panel className="mt-5 space-y-5 p-5" aria-label="Staff posting preparation">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 ref={heading} tabIndex={-1} className="font-semibold">
            Posting preparation · {staffLabel}
          </h3>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Prepare a department placement for review. A draft does not activate employment, assign
            a supervisor or grant operating access. All dates below use UTC.
          </p>
        </div>
        <Button variant="ghost" disabled={editor !== null} onClick={() => confirmLeave(onClose)}>
          Close posting preparation
        </Button>
      </div>
      {saved && <p role="status">Posting draft saved. Operating authority is unchanged.</p>}
      {source.isPending && <p role="status">Loading current Staff source…</p>}
      {source.isError && (
        <RequestFailure
          message="Current Staff source is unavailable. Recheck before preparing a draft."
          retry={source.refetch}
          error={source.error}
        />
      )}
      {canManage && source.data && !source.isError && !editor && (
        <Button
          variant="outline"
          disabled={!available}
          onClick={() => {
            setSaved(false);
            setEditor("new");
          }}
        >
          Prepare posting draft
        </Button>
      )}
      {drafts.isPending ? (
        <p role="status">Loading posting drafts…</p>
      ) : drafts.isError ? (
        <RequestFailure
          message="Posting drafts could not be verified."
          retry={drafts.refetch}
          error={drafts.error}
        />
      ) : (
        <div className="space-y-3">
          {drafts.data?.data.length === 0 && (
            <p className="text-sm text-muted-foreground">No posting drafts recorded.</p>
          )}
          {drafts.data?.data.map((row) => (
            <div
              key={row.id}
              className="flex flex-wrap items-center justify-between gap-3 border-b border-border py-3 text-sm"
            >
              <div>
                <p className="font-medium">Draft · version {row.version}</p>
                <p className="text-muted-foreground">
                  Department reference {row.department_id} · {row.starts_at}
                  {row.ends_at ? ` to ${row.ends_at}` : " · no proposed end"}
                </p>
              </div>
              <div className="flex gap-2">
                {canManage && (
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={!available || editor !== null}
                    onClick={() => {
                      setSaved(false);
                      setEditor(row);
                    }}
                  >
                    Edit draft
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setHistory(row.id);
                    setHistoryCursor(null);
                  }}
                >
                  View posting history
                </Button>
              </div>
            </div>
          ))}
          {drafts.data?.page.has_more && (
            <Button variant="outline" onClick={() => setCursor(drafts.data!.page.next_cursor)}>
              Next drafts
            </Button>
          )}
          {cursor && (
            <Button variant="ghost" onClick={() => setCursor(null)}>
              First drafts
            </Button>
          )}
        </div>
      )}
      {history && (
        <section aria-label="Posting revision history" className="space-y-2">
          <h4 className="font-medium">Immutable posting history</h4>
          {revisions.isPending ? (
            <p role="status">Loading posting history…</p>
          ) : revisions.isError ? (
            <p role="alert">Posting history could not be verified.</p>
          ) : (
            revisions.data?.data.map((row) => (
              <div key={row.revision_id} className="space-y-1 border-b border-border py-3 text-sm">
                <p className="font-medium">Version {row.version} · draft only</p>
                <p className="break-words">
                  Department reference {row.department_id} · {row.starts_at}
                  {row.ends_at ? ` to ${row.ends_at}` : " · no proposed end"}
                </p>
                <p className="text-muted-foreground">
                  Staff version {row.staff_version} · employment epoch {row.employment_epoch} ·
                  recorded {row.recorded_at}
                </p>
              </div>
            ))
          )}
          {revisions.data?.page.has_more && (
            <Button
              variant="outline"
              onClick={() => setHistoryCursor(revisions.data!.page.next_cursor)}
            >
              Older posting revisions
            </Button>
          )}
          <Button variant="ghost" onClick={() => setHistory(null)}>
            Close posting history
          </Button>
        </section>
      )}
      {editor && source.data && (
        <PostingEditor
          key={editor === "new" ? "new" : editor.id}
          scope={scope}
          source={source.data}
          sourceAvailable={Boolean(available)}
          item={editor === "new" ? undefined : editor}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            setSaved(true);
          }}
          onContextChanged={() => {
            setEditorAccessChanged(true);
            onContextChanged?.();
          }}
        />
      )}
    </Panel>
  );
}

const shape = StaffPostingDraftInputContract.shape;
const utcInput = z
  .string()
  .min(1, "Enter a UTC instant.")
  .transform((value) => (value.length === 16 ? `${value}:00Z` : `${value}Z`))
  .pipe(shape.starts_at);
const formSchema = z
  .object({
    department_id: shape.department_id,
    starts_at: utcInput,
    ends_at: z.union([z.literal(""), utcInput]),
    reason: shape.reason,
  })
  .superRefine((value, ctx) => {
    if (
      value.ends_at &&
      staffPostingInstantMicros(value.ends_at)! <= staffPostingInstantMicros(value.starts_at)!
    )
      ctx.addIssue({ code: "custom", path: ["ends_at"], message: "End must follow the start." });
  });
type Retained = { draft: string; command: string; input: StaffPostingDraftInput };
function PostingEditor({
  scope,
  source,
  sourceAvailable,
  item,
  onClose,
  onSaved,
  onContextChanged,
}: {
  scope: StaffPostingScope;
  source: EmploymentPeriodSource;
  sourceAvailable: boolean;
  item?: StaffPostingDraft;
  onClose: () => void;
  onSaved: () => void;
  onContextChanged?: () => void;
}) {
  const [basis] = useState(source);
  const [draft] = useState(() => item?.id ?? newStaffPostingId());
  const [retained, setRetained] = useState<Retained | null>(null);
  const [departmentCursor, setDepartmentCursor] = useState<string | null>(null);
  const active = useRef<AbortController | null>(null);
  useEffect(() => {
    const stop = onTenantTransition(() => {
      if (tenantTransitionPending()) active.current?.abort();
    });
    return () => {
      stop();
      active.current?.abort();
    };
  }, []);
  const departments = useStaffPostingDepartmentChoices(scope, departmentCursor);
  const form = useZodForm(formSchema, {
    department_id: item?.department_id ?? "",
    starts_at: item?.starts_at.replace(/Z$/, "") ?? "",
    ends_at: item?.ends_at?.replace(/Z$/, "") ?? "",
    reason: "",
  });
  const save = useApiMutation(
    async ({ command, recover }: { command: Retained; recover: boolean }) => {
      const controller = new AbortController();
      active.current = controller;
      const result = await (recover ? getStaffPostingDraftReceipt : saveStaffPostingDraft)(
        scope,
        command.draft,
        command.command,
        command.input,
        controller.signal,
      );
      controller.signal.throwIfAborted();
      return result;
    },
    { onSuccess: onSaved, suppressErrorToast: true, invalidate: [staffPostingsKey(scope)] },
  );
  const { confirm } = useLeaveGuard(
    form.formState.isDirty || retained !== null,
    "posting preparation; an unconfirmed command may already be saved",
    save.isPending,
  );
  const cutoff = useStaffAccessCutoff([save.error, departments.error], onContextChanged);
  const stale =
    basis.staff_version !== source.staff_version ||
    basis.employment_epoch !== source.employment_epoch;
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <form
      noValidate
      aria-label="Posting preparation"
      className="space-y-4 border-t border-border pt-5"
      onSubmit={form.handleSubmit((values) => {
        const department = departments.data?.data.find(
          (row) => row.id === values.department_id && row.status === "active",
        );
        if (!department) {
          form.setError("department_id", { message: "Choose an available active department." });
          return;
        }
        const parsed = StaffPostingDraftInputContract.safeParse({
          ...values,
          ends_at: values.ends_at || undefined,
          expected_version: item?.version ?? 0,
          staff_version: basis.staff_version,
          employment_epoch: basis.employment_epoch,
          department_revision_id: department.revision_id,
        });
        if (!parsed.success) {
          parsed.error.issues.forEach((issue) => {
            const key = issue.path[0];
            if (
              key === "department_id" ||
              key === "starts_at" ||
              key === "ends_at" ||
              key === "reason"
            )
              form.setError(key, { message: issue.message });
          });
          return;
        }
        const command = { draft, command: newStaffPostingId(), input: parsed.data };
        setRetained(command);
        save.mutate({ command, recover: false });
      })}
    >
      <h4 className="font-medium">
        {item ? `Edit posting draft · version ${item.version}` : "Prepare a posting draft"}
      </h4>
      <fieldset
        disabled={save.isPending || retained !== null}
        className="grid gap-4 md:grid-cols-2"
      >
        <div>
          <FormSelectField
            id="posting-department"
            control={form.control}
            name="department_id"
            label="Department or sub-unit"
            disabled={departments.isPending || departments.isError || retained !== null}
            options={(departments.data?.data ?? [])
              .filter((row) => row.status === "active")
              .map((row) => ({ value: row.id, label: `${row.code} · ${row.name}` }))}
          />
          {departments.data?.page.has_more && (
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                form.setValue("department_id", "");
                setDepartmentCursor(departments.data!.page.next_cursor);
              }}
            >
              Next departments
            </Button>
          )}
          {departmentCursor && (
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                form.setValue("department_id", "");
                setDepartmentCursor(null);
              }}
            >
              First departments
            </Button>
          )}
        </div>
        <Field
          id="posting-start"
          label="Start (UTC)"
          error={form.formState.errors.starts_at?.message}
        >
          <Input
            id="posting-start"
            type="datetime-local"
            step="any"
            {...form.register("starts_at")}
          />
        </Field>
        <Field
          id="posting-end"
          label="End (UTC, optional)"
          error={form.formState.errors.ends_at?.message}
        >
          <Input id="posting-end" type="datetime-local" step="any" {...form.register("ends_at")} />
        </Field>
        <Field
          id="posting-reason"
          label="Reason"
          error={
            form.formState.errors.reason
              ? "Enter a reason of 1–500 characters without control characters."
              : undefined
          }
        >
          <Input id="posting-reason" maxLength={500} {...form.register("reason")} />
        </Field>
      </fieldset>
      {departments.isError && (
        <RequestFailure
          message="Current departments are unavailable."
          retry={departments.refetch}
          error={departments.error}
        />
      )}
      {stale && (
        <p role="alert">Staff source changed. Close and reopen to review the current source.</p>
      )}
      {save.error && (
        <p role="alert">
          The command outcome could not be confirmed. Keep the exact command and recover its receipt
          or retry it. Source or version conflicts require a fresh review.
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
              Retry exact posting command
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={save.isPending}
              onClick={() => save.mutate({ command: retained, recover: true })}
            >
              Recover exact posting receipt
            </Button>
          </>
        ) : (
          <Button
            type="submit"
            disabled={
              save.isPending ||
              stale ||
              !sourceAvailable ||
              departments.isError ||
              departments.isPending
            }
          >
            Save posting draft
          </Button>
        )}
        <Button
          type="button"
          variant="ghost"
          disabled={save.isPending}
          onClick={() => confirm(onClose)}
        >
          Cancel posting preparation
        </Button>
      </div>
    </form>
  );
}
