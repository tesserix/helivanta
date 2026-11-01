"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { Field, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  PersonReviewDecisionInputContract,
  useApiMutation,
  usePersonReviewDetail,
  usePersonReviewHistory,
  newPersonReviewId,
  decidePersonReview,
  withdrawPersonReview,
  revokePersonReview,
  downloadPersonReviewEvidence,
  type PersonReview,
  type PersonReviewView,
  type PersonReviewScope,
  type PersonReviewSummary,
  type PersonReviewHospitalScope,
  type PersonReviewDecisionInput,
  type PersonReviewActionInput,
} from "@helivanta/api";
import { useStaffAccessCutoff } from "../staff-access-cutoff";
import { personReviewContextChanged } from "./person-review-context";
import { RecordedPersonReviewHistory } from "./review-history";

type Props = {
  scope: PersonReviewScope;
  item: PersonReviewSummary;
  capabilities: PersonReviewHospitalScope;
  onBusy: (value: boolean) => void;
  onAccessLost: () => void;
  onClose: () => void;
  onDone: (receipt: PersonReview) => void;
};
type Command =
  | { action: "approve" | "reject"; input: PersonReviewDecisionInput }
  | { action: "withdraw" | "revoke"; input: PersonReviewActionInput };

const stateLabels: Record<PersonReviewView["current_state"], string> = {
  pending: "Pending review",
  approved: "Approved finding",
  rejected: "Rejected finding",
  withdrawn: "Withdrawn",
  revoked: "Revoked",
  expired: "Expired",
  source_changed: "Identity details changed",
  evidence_unavailable: "Evidence unavailable",
};
const formSchema = PersonReviewDecisionInputContract.pick({
  reason: true,
  identity_examined: true,
  independent_person_confirmed: true,
});

export function PersonReviewDetail(props: Props) {
  const details = usePersonReviewDetail(props.scope, props.item.id);
  const cutoff = useStaffAccessCutoff(
    [details.error],
    props.onAccessLost,
    personReviewContextChanged,
  );
  if (cutoff) return <p role="alert">Review access changed. Refresh access to continue.</p>;
  if (details.isPending || details.isFetching)
    return <p role="status">Loading protected review details…</p>;
  if (details.isError)
    return (
      <div role="alert">
        <p>Could not verify current review details.</p>
        <Button variant="outline" onClick={() => void details.refetch()}>
          Retry review details
        </Button>
      </div>
    );
  return (
    <ReviewFinding
      key={`${details.data.review.id}:${details.data.review.version}`}
      {...props}
      view={details.data}
    />
  );
}

