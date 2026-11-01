"use client";
import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { X } from "lucide-react";
import {
  ApiError,
  CREDENTIAL_STATUSES,
  citableEvidence,
  credentialDecisionFormSchema,
  credentialReviewFormSchema,
  credentialReviewsPath,
  credentialsKey,
  credentialsPath,
  decideCredentialReview,
  proposeCredentialReview,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  useStaffDocuments,
  withdrawCredentialReview,
  type CredentialReview,
  type CredentialReviewForm,
  type CredentialScope,
  type EnrollmentRequestScope,
  type Hospital,
  type Staff,
  type StaffCredential,
  type StaffCredentials,
} from "@helivanta/api";
import { Field, LoadMore, formatDateTime, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "./roster-policy/policy-ui";

type View = { kind: "propose" | "decide" | "withdraw" | "history"; credential: StaffCredential };
const reviewStates: Record<CredentialReview["status"], string> = {
  pending: "Waiting for a decision",
  approved: "Approved",
  rejected: "Rejected",
  withdrawn: "Withdrawn",
};

export function standingText(status: StaffCredential["status"], days?: number) {
  const label = CREDENTIAL_STATUSES[status];
  const count = (n: number) => `${n} ${n === 1 ? "day" : "days"}`;
  if (days === undefined) return label;
  if (status === "expired") return `${label} ${count(-days)} ago`;
  if (status === "valid") return `${label} · ${count(days)} left`;
  return label;
}

function failureText(error: Error) {
  return error instanceof ApiError &&
    error.status === 409 &&
    error.detail.reason === "version_conflict"
    ? "This credential changed since you opened it. Close and reopen it to see the latest review."
    : error.message;
}

export function StaffCredentialPanel({
  hospital,
  staff,
  account,
  onClose,
}: {
  hospital: Hospital;
  staff: Staff;
  account: EnrollmentRequestScope;
  onClose: () => void;
}) {
  const { can, subject } = usePermissions();
  const scope = { hospitalId: hospital.id, staffId: staff.id };
  const record = useApiQuery<StaffCredentials>(credentialsKey(scope), credentialsPath(scope), {
    gcTime: 0,
    refetchOnWindowFocus: false,
    request: { cache: "no-store" },
  });
  const [view, setView] = useState<View | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start", behavior: "smooth" });
    heading.current?.focus();
  }, []);
  const reviewer = can("workforce.credentials.review");
  const close = () => setView(null);
  const data = record.data;
  return (
    <section
      aria-labelledby="staff-credentials-heading"
      className="mt-6 scroll-mt-24 overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <header className="flex items-start justify-between gap-4 border-b px-6 py-5">
        <div>
          <h3
            id="staff-credentials-heading"
            ref={heading}
            tabIndex={-1}
            className="text-xl font-semibold outline-none"
          >
            Credential validity
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {staff.display_name} · {staff.employee_code} · {hospital.name}
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close credential validity">
          <X className="size-4" />
        </Button>
      </header>
      <div className="space-y-4 p-6">
        <p className="max-w-2xl text-sm text-muted-foreground">
          A licence number or validity change takes effect only after a second person approves it
          against scanned evidence. Status uses the hospital calendar, and the expiry day still
          counts as valid.
        </p>
        {record.isPending ? (
          <p role="status">Loading credentials…</p>
        ) : record.isError ? (
          <Failure message="Could not load credentials." retry={() => void record.refetch()} />
        ) : (
          data && (
            <>
              <p className="text-xs text-muted-foreground">
                {data.time_zone
                  ? `Today is ${data.local_date} in ${data.time_zone}.`
                  : "Set the hospital time zone in roster settings to see whether credentials are in date."}
              </p>
              {data.credentials.length === 0 ? (
                <p className="rounded-xl border p-6 text-sm">
                  No credentials are enrolled. Add licences and registrations in the enrollment form
                  first.
                </p>
              ) : (
                <ul className="divide-y rounded-xl border" aria-label="Credentials">
                  {data.credentials.map((cred) => {
                    const pending = cred.pending;
                    const mine = pending?.proposed_by_subject === subject;
                    const shown = cred.reviewed ?? cred;
                    return (
                      <li key={cred.id} className="space-y-2 p-4">
                        <div className="flex flex-wrap items-start justify-between gap-3">
                          <div>
                            <p className="font-medium">
                              {cred.type} · {cred.issuer}
                            </p>
                            <p className="text-xs text-muted-foreground">
                              {shown.number} · {shown.valid_from ?? "—"} to{" "}
                              {shown.valid_until ?? "—"}
                              {cred.reviewed ? ` · Reviewed v${cred.reviewed.version}` : ""}
                            </p>
                          </div>
                          <span className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium">
                            {standingText(cred.status, cred.days_remaining)}
                          </span>
                        </div>
                        {pending && (
                          <p className="text-xs text-muted-foreground">
                            Proposed {pending.number} · {pending.valid_from} to{" "}
                            {pending.valid_until} by {mine ? "you" : pending.proposed_by_subject}
                          </p>
                        )}
                        <div className="flex flex-wrap gap-2">
                          {!pending && (
                            <Button
                              size="sm"
                              variant="outline"
                              aria-label={`Propose review of ${cred.type}`}
                              onClick={() => setView({ kind: "propose", credential: cred })}
                            >
                              Propose review
                            </Button>
                          )}
                          {pending && mine && (
                            <Button
                              size="sm"
                              variant="outline"
                              aria-label={`Withdraw proposal for ${cred.type}`}
                              onClick={() => setView({ kind: "withdraw", credential: cred })}
                            >
                              Withdraw proposal
                            </Button>
                          )}
                          {pending && !mine && reviewer && (
                            <Button
                              size="sm"
                              variant="outline"
                              aria-label={`Review proposal for ${cred.type}`}
                              onClick={() => setView({ kind: "decide", credential: cred })}
                            >
                              Review proposal
                            </Button>
                          )}
                          <Button
                            size="sm"
                            variant="ghost"
                            aria-label={`Review history for ${cred.type}`}
                            onClick={() => setView({ kind: "history", credential: cred })}
                          >
                            History
                          </Button>
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
              {view?.kind === "propose" && (
                <ProposeForm
                  account={account}
                  key={`${account.tenantId}:${account.subject}:${view.credential.id}`}
                  scope={scope}
                  hospital={hospital}
                  credential={view.credential}
                  documents={can("workforce.documents.manage")}
                  onClose={close}
                />
              )}
              {view?.kind === "decide" && view.credential.pending && (
                <DecisionForm
                  key={view.credential.pending.id}
                  scope={scope}
                  credential={view.credential}
                  review={view.credential.pending}
                  onClose={close}
                />
              )}
              {view?.kind === "withdraw" && view.credential.pending && (
                <WithdrawForm
                  key={view.credential.pending.id}
                  scope={scope}
                  credential={view.credential}
                  review={view.credential.pending}
                  onClose={close}
                />
              )}
              {view?.kind === "history" && (
                <History
                  key={view.credential.id}
                  scope={scope}
                  credential={view.credential}
                  onClose={close}
                />
              )}
            </>
          )
        )}
      </div>
    </section>
  );
}

function Panel({
  label,
  onClose,
  children,
}: {
  label: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label={label} className="space-y-4 rounded-xl border p-4 sm:p-5">
      <div className="flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {label}
        </h4>
        <Button variant="outline" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      {children}
    </div>
  );
}

function Problem({ error }: { error: Error | null }) {
  return error ? (
    <p role="alert" className="text-sm text-destructive">
      {failureText(error)}
    </p>
  ) : null;
}

function ProposeForm({
  scope,
  account,
  hospital,
  credential,
  documents,
  onClose,
}: {
  scope: CredentialScope;
  account: EnrollmentRequestScope;
  hospital: Hospital;
  credential: StaffCredential;
  documents: boolean;
  onClose: () => void;
}) {
  const source = credential.reviewed ?? credential;
  const form = useZodForm(credentialReviewFormSchema, {
    expected_version: credential.reviewed?.version ?? 0,
    number: source.number,
    valid_from: source.valid_from ?? "",
    valid_until: source.valid_until ?? "",
    evidence_document_ids: [],
    reason: "",
  });
  // One review id per distinct proposal, so a retried save replays rather than duplicating.
  const command = useRef<{ id: string; input: string } | null>(null);
  const save = useApiMutation(
    (input: CredentialReviewForm) => {
      const key = JSON.stringify(input);
      if (command.current?.input !== key) command.current = { id: crypto.randomUUID(), input: key };
      return proposeCredentialReview(scope, credential.id, command.current.id, input);
    },
    {
      invalidate: [credentialsKey(scope)],
      successToast: "Review proposed",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const errors = form.formState.errors;
  const chosen = form.watch("evidence_document_ids");
  const toggle = (id: string, on: boolean) =>
    form.setValue(
      "evidence_document_ids",
      on ? [...chosen, id] : chosen.filter((value) => value !== id),
      { shouldValidate: form.formState.isSubmitted },
    );
  return (
    <Panel label={`Propose review of ${credential.type}`} onClose={onClose}>
      <form
        noValidate
        autoComplete="off"
        aria-label="Propose credential review"
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <fieldset disabled={save.isPending} className="grid gap-4 sm:grid-cols-3">
          <Field id="credential-number" label="Number" error={errors.number?.message}>
            <Input id="credential-number" {...form.register("number")} />
          </Field>
          <Field id="credential-from" label="Valid from" error={errors.valid_from?.message}>
            <input
              id="credential-from"
              type="date"
              className={inputClass}
              {...form.register("valid_from")}
            />
          </Field>
          <Field id="credential-until" label="Valid until" error={errors.valid_until?.message}>
            <input
              id="credential-until"
              type="date"
              className={inputClass}
              {...form.register("valid_until")}
            />
          </Field>
        </fieldset>
        {documents ? (
          <EvidencePicker
            account={account}
            hospital={hospital}
            scope={scope}
            credential={credential.id}
            chosen={chosen}
            toggle={toggle}
            error={errors.evidence_document_ids?.message}
          />
        ) : (
          <p role="alert" className="text-sm">
            You need staff document access to cite evidence for a review.
          </p>
        )}
        <Field id="credential-reason" label="What you checked" error={errors.reason?.message}>
          <textarea
            id="credential-reason"
            rows={3}
            maxLength={500}
            className={inputClass}
            {...form.register("reason")}
          />
        </Field>
        <Problem error={save.error} />
        <Button type="submit" disabled={save.isPending || !documents}>
          {save.isPending ? "Proposing…" : "Propose review"}
        </Button>
      </form>
    </Panel>
  );
}

function EvidencePicker({
  account,
  hospital,
  scope,
  credential,
  chosen,
  toggle,
  error,
}: {
  hospital: Hospital;
  account: EnrollmentRequestScope;
  scope: CredentialScope;
  credential: string;
  chosen: string[];
  toggle: (id: string, on: boolean) => void;
  error?: string;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const files = useStaffDocuments(
    { ...account, hospitalId: hospital.id, staffId: scope.staffId },
    cursor,
    !!account.tenantId && !!account.subject,
  );
  const options = citableEvidence(files.data?.data ?? [], credential);
  return (
    <fieldset
      className="space-y-2"
      aria-describedby={error ? "credential-evidence-error" : undefined}
    >
      <legend className="text-sm font-medium">Evidence files</legend>
      {files.isError ? (
        <Failure
          message="Could not load this credential's evidence."
          retry={() => void files.refetch()}
        />
      ) : files.isPending ? (
        <p role="status" className="text-sm">
          Loading evidence…
        </p>
      ) : options.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No scanned evidence on this page. Upload it from Documents, choosing this credential.
        </p>
      ) : (
        options.map((file) => (
          <label key={file.id} className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={chosen.includes(file.id)}
              onChange={(ev) => toggle(file.id, ev.target.checked)}
            />
            {file.filename}
            {file.status === "scan_pending" && (
              <span className="text-xs text-muted-foreground">· still scanning</span>
            )}
          </label>
        ))
      )}
      <div className="flex gap-2">
        {cursor && (
          <Button type="button" size="sm" variant="outline" onClick={() => setCursor(null)}>
            First page
          </Button>
        )}
        {files.data?.page.has_more && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => setCursor(files.data?.page.next_cursor ?? null)}
          >
            More files
          </Button>
        )}
      </div>
      {error && (
        <p id="credential-evidence-error" className="text-xs text-destructive">
          {error}
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        {chosen.length} of up to 5 chosen. Approval waits until every file has finished scanning.
      </p>
    </fieldset>
  );
}

function Proposal({ review, current }: { review: CredentialReview; current: StaffCredential }) {
  const before = current.reviewed;
  return (
    <dl className="grid gap-3 text-sm sm:grid-cols-2">
      <div>
        <dt className="text-xs text-muted-foreground">Reviewed now</dt>
        <dd>
          {before
            ? `${before.number} · ${before.valid_from} to ${before.valid_until}`
            : "Nothing reviewed yet"}
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">Proposed</dt>
        <dd>
          {review.number} · {review.valid_from} to {review.valid_until}
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">Proposed by</dt>
        <dd>
          {review.proposed_by_subject} · {formatDateTime(review.created_at)}
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">Evidence</dt>
        <dd>
          {review.evidence_document_ids.length}{" "}
          {review.evidence_document_ids.length === 1 ? "file" : "files"} · open Documents to inspect
        </dd>
      </div>
      <div className="sm:col-span-2">
        <dt className="text-xs text-muted-foreground">What was checked</dt>
        <dd className="break-words">{review.reason}</dd>
      </div>
    </dl>
  );
}

function DecisionForm({
  scope,
  credential,
  review,
  onClose,
}: {
  scope: CredentialScope;
  credential: StaffCredential;
  review: CredentialReview;
  onClose: () => void;
}) {
  const form = useZodForm(credentialDecisionFormSchema, { decision: "approve", note: "" });
  const settle = (successToast: string) => ({
    invalidate: [credentialsKey(scope)],
    successToast,
    suppressErrorToast: true,
    onSuccess: onClose,
  });
  const post = (values: { decision: "approve" | "reject"; note: string }) =>
    decideCredentialReview(scope, credential.id, review.id, {
      expected_version: review.base_version,
      decision: values.decision,
      note: values.note.trim(),
    });
  const approve = useApiMutation(post, settle("Credential approved"));
  const reject = useApiMutation(post, settle("Credential rejected"));
  const busy = approve.isPending || reject.isPending;
  const submit = (decision: "approve" | "reject") => {
    form.setValue("decision", decision);
    void form.handleSubmit((values) =>
      (decision === "approve" ? approve : reject).mutate(values),
    )();
  };
  return (
    <Panel label={`Review proposal for ${credential.type}`} onClose={onClose}>
      <Proposal review={review} current={credential} />
      <form
        noValidate
        aria-label="Decide credential review"
        className="space-y-4"
        onSubmit={(ev) => {
          ev.preventDefault();
          submit("approve");
        }}
      >
        <Field
          id="credential-decision-note"
          label="Note (required to reject)"
          error={form.formState.errors.note?.message}
        >
          <textarea
            id="credential-decision-note"
            rows={2}
            maxLength={500}
            disabled={busy}
            className={inputClass}
            {...form.register("note")}
          />
        </Field>
        <Problem error={approve.error ?? reject.error} />
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={busy}>
            {approve.isPending ? "Approving…" : "Approve"}
          </Button>
          <Button type="button" variant="outline" disabled={busy} onClick={() => submit("reject")}>
            {reject.isPending ? "Rejecting…" : "Reject"}
          </Button>
        </div>
      </form>
    </Panel>
  );
}

function WithdrawForm({
  scope,
  credential,
  review,
  onClose,
}: {
  scope: CredentialScope;
  credential: StaffCredential;
  review: CredentialReview;
  onClose: () => void;
}) {
  const form = useZodForm(credentialDecisionFormSchema, { decision: "approve", note: "" });
  const withdraw = useApiMutation(
    (note: string) => withdrawCredentialReview(scope, credential.id, review.id, { note }),
    {
      invalidate: [credentialsKey(scope)],
      successToast: "Proposal withdrawn",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  return (
    <Panel label={`Withdraw proposal for ${credential.type}`} onClose={onClose}>
      <Proposal review={review} current={credential} />
      <form
        noValidate
        aria-label="Withdraw credential review"
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => withdraw.mutate(values.note.trim()))}
      >
        <Field
          id="credential-withdraw-note"
          label="Reason for withdrawing (optional)"
          error={form.formState.errors.note?.message}
        >
          <textarea
            id="credential-withdraw-note"
            rows={2}
            maxLength={500}
            disabled={withdraw.isPending}
            className={inputClass}
            {...form.register("note")}
          />
        </Field>
        <Problem error={withdraw.error} />
        <Button type="submit" disabled={withdraw.isPending}>
          {withdraw.isPending ? "Withdrawing…" : "Withdraw proposal"}
        </Button>
      </form>
    </Panel>
  );
}

function History({
  scope,
  credential,
  onClose,
}: {
  scope: CredentialScope;
  credential: StaffCredential;
  onClose: () => void;
}) {
  const path = credentialReviewsPath(scope, credential.id);
  const history = useApiPagedQuery<CredentialReview>([...credentialsKey(scope), "history"], path, {
    request: { cache: "no-store" },
  });
  return (
    <Panel label={`Review history for ${credential.type}`} onClose={onClose}>
      {history.isPending ? (
        <p role="status">Loading review history…</p>
      ) : history.isError ? (
        <Failure message="Could not load review history." retry={history.retry} />
      ) : history.items.length === 0 ? (
        <p className="text-sm text-muted-foreground">No reviews recorded for this credential.</p>
      ) : (
        <ol className="divide-y">
          {history.items.map((row) => (
            <li key={row.id} className="space-y-1 py-3 text-sm">
              <p className="font-medium">
                {reviewStates[row.status]} · {row.number} · {row.valid_from} to {row.valid_until}
              </p>
              <p className="text-xs text-muted-foreground">
                Proposed by {row.proposed_by_subject} · {formatDateTime(row.created_at)}
                {row.decided_by_subject &&
                  ` · ${row.status === "withdrawn" ? "Withdrawn" : "Decided"} by ${row.decided_by_subject}`}
                {row.decided_at && ` · ${formatDateTime(row.decided_at)}`}
              </p>
              <p className="break-words">{row.reason}</p>
              {row.decision_note && (
                <p className="break-words text-muted-foreground">Note: {row.decision_note}</p>
              )}
            </li>
          ))}
        </ol>
      )}
      <LoadMore
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </Panel>
  );
}
