"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  staffIdentifierRevealFormSchema,
  staffIdentifiersFormSchema,
  staffIdentifiersPath,
  statutoryDecisionFormSchema,
  toStaffIdentifiersInput,
  useApiMutation,
  useApiQuery,
  usePermissions,
  ApiError,
  IDENTIFIER_ACTIONS,
  IDENTIFIER_FIELDS,
  STATUTORY_STATUSES,
  type Hospital,
  type IdentifierField,
  type Staff,
  type StaffIdentifierReveal,
  type StaffIdentifierRevealInput,
  type StaffIdentifierRevision,
  type StaffIdentifiers as StaffIdentifiersRecord,
  type StaffIdentifiersInput,
  type StatutoryDecisionForm,
  type StatutoryDecisionInput,
  type StatutoryWithdrawInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Fact, RevisionHistory } from "./payroll-workspace";

const identifiersKey = (hospital: string, staff: string) => [
  "workforce",
  "staff-identifiers",
  hospital,
  staff,
];

const bankOf = (r: StaffIdentifierRevision) =>
  r.bank_account_masked
    ? `${r.bank_account_masked} · ${r.bank_ifsc} · ${r.bank_holder_name}`
    : "Not recorded";

const maskedOf = (r: StaffIdentifierRevision, field: IdentifierField) =>
  field === "bank_account" ? r.bank_account_masked : field === "pan" ? r.pan_masked : r.uan_masked;

export function StaffIdentifiers({
  hospital,
  base,
  member,
  writable,
}: {
  hospital: Hospital;
  base: string;
  member: Staff;
  writable: boolean;
}) {
  const { can, subject } = usePermissions();
  const manager = hospital.access_level === "manager";
  const approver = manager && can("workforce.payroll.approve");
  const revealer = manager && can("workforce.payroll.identifiers.reveal");
  const path = staffIdentifiersPath(base, member.id);
  const key = identifiersKey(hospital.id, member.id);
  const record = useApiQuery<StaffIdentifiersRecord>(key, path);
  const [view, setView] = useState<"propose" | "decide" | "history" | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const open = (next: typeof view, ev: React.MouseEvent<HTMLButtonElement>) => {
    opener.current = ev.currentTarget;
    setView(next);
  };
  const close = () => {
    setView(null);
    opener.current?.focus();
  };
  const missing = record.error instanceof ApiError && record.error.status === 404;
  const data = missing ? undefined : record.data;
  const pending = data?.pending;
  const mine = pending?.proposed_by_subject === subject;
  const active = member.employment_status === "active";
  return (
    <div role="region" aria-label="Bank and tax identifiers" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h4 className="font-semibold">Bank and tax identifiers</h4>
          <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
            Stored encrypted and shown masked. A second person approves every change, and each view
            of a full value is recorded with its reason.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {data && (
            <Button variant="outline" onClick={(ev) => open("history", ev)}>
              Identifier history
            </Button>
          )}
          {pending && ((approver && !mine) || (writable && mine)) && (
            <Button variant="outline" onClick={(ev) => open("decide", ev)}>
              {mine ? "Withdraw identifiers" : "Review identifiers"}
            </Button>
          )}
          {writable && active && !pending && !record.isPending && !(record.isError && !missing) && (
            <Button variant="outline" onClick={(ev) => open("propose", ev)}>
              {data?.current ? "Propose identifier change" : "Propose identifiers"}
            </Button>
          )}
        </div>
      </div>
      {record.isPending ? (
        <p role="status">Loading identifiers…</p>
      ) : record.isError && !missing ? (
        <Failure
          message="Could not load this staff member's identifiers."
          retry={() => record.refetch()}
        />
      ) : (
        <>
          {data?.current ? (
            <CurrentIdentifiers
              path={path}
              revision={data.current}
              version={data.version}
              revealer={revealer}
            />
          ) : (
            <p className="text-sm text-muted-foreground">
              No bank account, PAN or UAN approved for {member.display_name}.
            </p>
          )}
          {pending && (
            <p className="mt-3 text-xs">
              <span className="rounded-full bg-amber-100 px-2 py-0.5 font-medium text-amber-900">
                {STATUTORY_STATUSES.pending}
              </span>{" "}
              Change proposed by {mine ? "you" : pending.proposed_by_subject} · {pending.reason}
            </p>
          )}
        </>
      )}
      {view === "propose" && (
        <IdentifiersEditor path={path} queryKey={key} record={data} onClose={close} />
      )}
      {view === "decide" && data && pending && (
        <IdentifiersDecision
          key={pending.id}
          path={path}
          queryKey={key}
          record={data}
          proposal={pending}
          mine={mine}
          revealer={revealer}
          onClose={close}
        />
      )}
      {view === "history" && (
        <RevisionHistory<StaffIdentifierRevision>
          label={`Identifier history for ${member.display_name}`}
          queryKey={[...key, "history"]}
          path={`${path}/history`}
          rowKey={(r) => r.id}
          render={(r) => (
            <>
              <p className="font-medium">
                {STATUTORY_STATUSES[r.status]}
                {r.approved_version && ` · v${r.approved_version}`}
              </p>
              <p className="mt-1">
                Bank {bankOf(r)} · PAN {r.pan_masked ?? "not recorded"} · UAN{" "}
                {r.uan_masked ?? "not recorded"}
              </p>
              <p className="mt-1 text-muted-foreground">
                {r.reason} · proposed by {r.proposed_by_subject}
                {r.decided_by_subject && ` · decided by ${r.decided_by_subject}`}
                {r.decision_note && ` · “${r.decision_note}”`}
              </p>
            </>
          )}
          onClose={close}
        />
      )}
    </div>
  );
}

