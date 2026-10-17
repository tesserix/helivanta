"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatPaise,
  salaryStructuresPath,
  staffPayPath,
  statutoryDecisionFormSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  ApiError,
  STATUTORY_STATUSES,
  type Hospital,
  type SalaryAssignment,
  type SalaryAssignmentRevision,
  type SalaryStructure,
  type Staff,
  type StatutoryDecisionForm,
  type StatutoryDecisionInput,
  type StatutoryWithdrawInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Fact, RevisionHistory, structuresKey } from "./payroll-workspace";
import { StaffPayEditor } from "./staff-pay-editor";
import { StaffPayPreview } from "./staff-pay-preview";
import { TaxRegimes } from "./tax-regimes";

export const staffPayKey = (hospital: string, staff: string) => [
  "workforce",
  "staff-pay",
  hospital,
  staff,
];

// Names the structure and each per-person amount from the structure's current lines.
export const describeAssignment = (
  revision: SalaryAssignmentRevision,
  structures: Map<string, SalaryStructure>,
) => {
  const structure = structures.get(revision.structure_id);
  const names = new Map(structure?.lines.map((l) => [l.head_id, l.name]));
  const amounts = revision.amounts.map(
    (a) => `${names.get(a.head_id) ?? "Head"} ${formatPaise(a.amount_paise)}`,
  );
  return `${structure?.name ?? "Structure"}${amounts.length ? ` · ${amounts.join(" · ")}` : " · structure amounts"}`;
};

export function StaffPayPanel({ hospital, writable }: { hospital: Hospital; writable: boolean }) {
  const base = `/workforce/hospitals/${hospital.id}`;
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital.id], `${base}/staff`);
  const [selected, setSelected] = useState("");
  const member = staff.items.find((s) => s.id === selected);
  return (
    <section
      aria-label="Staff pay"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Staff pay</h3>
          <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
            Assign each staff member a structure and, where their pay differs, their own amounts for
            its fixed heads. A second person approves every pay change. The income-tax regime is
            recorded from the employee&apos;s intimation and is never assumed.
          </p>
        </div>
        <div className="w-full sm:w-80">
          <Field id="staff-pay-member" label="Staff member">
            <SelectControl
              id="staff-pay-member"
              value={selected}
              placeholder={staff.isPending ? "Loading…" : "Choose a staff member"}
              onValueChange={setSelected}
              options={staff.items.map((s) => ({
                value: s.id,
                label: `${s.display_name} · ${s.employee_code}${s.employment_status === "inactive" ? " (inactive)" : ""}`,
              }))}
            />
          </Field>
        </div>
      </div>
      {staff.isError ? (
        <div className="p-5">
          <Failure message="Could not load staff." retry={staff.retry} />
        </div>
      ) : member ? (
        <StaffPay
          key={member.id}
          hospital={hospital}
          base={base}
          member={member}
          writable={writable}
        />
      ) : (
        <p className="p-5 text-sm text-muted-foreground">
          Choose a staff member to see their pay and tax-regime elections.
        </p>
      )}
      {staff.hasMore && (
        <div className="px-5 pb-5">
          <Button variant="outline" disabled={staff.isFetchingMore} onClick={staff.loadMore}>
            Load more staff
          </Button>
        </div>
      )}
    </section>
  );
}

