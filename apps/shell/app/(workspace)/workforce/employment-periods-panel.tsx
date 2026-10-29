"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, LoadMore, Panel, useZodForm, FormSelectField, ConfirmDialog } from "@helivanta/ui";
import {
  ApiError,
  EmploymentPeriodSourceContract,
  EmploymentPeriodDraftContract,
  EmploymentPeriodDraftInputContract,
  employmentPeriodsKey,
  employmentPeriodsPath,
  employmentPeriodRequest,
  saveEmploymentPeriodDraft,
  newEmploymentPeriodId,
  useApiMutation,
  useApiQuery,
  useApiPagedQuery,
  type EmploymentPeriodDraft,
  type EmploymentPeriodDraftInput,
  type EmploymentPeriodScope,
  type EmploymentPeriodSource,
  type Staff,
} from "@helivanta/api";

function localTime(value: string) {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
const shape = EmploymentPeriodDraftInputContract.shape;
const termsForm = z
  .object({
    engagement_kind: shape.engagement_kind,
    duration: shape.duration,
    starts_local: z
      .string()
      .min(1, "Enter the effective start.")
      .refine((value) => Number.isFinite(Date.parse(value)), "Enter a valid start."),
    ends_local: z.string(),
    contracting_party: shape.contracting_party,
    sponsor_id: shape.sponsor_id,
    reason: shape.reason,
    evidence_reference: shape.evidence_reference,
  })
  .superRefine((value, ctx) => {
    if (!Number.isFinite(Date.parse(value.starts_local))) return;
    const parsed = EmploymentPeriodDraftInputContract.safeParse({
      command_id: "019a0000-0000-7000-8000-000000000001",
      expected_version: 0,
      staff_version: 1,
      employment_epoch: 1,
      engagement_kind: value.engagement_kind,
      duration: value.duration,
      starts_at: new Date(value.starts_local).toISOString(),
      ...(value.duration === "fixed" && Number.isFinite(Date.parse(value.ends_local))
        ? { ends_at: new Date(value.ends_local).toISOString() }
        : {}),
      contracting_party: value.contracting_party,
      sponsor_id: value.sponsor_id,
      reason: value.reason,
      evidence_reference: value.evidence_reference,
    });
    if (!parsed.success)
      for (const issue of parsed.error.issues)
        ctx.addIssue({
          code: "custom",
          path: issue.path.map((part) =>
            part === "starts_at" ? "starts_local" : part === "ends_at" ? "ends_local" : part,
          ),
          message: issue.message,
        });
  });

export function StaffEmploymentPeriods({
  scope,
  staffLabel,
  onClose,
}: {
  scope: EmploymentPeriodScope;
  staffLabel: string;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, [scope.staffId]);
  const source = useApiQuery<unknown>(
    [...employmentPeriodsKey(scope), "source"],
    `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-source`,
    { gcTime: 0, staleTime: 0, request: employmentPeriodRequest(scope) },
  );
  const periods = useApiPagedQuery<unknown>(
    employmentPeriodsKey(scope),
    employmentPeriodsPath(scope),
    { gcTime: 0, request: employmentPeriodRequest(scope) },
  );
  const [editor, setEditor] = useState<{ item?: EmploymentPeriodDraft } | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const basis = EmploymentPeriodSourceContract.safeParse(source.data);
  const rows = EmploymentPeriodDraftContract.array().safeParse(periods.items);
  const validSource =
    basis.success &&
    basis.data.hospital_id === scope.hospitalId &&
    basis.data.staff_id === scope.staffId;
  const validRows =
    rows.success &&
    rows.data.every(
      (row) => row.hospital_id === scope.hospitalId && row.staff_id === scope.staffId,
    );
  return (
    <Panel className="mt-5 space-y-5 p-5" aria-label="Staff employment terms">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 ref={heading} tabIndex={-1} className="font-semibold">
            Employment terms · {staffLabel}
          </h3>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Prepare appointment and contract terms. These drafts require a separate employment
            review and do not grant access, renew a contract or end employment.
          </p>
        </div>
        <Button variant="ghost" disabled={editor !== null} onClick={onClose}>
          Close employment terms
        </Button>
      </div>
      {source.isPending ? (
        <p role="status">Loading Staff employment basis…</p>
      ) : source.isError || !validSource ? (
        <p role="alert">
          Could not verify current Staff employment details. Close and reopen this panel.
        </p>
      ) : (
        <>
          {!editor && (
            <Button
              variant="outline"
              onClick={() => {
                setSaved(false);
                setEditor({});
              }}
            >
              Prepare employment terms
            </Button>
          )}
          {editor && (
            <EmploymentTermsEditor
              key={editor.item?.id ?? "new"}
              scope={scope}
              source={basis.data}
              item={editor.item}
              onClose={() => setEditor(null)}
              onSaved={() => {
                setEditor(null);
                setSaved(true);
                periods.retry();
              }}
            />
          )}
        </>
      )}
      {saved && <p role="status">Employment terms draft saved.</p>}
      {periods.isPending ? (
        <p role="status">Loading employment drafts…</p>
      ) : periods.isError || !validRows ? (
        <div role="alert">
          <p>Could not load employment drafts.</p>
          <Button variant="outline" onClick={periods.retry}>
            Retry drafts
          </Button>
        </div>
      ) : rows.data.length === 0 ? (
        <p className="text-sm text-muted-foreground">No employment terms drafts recorded.</p>
      ) : (
        <ul className="divide-y">
          {rows.data.map((row) => (
            <li key={row.id} className="space-y-3 p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h4 className="font-medium">{row.evidence_reference}</h4>
                  <p className="text-xs text-muted-foreground">
                    Draft · Revision {row.version} · {row.engagement_kind.replaceAll("_", " ")}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    disabled={editor !== null}
                    onClick={() => {
                      setSaved(false);
                      setEditor({ item: row });
                    }}
                  >
                    Edit terms
                  </Button>
                  <Button
                    variant="ghost"
                    onClick={() => setHistory(history === row.id ? null : row.id)}
                  >
                    Term history
                  </Button>
                </div>
              </div>
              <p className="text-sm">
                {new Date(row.starts_at).toLocaleString()} —{" "}
                {row.ends_at ? new Date(row.ends_at).toLocaleString() : "Ongoing"}
              </p>
              {row.contracting_party && <p className="text-sm">{row.contracting_party}</p>}
              {history === row.id && <EmploymentTermsHistory scope={scope} id={row.id} />}
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        hasMore={periods.hasMore}
        isLoading={periods.isFetchingMore}
        onClick={periods.loadMore}
      />
    </Panel>
  );
}
function EmploymentTermsHistory({ scope, id }: { scope: EmploymentPeriodScope; id: string }) {
  const query = useApiPagedQuery<unknown>(
    [...employmentPeriodsKey(scope), id, "history"],
    `${employmentPeriodsPath(scope)}/${id}/history`,
    { gcTime: 0, request: employmentPeriodRequest(scope) },
  );
  const parsed = EmploymentPeriodDraftContract.array().safeParse(query.items);
  if (query.isPending) return <p role="status">Loading term history…</p>;
  if (
    query.isError ||
    !parsed.success ||
    parsed.data.some(
      (row) =>
        row.id !== id || row.staff_id !== scope.staffId || row.hospital_id !== scope.hospitalId,
    )
  )
    return <p role="alert">Could not verify term history.</p>;
  return (
    <div className="space-y-3 border-t pt-3">
      <ol className="space-y-2 text-sm">
        {parsed.data.map((row) => (
          <li key={row.revision_id}>
            Revision {row.version} · {row.evidence_reference} ·{" "}
            {new Date(row.updated_at).toLocaleString()}
            <p className="text-muted-foreground">{row.reason}</p>
          </li>
        ))}
      </ol>
      <LoadMore hasMore={query.hasMore} isLoading={query.isFetchingMore} onClick={query.loadMore} />
    </div>
  );
}
function EmploymentTermsEditor({
  scope,
  source,
  item,
  onClose,
  onSaved,
}: {
  scope: EmploymentPeriodScope;
  source: EmploymentPeriodSource;
  item?: EmploymentPeriodDraft;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [id] = useState(() => item?.id ?? newEmploymentPeriodId());
  const [basis] = useState(() => source);
  const [submitted, setSubmitted] = useState<EmploymentPeriodDraftInput | null>(null);
  const [confirmClose, setConfirmClose] = useState(false);
  const form = useZodForm(termsForm, {
    engagement_kind: item?.engagement_kind ?? "employee",
    duration: item?.duration ?? "fixed",
    starts_local: item ? localTime(item.starts_at) : "",
    ends_local: item?.ends_at ? localTime(item.ends_at) : "",
    contracting_party: item?.contracting_party ?? "",
    sponsor_id: item?.sponsor_id ?? "",
    reason: item?.reason ?? "",
    evidence_reference: item?.evidence_reference ?? "",
  });
  const save = useApiMutation(
    (command: EmploymentPeriodDraftInput) => saveEmploymentPeriodDraft(scope, id, command),
    { invalidate: [employmentPeriodsKey(scope)], onSuccess: onSaved, suppressErrorToast: true },
  );
  const changed =
    basis.staff_version !== source.staff_version ||
    basis.employment_epoch !== source.employment_epoch;
  const sponsors = useApiPagedQuery<Staff>(
    [...employmentPeriodsKey(scope), "sponsors"],
    `/workforce/hospitals/${scope.hospitalId}/staff`,
    { gcTime: 0, request: employmentPeriodRequest(scope) },
  );
  const sponsor = form.watch("sponsor_id");
  const fixed = form.watch("duration") === "fixed";
  const contract = ["contractor", "agency_worker"].includes(form.watch("engagement_kind"));
  return (
    <form
      aria-label="Employment terms draft"
      noValidate
      className="space-y-4 border-t pt-4"
      onSubmit={form.handleSubmit((value) => {
        if (changed && !submitted) return;
        const command = submitted ?? {
          command_id: newEmploymentPeriodId(),
          expected_version: item?.version ?? 0,
          staff_version: basis.staff_version,
          employment_epoch: basis.employment_epoch,
          engagement_kind: value.engagement_kind,
          duration: value.duration,
          starts_at: new Date(value.starts_local).toISOString(),
          ...(value.duration === "fixed"
            ? { ends_at: new Date(value.ends_local).toISOString() }
            : {}),
          contracting_party: value.contracting_party,
          sponsor_id: value.sponsor_id,
          reason: value.reason,
          evidence_reference: value.evidence_reference,
        };
        setSubmitted(command);
        save.mutate(command);
      })}
    >
      <h4 className="font-medium">
        {item ? "Edit employment terms draft" : "Prepare employment terms draft"}
      </h4>
      <fieldset
        disabled={save.isPending || submitted !== null}
        className="grid gap-4 md:grid-cols-2"
      >
        <FormSelectField
          control={form.control}
          name="engagement_kind"
          id="period-kind"
          label="Engagement"
          disabled={save.isPending || submitted !== null}
          options={shape.engagement_kind.options.map((kind) => ({
            value: kind,
            label: kind.replaceAll("_", " "),
          }))}
        />
        <FormSelectField
          control={form.control}
          name="duration"
          id="period-duration"
          label="Duration"
          disabled={save.isPending || submitted !== null}
          options={[
            { value: "fixed", label: "Fixed term" },
            { value: "ongoing", label: "Ongoing" },
          ]}
        />
        <Field
          id="period-start"
          label="Effective start"
          error={form.formState.errors.starts_local?.message}
        >
          <Input id="period-start" type="datetime-local" {...form.register("starts_local")} />
        </Field>
        {fixed && (
          <Field
            id="period-end"
            label="Effective end"
            error={form.formState.errors.ends_local?.message}
          >
            <Input id="period-end" type="datetime-local" {...form.register("ends_local")} />
          </Field>
        )}
        <Field
          id="period-party"
          label={contract ? "Contracting party (required)" : "Contracting party"}
          error={form.formState.errors.contracting_party?.message}
        >
          <Input id="period-party" maxLength={200} {...form.register("contracting_party")} />
        </Field>
        <div>
          <FormSelectField
            control={form.control}
            name="sponsor_id"
            id="period-sponsor"
            label={contract ? "Hospital sponsor (required)" : "Hospital sponsor"}
            disabled={save.isPending || submitted !== null}
            options={[
              { value: "", label: "Choose sponsor" },
              ...sponsors.items
                .filter((row) => row.id !== scope.staffId && row.employment_status === "active")
                .map((row) => ({
                  value: row.id,
                  label: `${row.display_name} · ${row.employee_code}`,
                })),
              ...(sponsor && !sponsors.items.some((row) => row.id === sponsor)
                ? [{ value: sponsor, label: "Recorded sponsor — load more to view name" }]
                : []),
            ]}
          />
          {sponsors.hasMore && (
            <Button
              type="button"
              variant="ghost"
              onClick={sponsors.loadMore}
              disabled={sponsors.isFetchingMore}
            >
              More sponsors
            </Button>
          )}
          {sponsors.isError && (
            <p role="alert">
              Could not load sponsors.{" "}
              <Button type="button" variant="ghost" onClick={sponsors.retry}>
                Retry sponsors
              </Button>
            </p>
          )}
        </div>
        <Field
          id="period-evidence"
          label="Appointment or contract reference"
          error={form.formState.errors.evidence_reference?.message}
        >
          <Input id="period-evidence" maxLength={300} {...form.register("evidence_reference")} />
        </Field>
        <Field id="period-reason" label="Reason" error={form.formState.errors.reason?.message}>
          <Input id="period-reason" maxLength={500} {...form.register("reason")} />
        </Field>
      </fieldset>
      <p className="text-xs text-muted-foreground">
        Dates use your device timezone. A reference records the source of these draft terms; it does
        not verify the document.
      </p>
      {changed && <p role="alert">Staff details changed. Close and reopen before saving.</p>}
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error instanceof ApiError && save.error.status === 409
            ? "Staff details or this draft changed. Keep your draft, then close and reopen to review the current record."
            : "Could not confirm the save. Retry the same draft to recover its original result."}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={save.isPending}
          onClick={() => {
            if (form.formState.isDirty) setConfirmClose(true);
            else onClose();
          }}
        >
          Close draft form
        </Button>
        <Button type="submit" disabled={save.isPending || (changed && !submitted)}>
          {save.isPending ? "Saving…" : submitted ? "Retry same draft" : "Save draft"}
        </Button>
      </div>
      <ConfirmDialog
        open={confirmClose}
        onOpenChange={setConfirmClose}
        title="Discard employment draft edits?"
        description="Unsaved changes will be discarded. Saved employment terms will not change."
        confirmLabel="Discard edits"
        onConfirm={onClose}
      />
    </form>
  );
}