function CurrentIdentifiers({
  path,
  revision,
  version,
  revealer,
}: {
  path: string;
  revision: StaffIdentifierRevision;
  version: number;
  revealer: boolean;
}) {
  return (
    <>
      <dl className="grid gap-4 text-sm sm:grid-cols-3">
        <RevealableFact
          label="Bank account"
          value={bankOf(revision)}
          field="bank_account"
          path={path}
          revision={revision}
          revealer={revealer}
        />
        <RevealableFact
          label="PAN"
          value={revision.pan_masked ?? "Not recorded"}
          field="pan"
          path={path}
          revision={revision}
          revealer={revealer}
        />
        <RevealableFact
          label="UAN"
          value={revision.uan_masked ?? "Not recorded"}
          field="uan"
          path={path}
          revision={revision}
          revealer={revealer}
        />
      </dl>
      <p className="mt-2 text-xs text-muted-foreground">
        v{version} · {revision.reason}
      </p>
    </>
  );
}

function RevealableFact({
  label,
  value,
  field,
  path,
  revision,
  revealer,
}: {
  label: string;
  value: string;
  field: IdentifierField;
  path: string;
  revision: StaffIdentifierRevision;
  revealer: boolean;
}) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{value}</dd>
      {revealer && maskedOf(revision, field) && (
        <Reveal path={path} revision={revision.id} field={field} />
      )}
    </div>
  );
}

// The full value lives only in this component's state; it is never cached and is dropped on hide.
function Reveal({
  path,
  revision,
  field,
}: {
  path: string;
  revision: string;
  field: IdentifierField;
}) {
  const [asking, setAsking] = useState(false);
  const [shown, setShown] = useState<string | null>(null);
  const toggle = useRef<HTMLButtonElement>(null);
  const refocus = useRef(false);
  const label = IDENTIFIER_FIELDS[field];
  const form = useZodForm(staffIdentifierRevealFormSchema, { reason: "" });
  const reveal = useApiMutation(
    (input: StaffIdentifierRevealInput) =>
      apiFetch<StaffIdentifierReveal>(`${path}/reveal`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    {
      suppressErrorToast: true,
      onSuccess: (result) => {
        setShown(result.value);
        setAsking(false);
        form.reset({ reason: "" });
      },
    },
  );
  const hide = () => {
    setShown(null);
    setAsking(false);
    reveal.reset();
    form.reset({ reason: "" });
    refocus.current = true;
  };
  useEffect(() => {
    if (asking) form.setFocus("reason");
    else if (shown === null && refocus.current) toggle.current?.focus();
    refocus.current = false;
  }, [asking, shown, form]);
  const id = `reveal-${revision}-${field}`;
  if (shown !== null)
    return (
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <output aria-label={`Full ${label}`} className="font-mono text-sm">
          {shown}
        </output>
        <Button variant="outline" onClick={hide}>
          Hide {label}
        </Button>
      </div>
    );
  if (!asking)
    return (
      <Button ref={toggle} variant="outline" className="mt-2" onClick={() => setAsking(true)}>
        Show full {label}
      </Button>
    );
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={`Show full ${label}`}
      className="mt-2 space-y-2"
      onSubmit={form.handleSubmit((values) =>
        reveal.mutate({ revision_id: revision, field, reason: values.reason.trim() }),
      )}
    >
      <Field id={id} label="Why you need to see it" error={form.formState.errors.reason?.message}>
        <input
          id={id}
          className={inputClass}
          aria-invalid={!!form.formState.errors.reason}
          disabled={reveal.isPending}
          {...form.register("reason")}
        />
      </Field>
      {reveal.error && <SaveError message={reveal.error.message} />}
      <div className="flex gap-2">
        <Button type="button" variant="outline" disabled={reveal.isPending} onClick={hide}>
          Cancel
        </Button>
        <Button type="submit" disabled={reveal.isPending}>
          {reveal.isPending ? "Opening…" : "Show"}
        </Button>
      </div>
    </form>
  );
}

