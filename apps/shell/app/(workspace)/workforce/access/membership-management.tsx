"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Panel, Field, LoadMore, formatDateTime, useZodForm, useLeaveGuard } from "@helivanta/ui";
import {
  ApiError,
  useApiMutation,
  useMembershipCandidates,
  useMembershipHistory,
  membershipsKey,
  submitMembershipCommand,
  newMembershipCommandId,
  membershipCommandInputContract,
  type MembershipCommandInput,
  type StaffAssignmentScope,
} from "@helivanta/api";

type Action = Omit<MembershipCommandInput, "reason">;
export function MembershipManagement({ scope }: { scope: StaffAssignmentScope }) {
  const candidates = useMembershipCandidates(scope);
  const history = useMembershipHistory(scope);
  const [action, setAction] = useState<Action | null>(null);
  const [notice, setNotice] = useState("");
  return (
    <Panel className="mb-6 space-y-4">
      <h2 className="text-lg font-semibold">Membership removal and recovery</h2>
      <p>
        Remove one organization role at a time. This does not end employment, delete Staff records
        or disable the global login account. Other roles and other organizations remain separate.
      </p>
      <Button
        variant="outline"
        disabled={!!action || candidates.isFetching || history.isFetching}
        onClick={() => {
          void candidates.refetch();
          void history.refetch();
        }}
      >
        Refresh memberships and history
      </Button>
      {notice && <p role="status">{notice}</p>}
      {action && (
        <MembershipCommandForm
          key={`${action.operation}:${action.basis_id}`}
          scope={scope}
          action={action}
          onClose={() => setAction(null)}
          onSaved={(id) => {
            setAction(null);
            setNotice(
              `Membership change recorded. Receipt: ${id}. Access synchronization may still be pending. Review hospital access, Staff account links and scoped assignments separately.`,
            );
          }}
        />
      )}
      {candidates.isPending ? (
        <p role="status">Loading current memberships…</p>
      ) : candidates.isError ? (
        <p role="alert">Could not verify current memberships. Refresh before continuing.</p>
      ) : (
        <>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Current organization memberships</caption>
              <thead>
                <tr>
                  <th className="p-2">Account ID</th>
                  <th className="p-2">Role</th>
                  <th className="p-2">Membership since</th>
                  <th className="p-2">Action</th>
                </tr>
              </thead>
              <tbody>
                {candidates.data.pages
                  .flatMap((page) => page.data)
                  .map((row) => (
                    <tr key={row.id} className="border-t">
                      <td className="max-w-64 break-words p-2">{row.subject}</td>
                      <td className="p-2">{row.role_key}</td>
                      <td className="p-2">{formatDateTime(row.created_at)}</td>
                      <td className="p-2">
                        {row.role_key === "tenant_admin" ? (
                          <span>Separate owner recovery required</span>
                        ) : (
                          <Button
                            variant="outline"
                            disabled={!!action}
                            onClick={() => {
                              setNotice("");
                              setAction({
                                operation: "remove",
                                basis_id: row.id,
                                subject: row.subject,
                                role_key: row.role_key,
                                expected_membership_epoch: row.membership_epoch,
                              });
                            }}
                          >
                            Remove membership
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
          {!candidates.data.pages.some((page) => page.data.length) && (
            <p>No current memberships.</p>
          )}
          <LoadMore
            hasMore={candidates.hasNextPage}
            isLoading={candidates.isFetching}
            onClick={() => void candidates.fetchNextPage({ cancelRefetch: false })}
          />
        </>
      )}
      <h3 className="font-semibold">Membership history</h3>
      <p>
        Recovery verifies the current identity-provider account and creates a fresh ordinary
        membership. Roles, employment, Staff account links and cards have separate review workflows.
      </p>
      {history.isPending ? (
        <p role="status">Loading membership history…</p>
      ) : history.isError ? (
        <p role="alert">Could not verify membership history. Refresh before continuing.</p>
      ) : (
        <>
          {!history.data.pages.some((page) => page.data.length) && (
            <p>No membership commands recorded.</p>
          )}
          {history.data.pages
            .flatMap((page) => page.data)
            .map((row) => (
              <article key={row.id} className="space-y-2 rounded-md border p-4">
                <p className="break-words">
                  <strong>{row.operation === "remove" ? "Removed" : "Recovered"}</strong>:{" "}
                  {row.subject} · {row.role_key}
                </p>
                <p className="whitespace-pre-wrap break-words">{row.reason}</p>
                <p className="break-words text-sm">
                  By {row.actor_subject} · {formatDateTime(row.created_at)} · Receipt {row.id}
                </p>
                {row.recoverable && (
                  <Button
                    variant="outline"
                    disabled={!!action}
                    onClick={() => {
                      setNotice("");
                      setAction({
                        operation: "recover",
                        basis_id: row.id,
                        subject: row.subject,
                        role_key: "staff_member",
                        expected_membership_epoch: row.current_membership_epoch,
                      });
                    }}
                  >
                    Recover ordinary membership
                  </Button>
                )}
              </article>
            ))}
          <LoadMore
            hasMore={history.hasNextPage}
            isLoading={history.isFetching}
            onClick={() => void history.fetchNextPage({ cancelRefetch: false })}
          />
        </>
      )}
    </Panel>
  );
}

const formSchema = z.object({
  reason: z
    .string()
    .trim()
    .min(1, "Enter a reason.")
    .pipe(membershipCommandInputContract.shape.reason),
  reviewed: z.boolean().refine((value) => value, "Review the account, role and consequence."),
});
function MembershipCommandForm({
  scope,
  action,
  onClose,
  onSaved,
}: {
  scope: StaffAssignmentScope;
  action: Action;
  onClose: () => void;
  onSaved: (id: string) => void;
}) {
  const [id] = useState(() => newMembershipCommandId());
  const [submitted, setSubmitted] = useState<MembershipCommandInput | null>(null);
  const form = useZodForm(formSchema, { reason: "", reviewed: false });
  const save = useApiMutation(
    (input: MembershipCommandInput) => submitMembershipCommand(scope, id, input),
    {
      invalidate: [membershipsKey(scope)],
      suppressErrorToast: true,
      onSuccess: (result) => onSaved(result.id),
    },
  );
  const { confirm } = useLeaveGuard(
    !save.isSuccess && (form.formState.isDirty || !!submitted),
    "membership command",
    save.isPending,
  );
  const refused =
    save.error instanceof ApiError && [400, 401, 403, 404, 409].includes(save.error.status);
  return (
    <form
      aria-label={
        action.operation === "remove"
          ? "Remove organization membership"
          : "Recover ordinary membership"
      }
      className="space-y-4 rounded-md border p-4"
      noValidate
      onSubmit={form.handleSubmit(({ reason }) => {
        const command = submitted ?? { ...action, reason };
        setSubmitted(command);
        save.mutate(command);
      })}
    >
      <h3 className="font-semibold">
        {action.operation === "remove"
          ? "Review membership removal"
          : "Review ordinary membership recovery"}
      </h3>
      <p className="break-words">
        Account: {action.subject} · Role: {action.role_key} · Membership generation:{" "}
        {action.expected_membership_epoch}
      </p>
      <p>
        {action.operation === "remove"
          ? "Only the selected membership is removed. If this is the account’s final organization role, its current scoped assignments lose membership continuity. Sign-out and other organizations remain separate."
          : "The identity-provider account must currently be active. This creates a new staff_member membership and grants no hospital, payroll or clinical authority."}
      </p>
      <Field
        id="membership-command-reason"
        label="Reason"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="membership-command-reason"
          autoComplete="off"
          disabled={!!submitted}
          aria-invalid={!!form.formState.errors.reason}
          aria-describedby={
            form.formState.errors.reason ? "membership-command-reason-error" : undefined
          }
          {...form.register("reason")}
        />
      </Field>
      <label className="flex items-start gap-2">
        <input
          type="checkbox"
          disabled={!!submitted}
          aria-invalid={!!form.formState.errors.reviewed}
          aria-describedby={
            form.formState.errors.reviewed ? "membership-command-review-error" : undefined
          }
          {...form.register("reviewed")}
        />
        I reviewed this account, role and consequence.
      </label>
      {form.formState.errors.reviewed && (
        <p id="membership-command-review-error">{form.formState.errors.reviewed.message}</p>
      )}
      {save.isError && (
        <p role="alert">
          {refused
            ? `${save.error.message} Close this form and refresh before continuing.`
            : "The command may have been recorded. Retry the same membership command or check its history before starting another."}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={save.isPending || refused}>
          {save.isPending
            ? "Recording…"
            : submitted
              ? "Retry same membership command"
              : action.operation === "remove"
                ? "Record membership removal"
                : "Record ordinary membership recovery"}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={save.isPending}
          onClick={() => confirm(onClose)}
        >
          Close
        </Button>
      </div>
    </form>
  );
}
