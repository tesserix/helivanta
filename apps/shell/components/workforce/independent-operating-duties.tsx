"use client";
import { useId, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  Panel,
  RequestFailure,
  RefreshButton,
  useZodForm,
  useLeaveGuard,
} from "@helivanta/ui";
import {
  ApiError,
  usePermissions,
  useApiMutation,
  useOperatingDuties,
  operatingDutiesKey,
  decideOperatingDuty,
  recoverOperatingDutyCommand,
  newOperatingDutyId,
  OperatingDutyDecisionInputContract,
  operatingDutyTemplates,
  type OperatingDutyStaffScope,
  type OperatingGovernanceStaffChoice,
  type OperatingDuty,
  type OperatingDutyDecisionInput,
} from "@helivanta/api";
import { OperatingDutyPreparation } from "./operating-duty-preparation";

type Props = {
  scope: OperatingDutyStaffScope;
  staffVersion: number;
  staffActive: boolean;
  reviewedAccount?: OperatingGovernanceStaffChoice;
  accessSuspended?: boolean;
  onBusyChange?: (busy: boolean) => void;
};
export function IndependentOperatingDuties(props: Props) {
  const { can } = usePermissions();
  if (
    !can(
      props.scope.administration === "governance"
        ? "iam.governance.operating.manage"
        : "iam.operating.read",
    ) &&
    !props.accessSuspended
  )
    return null;
  return <Duties key={operatingDutiesKey(props.scope).join(":")} {...props} />;
}
function Duties({
  scope,
  staffVersion,
  staffActive,
  reviewedAccount,
  accessSuspended,
  onBusyChange,
}: Props) {
  const { can } = usePermissions();
  const [cursor, setCursor] = useState<string | null>(null);
  const [busy, updateBusy] = useState(false);
  const setBusy = (value: boolean) => {
    updateBusy(value);
    onBusyChange?.(value);
  };
  const native = scope.administration === "governance";
  const permitted = (legacy: string, governance: string) =>
    !accessSuspended && can(native ? governance : legacy);
  const [preparing, setPreparing] = useState(false);
  const query = useOperatingDuties(scope, cursor, !busy && !accessSuspended);
  if (accessSuspended && !busy)
    return (
      <p role="alert">
        Current duty access is unavailable. Restore and refresh the original Staff context.
      </p>
    );
  if (query.isError && !busy)
    return (
      <RequestFailure
        message="Current operating duties could not be verified."
        error={query.error}
        retry={query.refetch}
      />
    );
  if (!query.data && !busy) return <p role="status">Loading operating duties…</p>;
  return (
    <Panel className="mt-6 space-y-5">
      <div>
        <h3 className="font-semibold">Operating duties</h3>
        <p className="text-sm text-muted-foreground">
          Assign a declared operating role to an independently reviewed Staff account at an exact
          location. Hospital collaboration and trading approvals are separate.
        </p>
      </div>
      <RefreshButton busy={busy} refresh={query.refetch} error={query.error}>
        Refresh duties
      </RefreshButton>
      {(query.data?.data ?? []).length === 0 && <p>No operating duties on this page.</p>}
      {(query.data?.data ?? []).map((row) => (
        <Duty
          key={row.id}
          {...{ scope, row, busy, setBusy, accessSuspended }}
          canReview={permitted("iam.operating.review", "iam.governance.operating.review")}
          canRevoke={permitted("iam.operating.revoke", "iam.governance.operating.manage")}
          canPrepare={permitted("iam.operating.prepare", "iam.governance.operating.manage")}
        />
      ))}
      <nav aria-label="Operating duty pages" className="flex gap-3">
        {cursor && (
          <Button variant="outline" disabled={busy} onClick={() => setCursor(null)}>
            First duties page
          </Button>
        )}
        {query.data?.page.has_more && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => setCursor(query.data.page.next_cursor)}
          >
            Next duties page
          </Button>
        )}
      </nav>
      {staffActive &&
        (preparing || permitted("iam.operating.prepare", "iam.governance.operating.manage")) &&
        (!preparing ? (
          <Button disabled={busy} onClick={() => setPreparing(true)}>
            Assign operating duty
          </Button>
        ) : (
          <OperatingDutyPreparation
            {...{ scope, staffVersion, busy, setBusy, reviewedAccount, accessSuspended }}
            onClose={() => setPreparing(false)}
          />
        ))}
    </Panel>
  );
}
function Duty({
  scope,
  row,
  busy,
  setBusy,
  canReview,
  canRevoke,
  canPrepare,
  accessSuspended,
}: {
  scope: OperatingDutyStaffScope;
  row: OperatingDuty;
  busy: boolean;
  setBusy: (v: boolean) => void;
  canReview: boolean;
  canRevoke: boolean;
  canPrepare: boolean;
  accessSuspended?: boolean;
}) {
  const fieldId = useId();
  const [pending, setPending] = useState<{ id: string; input: OperatingDutyDecisionInput } | null>(
    null,
  );
  const [notice, setNotice] = useState("");
  const form = useZodForm(OperatingDutyDecisionInputContract, {
    expected_version: row.version,
    decision: "approve",
    reason: "",
  });
  useLeaveGuard(!!pending || form.formState.isDirty, "Operating duty decision", !!pending);
  const success = (value: { input: OperatingDutyDecisionInput }) => {
    setPending(null);
    setBusy(false);
    form.reset();
    setNotice(
      value.input.decision === "approve"
        ? "Approval recorded. Waiting for verified activation."
        : "Duty decision recorded.",
    );
  };
  const save = useApiMutation(
    (v: NonNullable<typeof pending>) => decideOperatingDuty(scope, row.id, v.id, v.input),
    {
      onSuccess: success,
      onError: (error) => {
        if (error instanceof ApiError && [400, 401, 403, 404, 409, 422].includes(error.status)) {
          setPending(null);
          setBusy(false);
        }
      },
      invalidate: [operatingDutiesKey(scope)],
      suppressErrorToast: true,
    },
  );
  const recover = useApiMutation(
    (v: NonNullable<typeof pending>) => recoverOperatingDutyCommand(scope, row.id, v.id, v.input),
    { onSuccess: success, invalidate: [operatingDutiesKey(scope)], suppressErrorToast: true },
  );
  const independent = scope.subject !== row.prepared_by && scope.subject !== row.input.subject;
  const actions: OperatingDutyDecisionInput["decision"][] = [];
  if (row.status === "pending_review") {
    if (canReview && independent) actions.push("approve", "reject");
    if (canPrepare && scope.subject === row.prepared_by) actions.push("withdraw");
  }
  if (canRevoke && ["active", "approved_pending_activation", "pending_review"].includes(row.status))
    actions.push("revoke");
  const names = {
    approve: "Approve duty",
    reject: "Reject duty",
    withdraw: "Withdraw duty",
    revoke: "Revoke duty",
  };
  function submit(decision: OperatingDutyDecisionInput["decision"]) {
    form.setValue("decision", decision);
    form.setValue("expected_version", row.version);
    void form.handleSubmit((input) => {
      const attempt = { id: newOperatingDutyId(), input };
      setPending(attempt);
      setBusy(true);
      setNotice("");
      save.mutate(attempt);
    })();
  }
  const accessError = recover.error ?? save.error;
  if (
    accessSuspended ||
    (accessError instanceof ApiError && [401, 403, 404].includes(accessError.status))
  )
    return (
      <RequestFailure
        message="This duty is unavailable."
        recheck={pending ? "Recover duty decision" : undefined}
        busy={save.isPending || recover.isPending}
        error={accessError}
        retry={() => {
          if (pending) recover.mutate(pending);
        }}
      />
    );
  return (
    <Panel as="article" className="space-y-3">
      <h4 className="font-medium">
        {operatingDutyTemplates.find((t) => t.key === row.input.template_key)?.label ??
          row.input.template_key}
      </h4>
      <p>{row.input.reason}</p>
      <p>Status: {row.status.replaceAll("_", " ")}</p>
      <dl className="text-sm">
        <dt>Account</dt>
        <dd>{row.input.subject}</dd>
        <dt>Source location</dt>
        <dd>{row.input.scope.location_id}</dd>
        <dt>Validity (UTC)</dt>
        <dd>
          {row.input.valid_from} – {row.input.expires_at}
        </dd>
      </dl>
      {notice && <p role="status">{notice}</p>}
      {(actions.length > 0 || pending) && (
        <>
          <Field id={fieldId} label="Decision reason" error={form.formState.errors.reason?.message}>
            <Input id={fieldId} {...form.register("reason")} disabled={busy || !!pending} />
          </Field>
          {pending ? (
            <div className="space-y-3">
              <p role="status">
                The decision result is unconfirmed. Recover this exact decision before continuing.
              </p>
              <Button
                disabled={save.isPending || recover.isPending}
                onClick={() => recover.mutate(pending)}
              >
                Recover duty decision
              </Button>
            </div>
          ) : (
            <div className="flex flex-wrap gap-3">
              {actions.map((action) => (
                <Button
                  key={action}
                  disabled={busy}
                  variant={action === "approve" ? "default" : "outline"}
                  onClick={() => submit(action)}
                >
                  {names[action]}
                </Button>
              ))}
            </div>
          )}
          {(save.error || recover.error) && (
            <p role="alert">
              {(recover.error ?? save.error)?.message ??
                "The duty decision could not be confirmed."}
            </p>
          )}
        </>
      )}
    </Panel>
  );
}
