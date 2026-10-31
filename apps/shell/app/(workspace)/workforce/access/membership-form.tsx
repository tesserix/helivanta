"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, Panel, LoadMore, formatDateTime, useZodForm, useLeaveGuard } from "@helivanta/ui";
import {
  ApiError,
  enrollStaffMember,
  staffMembershipInputContract,
  newMembershipCommandId,
  useApiMutation,
  membershipsKey,
  useStaffEnrollmentHistory,
  type StaffMembershipInput,
  type StaffAssignmentScope,
} from "@helivanta/api";

const schema = staffMembershipInputContract.safeExtend({
  acknowledged: z
    .boolean()
    .refine((value) => value, { message: "Acknowledge the membership consequence." }),
});
type Command = { id: string; input: StaffMembershipInput };
export function StaffMembershipForm({ scope }: { scope: StaffAssignmentScope }) {
  const form = useZodForm(schema, { subject: "", reason: "", acknowledged: false });
  const [submitted, setSubmitted] = useState<Command | null>(null);
  const save = useApiMutation(
    (command: Command) => enrollStaffMember(scope, command.id, command.input),
    { suppressErrorToast: true, invalidate: [membershipsKey(scope)] },
  );
  const refused =
    save.error instanceof ApiError && [400, 401, 403, 404, 409].includes(save.error.status);
  useLeaveGuard(
    form.formState.isDirty && !save.isSuccess,
    "staff membership enrollment",
    save.isPending,
  );
  return (
    <Panel className="mb-6 space-y-4">
      <h2 className="text-lg font-semibold">Enroll existing account</h2>
      <p>
        Enroll an existing verified human account as an ordinary member of this organization. This
        grants no business permissions or hospital access. Staff records, reviewed account links and
        access assignments are separate steps.
      </p>
      <p>
        Use first enrollment only for an account with no earlier membership here. For a removed
        account, review membership history and use recovery.
      </p>
      {save.isSuccess ? (
        <>
          <p role="status">
            Membership recorded for {submitted?.input.subject}. Membership ID:{" "}
            {save.data.membership_id}. Receipt: {save.data.id}.
          </p>
          <p>
            Access synchronization may still be pending. This does not activate hospital access or
            verify a Staff person's identity.
          </p>
          <Button
            variant="outline"
            onClick={() => {
              save.reset();
              setSubmitted(null);
              form.reset();
            }}
          >
            Enroll another account
          </Button>
        </>
      ) : (
        <form
          aria-label="Staff membership enrollment"
          noValidate
          className="space-y-4"
          onSubmit={form.handleSubmit(({ subject, reason }) => {
            const command = submitted ?? {
              id: newMembershipCommandId(),
              input: { subject, reason },
            };
            setSubmitted(command);
            save.mutate(command);
          })}
        >
          <div className="grid gap-4 md:grid-cols-2">
            <Field
              id="membership-subject"
              label="Existing account ID"
              error={form.formState.errors.subject?.message}
            >
              <Input
                id="membership-subject"
                autoComplete="off"
                disabled={submitted !== null}
                aria-invalid={!!form.formState.errors.subject}
                aria-describedby={
                  form.formState.errors.subject ? "membership-subject-error" : undefined
                }
                {...form.register("subject")}
              />
            </Field>
            <Field
              id="membership-enrollment-reason"
              label="Enrollment reason"
              error={form.formState.errors.reason?.message}
            >
              <Input
                id="membership-enrollment-reason"
                disabled={submitted !== null}
                aria-invalid={!!form.formState.errors.reason}
                aria-describedby={
                  form.formState.errors.reason ? "membership-enrollment-reason-error" : undefined
                }
                {...form.register("reason")}
              />
            </Field>
          </div>
          <p>
            Use the identity provider’s user ID. An email address or staff record ID is not a
            substitute.
          </p>
          <Field
            id="membership-acknowledged"
            label="I understand this creates ordinary membership only."
            error={form.formState.errors.acknowledged?.message}
          >
            <input
              id="membership-acknowledged"
              type="checkbox"
              disabled={submitted !== null}
              aria-invalid={!!form.formState.errors.acknowledged}
              {...form.register("acknowledged")}
            />
          </Field>
          {save.isError && (
            <p role="alert">
              {refused
                ? `${save.error.message} Refresh and review current membership and enrollment history before continuing.`
                : "The enrollment may have been recorded. Retry the same request to reconcile the result."}
            </p>
          )}
          {refused && (
            <Button variant="outline" type="button" onClick={() => window.location.reload()}>
              Reload workspace
            </Button>
          )}
          <Button type="submit" disabled={save.isPending || refused}>
            {save.isPending
              ? "Enrolling…"
              : save.isError
                ? "Retry same enrollment"
                : "Enroll staff member"}
          </Button>
        </form>
      )}
      <EnrollmentHistory scope={scope} />
    </Panel>
  );
}
function EnrollmentHistory({ scope }: { scope: StaffAssignmentScope }) {
  const history = useStaffEnrollmentHistory(scope);
  return (
    <section aria-label="Enrollment history" className="space-y-3 border-t pt-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-semibold">Enrollment history</h3>
        <Button
          variant="outline"
          disabled={history.isFetching}
          onClick={() => void history.refetch()}
        >
          Refresh enrollment history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading enrollment history…</p>
      ) : history.isError ? (
        <p role="alert">
          Could not verify enrollment history. Refresh before reconciling an uncertain enrollment.
        </p>
      ) : (
        <>
          {history.data.pages.every((page) => page.data.length === 0) && (
            <p>No first-enrollment receipts recorded.</p>
          )}
          <div className="grid gap-3 lg:grid-cols-2">
            {history.data.pages
              .flatMap((page) => page.data)
              .map((row) => (
                <article key={row.id} className="space-y-2 rounded-md border p-4">
                  <p className="break-words font-medium">{row.subject} · Ordinary membership</p>
                  <p className="whitespace-pre-wrap break-words">{row.reason}</p>
                  <p className="break-words text-sm">
                    By {row.actor_subject} · {formatDateTime(row.created_at)}
                  </p>
                  <p className="break-words text-sm">
                    Receipt {row.id} · Membership {row.membership_id}
                  </p>
                  <p className="text-sm">
                    Historical enrollment evidence; current access may have changed.
                  </p>
                </article>
              ))}
          </div>
          <LoadMore
            hasMore={history.hasNextPage}
            isLoading={history.isFetching}
            onClick={() => void history.fetchNextPage({ cancelRefetch: false })}
          />
        </>
      )}
    </section>
  );
}
