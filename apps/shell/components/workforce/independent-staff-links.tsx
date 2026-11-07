"use client";
import { useId, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  IndependentLinkInputContract,
  IndependentLinkDecisionInputContract,
  proposeIndependentStaffLink,
  recoverIndependentStaffProposal,
  decideIndependentStaffLink,
  recoverIndependentStaffCommand,
  newIndependentStaffLinkId,
  independentStaffLinksKey,
  useIndependentStaffLinksPage,
  useApiMutation,
  type IndependentStaffScope,
  type IndependentLinkInput,
  type IndependentLinkDecisionInput,
  type IndependentLinkView,
} from "@helivanta/api";
import { Field, Panel, RequestFailure, useZodForm } from "@helivanta/ui";

type Props = {
  scope: IndependentStaffScope;
  staffVersion: number;
  canManage: boolean;
  canReview: boolean;
};
export function IndependentStaffLinks(props: Props) {
  const { scope } = props;
  return (
    <Links
      key={`${scope.tenantId}:${scope.subject}:${scope.employerKind}:${scope.employerId}:${scope.staffId}:${props.staffVersion}`}
      {...props}
    />
  );
}
function Links({ scope, staffVersion, canManage, canReview }: Props) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useIndependentStaffLinksPage(scope, cursor);
  if (query.isPending) return <p role="status">Loading reviewed accounts…</p>;
  if (query.isError)
    return (
      <RequestFailure
        message="Could not verify current Staff account-link access."
        retry={query.refetch}
        error={query.error}
      />
    );
  return (
    <LinkPage
      {...{ scope, staffVersion, canManage, canReview, cursor, setCursor }}
      result={query.data}
    />
  );
}
function LinkPage({
  scope,
  staffVersion,
  canManage,
  canReview,
  cursor,
  setCursor,
  result,
}: Props & {
  cursor: string | null;
  setCursor: (value: string | null) => void;
  result: NonNullable<ReturnType<typeof useIndependentStaffLinksPage>["data"]>;
}) {
  const [busy, setBusy] = useState(false);
  return (
    <Panel className="mt-6 space-y-5">
      <div>
        <h3 className="font-semibold">Reviewed accounts</h3>
        <p className="text-sm text-muted-foreground">
          Link a verified account to this Staff member. Operating duties are assigned separately.
        </p>
      </div>
      {result.data.length === 0 ? (
        <p>No account links on this page.</p>
      ) : (
        <ul className="space-y-5">
          {result.data.map((row) => (
            <li key={row.link.id}>
              <AccountLink
                {...{ scope, staffVersion, canManage, canReview, row, busy }}
                onPending={setBusy}
              />
            </li>
          ))}
        </ul>
      )}
      {canManage && !cursor && (
        <Propose
          {...{ scope, staffVersion, busy }}
          onPending={setBusy}
          hasLiveLink={result.data.some((row) => ["pending", "active"].includes(row.link.status))}
        />
      )}
      <nav aria-label="Staff account link pages" className="flex gap-3">
        {cursor && (
          <Button variant="outline" disabled={busy} onClick={() => setCursor(null)}>
            First accounts page
          </Button>
        )}
        {result.page.has_more && result.page.next_cursor && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => setCursor(result.page.next_cursor)}
          >
            Next accounts page
          </Button>
        )}
      </nav>
    </Panel>
  );
}
type PendingControl = { busy: boolean; onPending: (pending: boolean) => void };
function Propose({
  scope,
  staffVersion,
  busy,
  onPending,
  hasLiveLink,
}: Pick<Props, "scope" | "staffVersion"> & PendingControl & { hasLiveLink: boolean }) {
  const id = useId();
  const [pending, setPending] = useState<{ id: string; input: IndependentLinkInput } | null>(null);
  const [notice, setNotice] = useState("");
  const form = useZodForm(IndependentLinkInputContract, {
    expected_staff_version: staffVersion,
    subject: "",
    reason: "",
  });
  const success = () => {
    setPending(null);
    onPending(false);
    setNotice("Account proposal recorded. Independent review is required.");
    form.reset();
  };
  const write = useApiMutation(
    (value: NonNullable<typeof pending>) =>
      proposeIndependentStaffLink(scope, value.id, value.input),
    { onSuccess: success, invalidate: [independentStaffLinksKey(scope)], suppressErrorToast: true },
  );
  const recover = useApiMutation(
    async () => {
      if (!pending) throw new Error("No retained account proposal.");
      return recoverIndependentStaffProposal(scope, pending.id, pending.input);
    },
    { onSuccess: success, invalidate: [independentStaffLinksKey(scope)], suppressErrorToast: true },
  );
  if (hasLiveLink && !pending && !notice) return null;
  return (
    <div className="space-y-3">
      {notice && <p role="status">{notice}</p>}
      {(!hasLiveLink || pending) && (
        <form
          noValidate
          onSubmit={form.handleSubmit((input) => {
            const value = { id: newIndependentStaffLinkId(), input };
            setPending(value);
            onPending(true);
            setNotice("");
            write.mutate(value);
          })}
        >
          <fieldset disabled={pending !== null || busy} className="grid gap-4 sm:grid-cols-2">
            <Field
              id={`${id}-account`}
              label="Account ID"
              error={form.formState.errors.subject?.message}
            >
              <Input id={`${id}-account`} {...form.register("subject")} />
            </Field>
            <Field
              id={`${id}-reason`}
              label="Account link reason"
              error={form.formState.errors.reason?.message}
            >
              <Input id={`${id}-reason`} {...form.register("reason")} />
            </Field>
            <Button type="submit">Propose account link</Button>
          </fieldset>
        </form>
      )}
      {pending && (write.isError || recover.isError) && (
        <p role="alert">
          The proposal outcome is unconfirmed. Recover the retained proposal before continuing.
        </p>
      )}
      {pending && !write.isPending && (
        <Button variant="outline" disabled={recover.isPending} onClick={() => recover.mutate()}>
          Recover account proposal
        </Button>
      )}
    </div>
  );
}
function AccountLink({
  scope,
  staffVersion,
  canManage,
  canReview,
  row,
  busy,
  onPending,
}: Props & { row: IndependentLinkView } & PendingControl) {
  const id = useId();
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState<{
    id: string;
    input: IndependentLinkDecisionInput;
  } | null>(null);
  const [notice, setNotice] = useState("");
  const success = () => {
    setPending(null);
    onPending(false);
    setNotice("Account decision recorded.");
    setReason("");
  };
  const write = useApiMutation(
    (value: NonNullable<typeof pending>) =>
      decideIndependentStaffLink(scope, row.link.id, value.id, value.input),
    { onSuccess: success, invalidate: [independentStaffLinksKey(scope)], suppressErrorToast: true },
  );
  const recover = useApiMutation(
    async () => {
      if (!pending) throw new Error("No retained account decision.");
      return recoverIndependentStaffCommand(scope, row.link.id, pending.id, pending.input);
    },
    { onSuccess: success, invalidate: [independentStaffLinksKey(scope)], suppressErrorToast: true },
  );
  const independent =
    scope.subject !== row.link.subject && scope.subject !== row.link.proposed_by_subject;
  const decisions: Array<{ decision: IndependentLinkDecisionInput["decision"]; label: string }> =
    [];
  if (row.link.status === "pending") {
    if (canReview && independent) {
      if (row.staff_version === staffVersion)
        decisions.push({ decision: "approve", label: "Approve account link" });
      decisions.push({ decision: "reject", label: "Reject account link" });
    }
    if (canManage && scope.subject === row.link.proposed_by_subject)
      decisions.push({ decision: "withdraw", label: "Withdraw account proposal" });
  }
  if (canManage && ["pending", "active"].includes(row.link.status))
    decisions.push({ decision: "revoke", label: "Revoke account link" });
  function decide(decision: IndependentLinkDecisionInput["decision"]) {
    const parsed = IndependentLinkDecisionInputContract.safeParse({
      expected_version: row.link.version,
      decision,
      reason,
    });
    if (!parsed.success) return;
    const value = { id: newIndependentStaffLinkId(), input: parsed.data };
    setPending(value);
    onPending(true);
    setNotice("");
    write.mutate(value);
  }
  return (
    <Panel as="div" className="space-y-3">
      <p>
        <span className="font-medium">{row.link.subject}</span> · {row.link.status}
      </p>
      {row.staff_version !== staffVersion && (
        <p>Staff details changed. A new identity review is required before assigning duties.</p>
      )}
      {notice && <p role="status">{notice}</p>}
      {decisions.length > 0 && (
        <fieldset disabled={pending !== null || busy} className="space-y-3">
          <Field id={id} label={`Decision reason for ${row.link.subject}`}>
            <Input id={id} value={reason} onChange={(event) => setReason(event.target.value)} />
          </Field>
          <div className="flex flex-wrap gap-2">
            {decisions.map((action) => (
              <Button
                key={action.decision}
                variant="outline"
                disabled={!reason.trim()}
                onClick={() => decide(action.decision)}
              >
                {action.label}
              </Button>
            ))}
          </div>
        </fieldset>
      )}
      {pending && (write.isError || recover.isError) && (
        <p role="alert">
          The decision outcome is unconfirmed. Recover the retained decision before continuing.
        </p>
      )}
      {pending && !write.isPending && (
        <Button variant="outline" disabled={recover.isPending} onClick={() => recover.mutate()}>
          Recover account decision
        </Button>
      )}
    </Panel>
  );
}
