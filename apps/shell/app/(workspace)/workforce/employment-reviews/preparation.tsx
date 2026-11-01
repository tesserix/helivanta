"use client";
import { useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, FormSelectField, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  EmploymentReviewSubmissionInputContract,
  newEmploymentReviewId,
  submitEmploymentReview,
  useApiMutation,
  useEmploymentPersonFindings,
  type EmploymentReviewPreparationScope,
  type EmploymentPeriodDraft,
  type EmploymentEvidenceBinding,
  type EmploymentReview,
  type EmploymentReviewSubmissionInput,
} from "@helivanta/api";
import { useStaffAccessCutoff } from "../staff-access-cutoff";
import { employmentReviewContextChanged } from "./employment-review-context";
const shape = EmploymentReviewSubmissionInputContract.shape;
const schema = z.object({
  person_review_id: z.string().min(1, "Select an approved person finding."),
  policy_reference: shape.policy_reference.min(
    1,
    "Enter the hospital employment policy reference.",
  ),
  reason: shape.reason.min(1, "Enter a reason for this submission."),
  evidence_ids: z.array(z.string()).min(1, "Select at least one examined employment file.").max(20),
  terms_examined: shape.terms_examined.refine(
    Boolean,
    "Confirm your examination of these terms and evidence.",
  ),
  known_associations_disclosed: shape.known_associations_disclosed.refine(
    Boolean,
    "Confirm your association disclosure.",
  ),
});
type Props = {
  scope: EmploymentReviewPreparationScope;
  terms: EmploymentPeriodDraft;
  evidence: EmploymentEvidenceBinding[];
  onClose: () => void;
  onSubmitted: (receipt: EmploymentReview) => void;
  onAccessLost: () => void;
};
export function EmploymentReviewPreparation({
  scope,
  terms,
  evidence,
  onClose,
  onSubmitted,
  onAccessLost,
}: Props) {
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const findings = useEmploymentPersonFindings(
    scope,
    { staff_version: terms.staff_version, employment_epoch: terms.employment_epoch },
    cursors.at(-1) ?? null,
  );
  const form = useZodForm(schema, {
    person_review_id: "",
    policy_reference: "",
    reason: "",
    evidence_ids: [],
    terms_examined: false,
    known_associations_disclosed: false,
  });
  const [submitted, setSubmitted] = useState<{
    id: string;
    input: EmploymentReviewSubmissionInput;
  } | null>(null);
  const original = useRef<typeof submitted>(null);
  const mutation = useApiMutation(
    (command: NonNullable<typeof submitted>) =>
      submitEmploymentReview(scope, command.id, command.input),
    {
      suppressErrorToast: true,
      onSuccess: onSubmitted,
      onError: (error) => {
        if (error instanceof ApiError && error.status === 400) {
          original.current = null;
          setSubmitted(null);
        }
      },
    },
  );
  const cutoff = useStaffAccessCutoff(
    [findings.error, mutation.error],
    onAccessLost,
    employmentReviewContextChanged,
  );
  const { confirm } = useLeaveGuard(
    form.formState.isDirty || submitted !== null,
    "employment review submission",
    submitted !== null,
  );
  const options = evidence.filter(
    (file) =>
      file.status === "attached" &&
      file.hospital_id === scope.hospitalId &&
      file.staff_id === scope.staffId &&
      file.period_id === terms.id &&
      file.period_version === terms.version &&
      file.period_revision_id === terms.revision_id &&
      file.staff_version === terms.staff_version &&
      file.employment_epoch === terms.employment_epoch,
  );
  const locked = submitted !== null;
  const termMatches =
    terms.status === "draft" &&
    terms.hospital_id === scope.hospitalId &&
    terms.staff_id === scope.staffId;
  if (cutoff) return <p role="alert">Review access changed. Refresh Staff before continuing.</p>;
  function submit() {
    if (original.current || !termMatches || findings.isFetching || findings.isError) return;
    void form.handleSubmit((values) => {
      if (original.current) return;
      const finding = findings.data?.data.find((row) => row.id === values.person_review_id);
      const selected = options.filter((file) => values.evidence_ids.includes(file.id));
      if (!finding) {
        form.setError("person_review_id", {
          message: "Recheck the approved person finding before submitting.",
        });
        return;
      }
      if (selected.length !== values.evidence_ids.length) {
        form.setError("evidence_ids", { message: "Recheck the exact selected employment files." });
        return;
      }
      const command = {
        id: newEmploymentReviewId(),
        input: {
          command_id: newEmploymentReviewId(),
          staff_version: terms.staff_version,
          employment_epoch: terms.employment_epoch,
          identity_hash: finding.identity_hash,
          period_id: terms.id,
          period_revision_id: terms.revision_id,
          period_version: terms.version,
          person_review_id: finding.id,
          person_review_revision_id: finding.revision_id,
          person_review_version: finding.version,
          evidence: selected
            .map((file) => ({ id: file.id, revision_id: file.revision_id, version: file.version }))
            .sort((a, b) => a.id.localeCompare(b.id)),
          policy_reference: values.policy_reference,
          reason: values.reason,
          terms_examined: values.terms_examined,
          known_associations_disclosed: values.known_associations_disclosed,
        },
      };
      original.current = command;
      setSubmitted(command);
      mutation.mutate(command);
    })();
  }
  return (
    <section aria-label="Prepare employment review" className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold">Submit employment terms for independent review</h4>
          <p className="max-w-2xl text-sm text-muted-foreground">
            Examine the selected draft and employment files. Submission and approval do not activate
            employment or grant access.
          </p>
        </div>
        <Button variant="ghost" disabled={locked} onClick={() => confirm(onClose)}>
          Close preparation
        </Button>
      </div>
      <dl className="grid gap-4 sm:grid-cols-2">
        <div>
          <dt className="text-xs text-muted-foreground">Terms revision</dt>
          <dd>
            {terms.evidence_reference} · Revision {terms.version}
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Engagement</dt>
          <dd>
            {terms.engagement_kind} · {terms.duration}
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Terms</dt>
          <dd className="break-words">{terms.reason}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Effective period</dt>
          <dd>
            {new Date(terms.starts_at).toLocaleString()} —{" "}
            {terms.ends_at ? new Date(terms.ends_at).toLocaleString() : "Ongoing"}
          </dd>
        </div>
        {terms.contracting_party && (
          <div>
            <dt className="text-xs text-muted-foreground">Contracting party</dt>
            <dd>{terms.contracting_party}</dd>
          </div>
        )}
      </dl>
      {!termMatches ? (
        <p role="alert">The selected draft does not match this Staff workspace.</p>
      ) : findings.isPending || findings.isFetching ? (
        <p role="status">Checking current approved person findings…</p>
      ) : findings.isError ? (
        <div role="alert">
          <p>Could not verify approved person findings.</p>
          <Button variant="outline" onClick={() => void findings.refetch()}>
            Recheck person findings
          </Button>
        </div>
      ) : (
        <form
          aria-label="Employment review submission"
          noValidate
          onSubmit={(event) => event.preventDefault()}
          className="space-y-4"
        >
          <fieldset disabled={locked} className="space-y-4">
            <FormSelectField
              control={form.control}
              id="employment-person-finding"
              name="person_review_id"
              label="Approved person finding"
              options={findings.data.data.map((finding) => ({
                value: finding.id,
                label: `${finding.finding_reference} · ${finding.policy_reference}`,
              }))}
            />
            {findings.data.data.length === 0 && (
              <p>No current approved person findings are shown on this page.</p>
            )}
            <div className="flex gap-2">
              {cursors.length > 1 && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    form.setValue("person_review_id", "");
                    setCursors((value) => value.slice(0, -1));
                  }}
                >
                  Previous findings
                </Button>
              )}
              {findings.data.page.next_cursor && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    form.setValue("person_review_id", "");
                    setCursors((value) => [...value, findings.data.page.next_cursor]);
                  }}
                >
                  Next findings
                </Button>
              )}
            </div>
            <fieldset className="space-y-2">
              <legend className="font-medium">Examined employment evidence</legend>
              {options.length === 0 && (
                <p>No current attached files match this exact terms revision.</p>
              )}
              {options.map((file) => (
                <label key={file.id} className="flex items-start gap-2">
                  <input type="checkbox" value={file.id} {...form.register("evidence_ids")} />
                  <span>Select {file.filename}</span>
                </label>
              ))}
              {form.formState.errors.evidence_ids && (
                <p role="alert">{form.formState.errors.evidence_ids.message}</p>
              )}
            </fieldset>
            <Field
              id="employment-submission-policy"
              label="Hospital employment policy"
              error={
                form.formState.errors.policy_reference?.type === "too_small"
                  ? "Enter the hospital employment policy reference."
                  : form.formState.errors.policy_reference?.message
              }
            >
              <Input
                id="employment-submission-policy"
                aria-invalid={Boolean(form.formState.errors.policy_reference)}
                aria-describedby={
                  form.formState.errors.policy_reference
                    ? "employment-submission-policy-error"
                    : undefined
                }
                required
                maxLength={300}
                {...form.register("policy_reference")}
              />
            </Field>
            <Field
              id="employment-submission-reason"
              label="Submission reason"
              error={
                form.formState.errors.reason?.type === "too_small"
                  ? "Enter a reason for this submission."
                  : form.formState.errors.reason?.message
              }
            >
              <Input
                id="employment-submission-reason"
                aria-invalid={Boolean(form.formState.errors.reason)}
                aria-describedby={
                  form.formState.errors.reason ? "employment-submission-reason-error" : undefined
                }
                required
                maxLength={500}
                {...form.register("reason")}
              />
            </Field>
            <div>
              <label className="flex items-start gap-2">
                <input type="checkbox" {...form.register("terms_examined")} />
                <span>I examined these terms and the selected employment evidence.</span>
              </label>
              {form.formState.errors.terms_examined && (
                <p role="alert">{form.formState.errors.terms_examined.message}</p>
              )}
            </div>
            <div>
              <label className="flex items-start gap-2">
                <input type="checkbox" {...form.register("known_associations_disclosed")} />
                <span>I disclosed known associations with the applicant and contributors.</span>
              </label>
              {form.formState.errors.known_associations_disclosed && (
                <p role="alert">{form.formState.errors.known_associations_disclosed.message}</p>
              )}
            </div>
          </fieldset>
          {mutation.error && (
            <p role="alert">
              {mutation.error instanceof ApiError && mutation.error.status === 400
                ? "The submission was refused. Review the fields before submitting again."
                : "The submission result is unconfirmed. Retry the same submission before starting another action."}
            </p>
          )}
          {submitted ? (
            <Button
              type="button"
              disabled={mutation.isPending}
              onClick={() => mutation.mutate(submitted)}
            >
              {mutation.isPending ? "Recording submission…" : "Retry same submission"}
            </Button>
          ) : (
            <Button type="button" onClick={submit}>
              Submit for independent employment review
            </Button>
          )}
        </form>
      )}
    </section>
  );
}