function ReviewFinding({
  scope,
  item,
  capabilities,
  view,
  onBusy,
  onAccessLost,
  onClose,
  onDone,
}: Props & { view: PersonReviewView }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, []);
  const [submitted, setSubmitted] = useState<Command | null>(null);
  const [showHistory, setShowHistory] = useState(false);
  const [historyCursors, setHistoryCursors] = useState<(string | null)[]>([null]);
  const history = usePersonReviewHistory(
    scope,
    item.id,
    historyCursors.at(-1) ?? null,
    showHistory,
  );
  const [seenHistoryVersion, setSeenHistoryVersion] = useState(0);
  const highestHistoryVersion = Math.max(
    seenHistoryVersion,
    ...(history.data?.data.map((receipt) => receipt.version) ?? [0]),
  );
  const newerReceipt = highestHistoryVersion > view.review.version;
  const original = useRef<Command | null>(null);
  const [downloading, setDownloading] = useState<string | null>(null);
  const [downloadError, setDownloadError] = useState<Error | null>(null);
  const controller = useRef<AbortController | null>(null);
  const urls = useRef(new Set<string>());
  useEffect(
    () => () => {
      controller.current?.abort();
      for (const url of urls.current) URL.revokeObjectURL(url);
      urls.current.clear();
    },
    [],
  );
  const form = useZodForm(formSchema, {
    reason: "",
    identity_examined: false,
    independent_person_confirmed: false,
  });
  useEffect(() => {
    onBusy(form.formState.isDirty || submitted !== null || downloading !== null);
  }, [form.formState.isDirty, submitted, downloading, onBusy]);
  const { confirm } = useLeaveGuard(
    form.formState.isDirty || submitted !== null,
    "person review decision",
    submitted !== null,
  );
  const mutation = useApiMutation(
    (command: Command) => {
      if (command.action === "approve" || command.action === "reject")
        return decidePersonReview(scope, item.id, command.input);
      if (command.action === "withdraw") return withdrawPersonReview(scope, item.id, command.input);
      return revokePersonReview(scope, item.id, command.input);
    },
    {
      suppressErrorToast: true,
      onError: (error) => {
        if (error instanceof ApiError && error.status === 400) {
          original.current = null;
          setSubmitted(null);
        }
      },
      onSuccess: (receipt) => {
        onBusy(false);
        onDone(receipt);
      },
    },
  );
  const cutoff = useStaffAccessCutoff(
    [mutation.error, downloadError, history.error],
    onAccessLost,
    personReviewContextChanged,
  );
  const row = view.review;
  const self = row.proposed_by_subject === scope.subject || row.declared_subject === scope.subject;
  const canDecide = capabilities.can_review && view.current_state === "pending" && !self;
  const canWithdraw =
    capabilities.can_submit &&
    row.proposed_by_subject === scope.subject &&
    row.status === "pending";
  const canRevoke = capabilities.can_review && row.status === "approved";
  const locked = submitted !== null || downloading !== null;
  const actionLocked = locked || newerReceipt || history.isFetching;
  const canDownload = ["pending", "approved"].includes(view.current_state);
  function decide(action: Command["action"]) {
    if (original.current || downloading || newerReceipt || history.isFetching) return;
    void form.handleSubmit((values) => {
      if (original.current) return;
      if (action === "approve" || action === "reject") {
        if (!values.independent_person_confirmed) {
          form.setError("independent_person_confirmed", {
            message: "Confirm your independence before recording a decision.",
          });
          return;
        }
        if (action === "approve" && !values.identity_examined) {
          form.setError("identity_examined", {
            message: "Confirm your examination before approving.",
          });
          return;
        }
      }
      const base = {
        command_id: newPersonReviewId(),
        expected_version: row.version,
        reason: values.reason,
      };
      const command: Command =
        action === "approve" || action === "reject"
          ? {
              action,
              input: {
                ...base,
                decision: action,
                identity_examined: values.identity_examined,
                independent_person_confirmed: values.independent_person_confirmed,
              },
            }
          : { action, input: base };
      original.current = command;
      setSubmitted(command);
      onBusy(true);
      mutation.mutate(command);
    })();
  }
  async function download(fileId: string, filename: string) {
    if (actionLocked) return;
    const active = new AbortController();
    controller.current?.abort();
    controller.current = active;
    setDownloading(fileId);
    setDownloadError(null);
    onBusy(true);
    try {
      const blob = await downloadPersonReviewEvidence(scope, item.id, view, fileId, active.signal);
      active.signal.throwIfAborted();
      const url = URL.createObjectURL(blob);
      urls.current.add(url);
      try {
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = filename;
        anchor.click();
      } finally {
        window.setTimeout(() => {
          if (urls.current.delete(url)) URL.revokeObjectURL(url);
        }, 0);
      }
    } catch (error) {
      if (!active.signal.aborted)
        setDownloadError(error instanceof Error ? error : new Error("Could not verify evidence."));
    } finally {
      if (!active.signal.aborted) {
        setDownloading(null);
      }
    }
  }
  if (cutoff) return <p role="alert">Review access changed. Refresh access to continue.</p>;
  return (
    <section aria-label="Person review details" className="space-y-4 border-t pt-5">
      <Button
        variant="outline"
        disabled={mutation.isPending}
        onClick={() => {
          setSeenHistoryVersion(highestHistoryVersion);
          setShowHistory((value) => !value);
          if (!showHistory) setHistoryCursors([null]);
        }}
      >
        {showHistory ? "Close history" : "View history"}
      </Button>
      {showHistory && (
        <RecordedPersonReviewHistory
          history={history}
          subject={scope.subject}
          hasPrevious={historyCursors.length > 1}
          onPrevious={() => {
            setSeenHistoryVersion(highestHistoryVersion);
            setHistoryCursors((value) => value.slice(0, -1));
          }}
          onNext={(cursor) => {
            setSeenHistoryVersion(highestHistoryVersion);
            setHistoryCursors((value) => [...value, cursor]);
          }}
        />
      )}
      {newerReceipt && (
        <p role="alert">
          A newer receipt was recorded. Close and reopen this review before another action.
        </p>
      )}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 ref={heading} tabIndex={-1} className="font-semibold">
            {item.display_name} · {item.employee_code}
          </h3>
          <p className="text-sm text-muted-foreground">
            Finding when opened: {stateLabels[view.current_state]}
          </p>
        </div>
        <Button
          variant="ghost"
          disabled={locked}
          onClick={() =>
            confirm(() => {
              onBusy(false);
              onClose();
            })
          }
        >
          Close review
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        A person finding does not activate employment, create a login link or grant access.
      </p>
      <dl className="grid gap-4 sm:grid-cols-2">
        <div>
          <dt className="text-xs text-muted-foreground">Hospital policy</dt>
          <dd className="break-words font-medium">{row.policy_reference}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Recorded observation</dt>
          <dd className="break-words">{row.finding_reference}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Observation date (UTC)</dt>
          <dd>{row.observed_on}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Valid until</dt>
          <dd>{new Date(row.valid_until).toLocaleString()}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Account intent</dt>
          <dd>
            {row.access_intent === "non_digital"
              ? "No digital access requested"
              : `Digital access requested for ${row.declared_subject}`}
          </dd>
        </div>
        {row.decision_reason && (
          <div>
            <dt className="text-xs text-muted-foreground">Recorded decision reason</dt>
            <dd className="break-words">{row.decision_reason}</dd>
          </div>
        )}
      </dl>
      <div className="space-y-2">
        <h4 className="font-medium">Bound identity evidence</h4>
        <ul className="divide-y">
          {row.evidence.map((file) => (
            <li
              key={file.document_id}
              className="flex flex-wrap items-center justify-between gap-3 py-3"
            >
              <div>
                <p className="break-words">{file.filename}</p>
                <p className="text-xs text-muted-foreground">
                  {file.content_type} · {Math.ceil(file.size / 1024)} KB · scan released
                </p>
              </div>
              <Button
                variant="outline"
                disabled={actionLocked || !canDownload}
                onClick={() => void download(file.document_id, file.filename)}
              >
                {downloading === file.document_id
                  ? "Verifying evidence…"
                  : `Download ${file.filename}`}
              </Button>
            </li>
          ))}
        </ul>
      </div>
      {downloadError && (
        <p role="alert">
          Could not verify or release the evidence. Refresh access before relying on this finding.
        </p>
      )}
      {self && capabilities.can_review && <p>An independent reviewer must decide this finding.</p>}
      {(canDecide || canWithdraw || canRevoke) && (
        <form
          aria-label="Person finding decision"
          noValidate
          onSubmit={(event) => event.preventDefault()}
          className="space-y-4"
        >
          <fieldset disabled={locked} className="space-y-4">
            <Field
              id="person-decision-reason"
              label="Decision reason"
              error={
                form.formState.errors.reason?.type === "too_small"
                  ? "Enter a reason for this decision."
                  : form.formState.errors.reason?.message
              }
            >
              <Input
                id="person-decision-reason"
                required
                aria-invalid={Boolean(form.formState.errors.reason)}
                aria-describedby={
                  form.formState.errors.reason ? "person-decision-reason-error" : undefined
                }
                maxLength={500}
                {...form.register("reason")}
              />
            </Field>
            {canDecide && (
              <>
                <div>
                  <label className="flex items-start gap-2">
                    <input type="checkbox" {...form.register("identity_examined")} />
                    <span>I independently examined the identity evidence under this policy.</span>
                  </label>
                  {form.formState.errors.identity_examined && (
                    <p role="alert">{form.formState.errors.identity_examined.message}</p>
                  )}
                </div>
                <div>
                  <label className="flex items-start gap-2">
                    <input type="checkbox" {...form.register("independent_person_confirmed")} />
                    <span>I am independent of the applicant and all contributors.</span>
                  </label>
                  {form.formState.errors.independent_person_confirmed && (
                    <p role="alert">{form.formState.errors.independent_person_confirmed.message}</p>
                  )}
                </div>
              </>
            )}
          </fieldset>
          {mutation.error && (
            <p role="alert">
              {mutation.error instanceof ApiError && mutation.error.status === 400
                ? "The submitted decision was refused. Review the fields before submitting again."
                : "The decision result is unconfirmed. Retry the same decision before starting another action."}
            </p>
          )}
          {submitted ? (
            <Button
              type="button"
              disabled={mutation.isPending}
              onClick={() => mutation.mutate(submitted)}
            >
              {mutation.isPending ? "Recording decision…" : "Retry same decision"}
            </Button>
          ) : (
            <div className="flex flex-wrap justify-end gap-2">
              {canWithdraw && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={actionLocked}
                  onClick={() => decide("withdraw")}
                >
                  Withdraw submission
                </Button>
              )}
              {canRevoke && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={actionLocked}
                  onClick={() => decide("revoke")}
                >
                  Revoke finding
                </Button>
              )}
              {canDecide && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={actionLocked}
                    onClick={() => decide("reject")}
                  >
                    Reject finding
                  </Button>
                  <Button type="button" disabled={actionLocked} onClick={() => decide("approve")}>
                    Approve finding
                  </Button>
                </>
              )}
            </div>
          )}
        </form>
      )}
    </section>
  );
}