const actionOptions = (present: boolean) =>
  (Object.keys(IDENTIFIER_ACTIONS) as (keyof typeof IDENTIFIER_ACTIONS)[])
    .filter((a) => present || a !== "clear")
    .map((a) => ({
      value: a,
      label: present || a !== "keep" ? IDENTIFIER_ACTIONS[a] : "Leave unrecorded",
    }));

function IdentifiersEditor({
  path,
  queryKey,
  record,
  onClose,
}: {
  path: string;
  queryKey: unknown[];
  record?: StaffIdentifiersRecord;
  onClose: () => void;
}) {
  const current = record?.current;
  const form = useZodForm(staffIdentifiersFormSchema, {
    reason: "",
    bank: "keep",
    account_number: "",
    ifsc: "",
    holder_name: "",
    pan: "keep",
    pan_value: "",
    uan: "keep",
    uan_value: "",
  });
  const save = useApiMutation(
    (input: StaffIdentifiersInput) =>
      apiFetch<StaffIdentifiersRecord>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [queryKey],
      successToast: "Identifiers proposed",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("reason"), [form]);
  const errors = form.formState.errors;
  const value = form.watch();
  const input = (
    name: "account_number" | "ifsc" | "holder_name" | "pan_value" | "uan_value",
    label: string,
    numeric = false,
  ) => (
    <Field id={`identifiers-${name}`} label={label} error={errors[name]?.message}>
      <input
        id={`identifiers-${name}`}
        className={inputClass}
        inputMode={numeric ? "numeric" : undefined}
        spellCheck={false}
        aria-invalid={!!errors[name]}
        {...form.register(name)}
      />
    </Field>
  );
  const choice = (name: "bank" | "pan" | "uan", label: string, present: boolean) => (
    <Field id={`identifiers-${name}`} label={label} error={errors[name]?.message}>
      <SelectControl
        id={`identifiers-${name}`}
        value={value[name]}
        onValueChange={(v) =>
          form.setValue(name, v as typeof value.bank, {
            shouldValidate: form.formState.isSubmitted,
          })
        }
        options={actionOptions(present)}
      />
    </Field>
  );
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={current ? "Propose identifier change" : "Propose identifiers"}
      onSubmit={form.handleSubmit((values) =>
        save.mutate(toStaffIdentifiersInput(values, record?.version ?? 0)),
      )}
      className="mt-4 space-y-6 border-t bg-muted/10 pt-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-3">
          <div className="space-y-4">
            {choice("bank", "Bank account", !!current?.bank_account_masked)}
            {value.bank === "set" && (
              <>
                {input("account_number", "Account number", true)}
                {input("ifsc", "IFSC")}
                {input("holder_name", "Account holder name")}
              </>
            )}
          </div>
          <div className="space-y-4">
            {choice("pan", "PAN", !!current?.pan_masked)}
            {value.pan === "set" && input("pan_value", "New PAN")}
          </div>
          <div className="space-y-4">
            {choice("uan", "UAN", !!current?.uan_masked)}
            {value.uan === "set" && input("uan_value", "New UAN", true)}
          </div>
        </div>
        <Field id="identifiers-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="identifiers-reason"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
      </fieldset>
      {save.error && (
        <SaveError
          message={`${save.error.message} Close and reload to see the current identifiers.`}
        />
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Proposing…" : "Propose identifiers"}
        </Button>
      </div>
    </form>
  );
}

function IdentifiersDecision({
  path,
  queryKey,
  record,
  proposal,
  mine,
  revealer,
  onClose,
}: {
  path: string;
  queryKey: unknown[];
  record: StaffIdentifiersRecord;
  proposal: StaffIdentifierRevision;
  mine: boolean;
  revealer: boolean;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useZodForm(statutoryDecisionFormSchema, {
    decision: mine ? "reject" : "approve",
    note: "",
  });
  const settle = (successToast: string) => ({
    invalidate: [queryKey],
    successToast,
    suppressErrorToast: true,
    onSuccess: onClose,
  });
  const post = (input: StatutoryDecisionInput) =>
    apiFetch<StaffIdentifiersRecord>(`${path}/decision`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  const approve = useApiMutation(post, settle("Identifiers approved"));
  const reject = useApiMutation(post, settle("Identifiers rejected"));
  const withdraw = useApiMutation(
    (input: StatutoryWithdrawInput) =>
      apiFetch<StaffIdentifiersRecord>(`${path}/withdraw`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    settle("Proposal withdrawn"),
  );
  const busy = approve.isPending || reject.isPending || withdraw.isPending;
  const failure = approve.error ?? reject.error ?? withdraw.error;
  const errors = form.formState.errors;
  const submit = (decision: StatutoryDecisionForm["decision"]) =>
    form.handleSubmit((values) =>
      (decision === "approve" ? approve : reject).mutate({
        proposal_id: proposal.id,
        expected_version: record.version,
        decision,
        note: values.note.trim(),
      }),
    );
  // Only someone other than the proposer may check the proposed values against the source documents.
  const checker = revealer && !mine;
  return (
    <div role="region" aria-label="Identifier decision" className="mt-4 border-t pt-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {mine ? "Withdraw your identifier proposal" : "Review identifier proposal"}
        </h4>
        <Button variant="outline" onClick={onClose}>
          Close
        </Button>
      </div>
      <dl className="mb-4 grid gap-4 text-sm sm:grid-cols-3">
        <RevealableFact
          label="Proposed bank account"
          value={bankOf(proposal)}
          field="bank_account"
          path={path}
          revision={proposal}
          revealer={checker}
        />
        <RevealableFact
          label="Proposed PAN"
          value={proposal.pan_masked ?? "Not recorded"}
          field="pan"
          path={path}
          revision={proposal}
          revealer={checker}
        />
        <RevealableFact
          label="Proposed UAN"
          value={proposal.uan_masked ?? "Not recorded"}
          field="uan"
          path={path}
          revision={proposal}
          revealer={checker}
        />
        <Fact label="Reason" value={proposal.reason} />
        <Fact
          label="In force now"
          value={
            record.current
              ? `Bank ${bankOf(record.current)} · PAN ${record.current.pan_masked ?? "not recorded"} · UAN ${record.current.uan_masked ?? "not recorded"}`
              : "Nothing approved yet"
          }
        />
        <Fact label="Proposed by" value={mine ? "You" : proposal.proposed_by_subject} />
      </dl>
      <form
        noValidate
        aria-label={mine ? "Withdraw identifier proposal" : "Decide identifier proposal"}
        onSubmit={
          mine
            ? (ev) => {
                ev.preventDefault();
                withdraw.mutate({ proposal_id: proposal.id, note: form.getValues("note").trim() });
              }
            : submit("approve")
        }
        className="space-y-4"
      >
        <Field
          id="identifiers-decision-note"
          label={mine ? "Reason for withdrawing (optional)" : "Note (required to reject)"}
          error={errors.note?.message}
        >
          <textarea
            id="identifiers-decision-note"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.note}
            disabled={busy}
            {...form.register("note")}
          />
        </Field>
        {failure && (
          <SaveError
            message={`${failure.message} Close and reload to see the current identifiers.`}
          />
        )}
        <div className="flex justify-end gap-2">
          {mine ? (
            <Button type="submit" disabled={busy}>
              {withdraw.isPending ? "Withdrawing…" : "Withdraw proposal"}
            </Button>
          ) : (
            <>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={(ev) => {
                  form.setValue("decision", "reject");
                  void submit("reject")(ev);
                }}
              >
                {reject.isPending ? "Rejecting…" : "Reject identifiers"}
              </Button>
              <Button
                type="submit"
                disabled={busy}
                onClick={() => form.setValue("decision", "approve")}
              >
                {approve.isPending ? "Approving…" : "Approve identifiers"}
              </Button>
            </>
          )}
        </div>
      </form>
    </div>
  );
}