function StaffPay({
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
  const approver = hospital.access_level === "manager" && can("workforce.payroll.approve");
  const path = `${staffPayPath(base, member.id)}/salary-assignment`;
  const assignment = useApiQuery<SalaryAssignment>(staffPayKey(hospital.id, member.id), path);
  const structures = useApiPagedQuery<SalaryStructure>(
    structuresKey(hospital.id),
    salaryStructuresPath(base),
  );
  const byId = new Map(structures.items.map((s) => [s.id, s]));
  const [view, setView] = useState<"propose" | "decide" | "history" | "preview" | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const open = (next: typeof view, ev: React.MouseEvent<HTMLButtonElement>) => {
    opener.current = ev.currentTarget;
    setView(next);
  };
  const close = () => {
    setView(null);
    opener.current?.focus();
  };
  const missing = assignment.error instanceof ApiError && assignment.error.status === 404;
  const record = assignment.data;
  const pending = record?.pending;
  const mine = !!pending && !!subject && pending.proposed_by_subject === subject;
  const active = member.employment_status === "active";
  if (assignment.isPending)
    return (
      <p role="status" className="p-5">
        Loading pay…
      </p>
    );
  if (assignment.isError && !missing)
    return (
      <div className="p-5">
        <Failure
          message="Could not load this staff member's pay."
          retry={() => void assignment.refetch()}
        />
      </div>
    );
  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-4 p-5">
        <div className="min-w-0 text-sm">
          {record?.current ? (
            <>
              <p className="font-medium">{describeAssignment(record.current, byId)}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                v{record.version} effective {record.current.effective_on} · {record.current.reason}
              </p>
            </>
          ) : (
            <p className="text-muted-foreground">
              No pay approved yet. Payroll will not pay {member.display_name} until an assignment is
              approved.
            </p>
          )}
          {pending && (
            <p className="mt-2 text-xs">
              <span className="rounded-full bg-amber-100 px-2 py-0.5 font-medium text-amber-900">
                {STATUTORY_STATUSES.pending}
              </span>{" "}
              {describeAssignment(pending, byId)} · effective {pending.effective_on} · proposed by{" "}
              {mine ? "you" : pending.proposed_by_subject}
            </p>
          )}
          {!active && (
            <p className="mt-2 text-xs text-muted-foreground">
              Inactive staff keep their pay history but cannot receive a new proposal.
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {record && (
            <Button variant="outline" onClick={(ev) => open("history", ev)}>
              Pay history
            </Button>
          )}
          {record?.current && (
            <Button variant="outline" onClick={(ev) => open("preview", ev)}>
              Preview pay
            </Button>
          )}
          {pending && ((approver && !mine) || (writable && mine)) && (
            <Button variant="outline" onClick={(ev) => open("decide", ev)}>
              {mine ? "Withdraw" : "Review proposal"}
            </Button>
          )}
          {writable && active && !pending && (
            <Button variant="outline" onClick={(ev) => open("propose", ev)}>
              {record ? "Propose pay change" : "Propose pay"}
            </Button>
          )}
        </div>
      </div>
      {view === "propose" && (
        <StaffPayEditor
          hospital={hospital.id}
          path={path}
          staff={member.id}
          record={record}
          structures={structures}
          onClose={close}
        />
      )}
      {view === "decide" && record && pending && (
        <PayDecision
          key={pending.id}
          hospital={hospital.id}
          staff={member.id}
          path={path}
          record={record}
          proposal={pending}
          mine={mine}
          describe={(r) => describeAssignment(r, byId)}
          onClose={close}
        />
      )}
      {view === "history" && (
        <RevisionHistory<SalaryAssignmentRevision>
          label={`Pay history · ${member.display_name}`}
          queryKey={[...staffPayKey(hospital.id, member.id), "history"]}
          path={`${path}/history`}
          rowKey={(r) => r.id}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                {STATUTORY_STATUSES[r.status]}
                {r.approved_version !== undefined && ` · v${r.approved_version}`} · effective{" "}
                {r.effective_on}
              </p>
              <p className="mt-1">{describeAssignment(r, byId)}</p>
              <p className="mt-1 text-muted-foreground">
                {r.reason} · proposed by {r.proposed_by_subject}
                {r.decided_by_subject && ` · decided by ${r.decided_by_subject}`}
                {r.decision_note && ` · “${r.decision_note}”`}
              </p>
            </>
          )}
        />
      )}
      {view === "preview" && (
        <StaffPayPreview path={staffPayPath(base, member.id)} onClose={close} />
      )}
      <TaxRegimes hospital={hospital.id} base={base} staff={member.id} writable={writable} />
    </>
  );
}

function PayDecision({
  hospital,
  staff,
  path,
  record,
  proposal,
  mine,
  describe,
  onClose,
}: {
  hospital: string;
  staff: string;
  path: string;
  record: SalaryAssignment;
  proposal: SalaryAssignmentRevision;
  mine: boolean;
  describe: (revision: SalaryAssignmentRevision) => string;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useZodForm(statutoryDecisionFormSchema, {
    decision: mine ? "reject" : "approve",
    note: "",
  });
  const settle = (successToast: string) => ({
    invalidate: [staffPayKey(hospital, staff), ["workforce", "salary-assignments", hospital]],
    successToast,
    suppressErrorToast: true,
    onSuccess: onClose,
  });
  const post = (input: StatutoryDecisionInput) =>
    apiFetch<SalaryAssignment>(`${path}/decision`, { method: "POST", body: JSON.stringify(input) });
  const approve = useApiMutation(post, settle("Pay approved"));
  const reject = useApiMutation(post, settle("Pay rejected"));
  const withdraw = useApiMutation(
    (input: StatutoryWithdrawInput) =>
      apiFetch<SalaryAssignment>(`${path}/withdraw`, {
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
  return (
    <div role="region" aria-label="Pay decision" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {mine ? "Your pay proposal" : "Review pay proposal"}
        </h4>
        <Button variant="ghost" disabled={busy} onClick={onClose}>
          Close
        </Button>
      </div>
      <dl className="mb-4 grid gap-4 text-sm sm:grid-cols-3">
        <Fact label="Proposed" value={describe(proposal)} />
        <Fact label="Effective" value={proposal.effective_on} />
        <Fact label="Reason" value={proposal.reason} />
        <Fact
          label="In force now"
          value={record.current ? describe(record.current) : "Nothing approved yet"}
        />
        <Fact label="Proposed by" value={mine ? "You" : proposal.proposed_by_subject} />
      </dl>
      <form
        noValidate
        autoComplete="off"
        aria-label={mine ? "Withdraw pay proposal" : "Decide pay proposal"}
        onSubmit={(ev) => {
          ev.preventDefault();
          if (mine)
            withdraw.mutate({ proposal_id: proposal.id, note: form.getValues("note").trim() });
          else void submit("approve")(ev);
        }}
        className="space-y-4"
      >
        <Field
          id="pay-decision-note"
          label={mine ? "Reason for withdrawing (optional)" : "Note (required to reject)"}
          error={errors.note?.message}
        >
          <textarea
            id="pay-decision-note"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.note}
            disabled={busy}
            {...form.register("note")}
          />
        </Field>
        {failure && (
          <SaveError message={`${failure.message} Close and reload to see the current pay.`} />
        )}
        <div className="flex justify-end gap-2">
          {mine ? (
            <Button type="submit" variant="outline" disabled={busy}>
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
                {reject.isPending ? "Rejecting…" : "Reject pay"}
              </Button>
              <Button
                type="submit"
                disabled={busy}
                onClick={() => form.setValue("decision", "approve")}
              >
                {approve.isPending ? "Approving…" : "Approve pay"}
              </Button>
            </>
          )}
        </div>
      </form>
    </div>
  );
}
