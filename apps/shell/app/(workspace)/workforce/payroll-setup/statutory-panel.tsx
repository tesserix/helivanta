"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  describeStatutoryRevision,
  formatPaise,
  salaryStructuresPath,
  statutoryDecisionFormSchema,
  statutoryPreviewFormSchema,
  statutoryPreviewPath,
  statutoryRulesPath,
  toStatutoryPreviewInput,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  PT_CADENCES,
  STATUTORY_COMPONENTS,
  STATUTORY_GENDERS,
  STATUTORY_KINDS,
  STATUTORY_RESULT_STATUSES,
  STATUTORY_STATUSES,
  type Hospital,
  type SalaryStructure,
  type StatutoryDecisionForm,
  type StatutoryDecisionInput,
  type StatutoryPreview as StatutoryPreviewResult,
  type StatutoryPreviewInput,
  type StatutoryPeriod,
  type StatutoryRevision,
  type StatutoryRule,
  type StatutoryWithdrawInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Check, Fact, RevisionHistory, structuresKey } from "./payroll-workspace";
import { IncomeTaxPreview } from "./income-tax-preview";
import { StatutoryEditor } from "./statutory-editor";

type Kind = keyof typeof STATUTORY_KINDS;
const kinds = Object.keys(STATUTORY_KINDS) as Kind[];
export const statutoryKey = (hospital: string) => ["workforce", "statutory-rules", hospital];

export function StatutoryPanel({ hospital, writable }: { hospital: Hospital; writable: boolean }) {
  const base = `/workforce/hospitals/${hospital.id}`;
  const { can, subject } = usePermissions();
  const approver = hospital.access_level === "manager" && can("workforce.payroll.approve");
  const [editing, setEditing] = useState<Kind | null>(null);
  const [open, setOpen] = useState<{ rule: StatutoryRule; view: "decide" | "history" } | null>(
    null,
  );
  const [previewing, setPreviewing] = useState<"contributions" | "income_tax" | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const rules = useApiPagedQuery<StatutoryRule>(
    statutoryKey(hospital.id),
    statutoryRulesPath(base),
  );
  const byKind = new Map(rules.items.map((r) => [r.kind, r]));
  const close = () => {
    setEditing(null);
    setOpen(null);
    setPreviewing(null);
    opener.current?.focus();
  };
  const remember = (ev: React.MouseEvent<HTMLButtonElement>) => {
    opener.current = ev.currentTarget;
    setEditing(null);
    setOpen(null);
    setPreviewing(null);
  };
  return (
    <section
      aria-label="Statutory contributions"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Statutory contributions</h3>
          <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
            Provident Fund, ESI, professional tax, labour welfare fund and income-tax rules for this
            hospital. Nothing is pre-loaded: each rate, ceiling and slab is entered from the
            notification you cite, and a second person approves every change before it takes effect.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={rules.isPending || rules.isError}
            onClick={(ev) => {
              remember(ev);
              setPreviewing("contributions");
            }}
          >
            Preview contributions
          </Button>
          <Button
            variant="outline"
            disabled={rules.isPending || rules.isError}
            onClick={(ev) => {
              remember(ev);
              setPreviewing("income_tax");
            }}
          >
            Preview income tax
          </Button>
        </div>
      </div>
      {editing !== null && (
        <StatutoryEditor
          key={editing}
          hospital={hospital.id}
          base={base}
          kind={editing}
          rule={byKind.get(editing)}
          onClose={close}
        />
      )}
      {rules.isPending ? (
        <p role="status" className="p-5">
          Loading statutory rules…
        </p>
      ) : rules.isError ? (
        <div className="p-5">
          <Failure message="Could not load statutory rules." retry={rules.retry} />
        </div>
      ) : (
        <ul className="divide-y">
          {kinds.map((kind) => {
            const rule = byKind.get(kind);
            const pending = rule?.pending;
            const mine = !!pending && !!subject && pending.proposed_by_subject === subject;
            return (
              <li key={kind} className="flex flex-wrap items-start justify-between gap-4 p-5">
                <div className="min-w-0">
                  <p className="font-medium">{STATUTORY_KINDS[kind]}</p>
                  {rule?.current ? (
                    <>
                      <p className="mt-1 text-sm">{describeStatutoryRevision(rule.current)}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {rule.current.name} · v{rule.version} effective {rule.current.effective_on}{" "}
                        · {rule.current.citation}
                      </p>
                    </>
                  ) : (
                    <p className="mt-1 text-sm text-muted-foreground">
                      Not configured. Payroll will not compute {STATUTORY_KINDS[kind]} until a rule
                      is approved.
                    </p>
                  )}
                  {pending && (
                    <p className="mt-2 text-xs">
                      <span className="rounded-full bg-(--hms-amber-tint) px-2 py-0.5 font-medium text-(--hms-amber)">
                        {STATUTORY_STATUSES.pending}
                      </span>{" "}
                      {describeStatutoryRevision(pending)} · effective {pending.effective_on} ·
                      proposed by {mine ? "you" : pending.proposed_by_subject}
                    </p>
                  )}
                </div>
                <div className="flex flex-wrap gap-2">
                  {rule && (
                    <Button
                      variant="outline"
                      aria-label={`History of ${STATUTORY_KINDS[kind]}`}
                      onClick={(ev) => {
                        remember(ev);
                        setOpen({ rule, view: "history" });
                      }}
                    >
                      History
                    </Button>
                  )}
                  {rule && pending && ((approver && !mine) || (writable && mine)) && (
                    <Button
                      variant="outline"
                      aria-label={`${mine ? "Withdraw" : "Review"} ${STATUTORY_KINDS[kind]} proposal`}
                      onClick={(ev) => {
                        remember(ev);
                        setOpen({ rule, view: "decide" });
                      }}
                    >
                      {mine ? "Withdraw" : "Review proposal"}
                    </Button>
                  )}
                  {writable && !pending && (
                    <Button
                      variant="outline"
                      aria-label={`Propose ${STATUTORY_KINDS[kind]} change`}
                      onClick={(ev) => {
                        remember(ev);
                        setEditing(kind);
                      }}
                    >
                      {rule ? "Propose change" : "Propose rule"}
                    </Button>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
      {open?.view === "decide" && open.rule.pending && (
        <ProposalDecision
          key={open.rule.pending.id}
          hospital={hospital.id}
          base={base}
          rule={open.rule}
          proposal={open.rule.pending}
          mine={open.rule.pending.proposed_by_subject === subject}
          onClose={close}
        />
      )}
      {open?.view === "history" && (
        <RevisionHistory<StatutoryRevision>
          key={open.rule.id}
          label={`${STATUTORY_KINDS[open.rule.kind]} history`}
          queryKey={[...statutoryKey(hospital.id), "history", open.rule.id]}
          path={`${statutoryRulesPath(base)}/${open.rule.id}/history`}
          rowKey={(r) => r.id}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                {STATUTORY_STATUSES[r.status]}
                {r.approved_version !== undefined && ` · v${r.approved_version}`} · effective{" "}
                {r.effective_on}
              </p>
              <p className="mt-1">{describeStatutoryRevision(r)}</p>
              <p className="mt-1 text-muted-foreground">
                {r.citation} · proposed by {r.proposed_by_subject}
                {r.decided_by_subject && ` · decided by ${r.decided_by_subject}`}
                {r.decision_note && ` · “${r.decision_note}”`}
              </p>
            </>
          )}
        />
      )}
      {previewing === "contributions" && (
        <StatutoryPreview hospital={hospital.id} base={base} onClose={close} />
      )}
      {previewing === "income_tax" && <IncomeTaxPreview base={base} onClose={close} />}
    </section>
  );
}

function ProposalDecision({
  hospital,
  base,
  rule,
  proposal,
  mine,
  onClose,
}: {
  hospital: string;
  base: string;
  rule: StatutoryRule;
  proposal: StatutoryRevision;
  mine: boolean;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const path = `${statutoryRulesPath(base)}/${rule.id}`;
  const form = useZodForm(statutoryDecisionFormSchema, {
    decision: mine ? "reject" : "approve",
    note: "",
  });
  const decision = (successToast: string) => ({
    invalidate: [statutoryKey(hospital)],
    successToast,
    suppressErrorToast: true,
    onSuccess: onClose,
  });
  const post = (input: StatutoryDecisionInput) =>
    apiFetch<StatutoryRule>(`${path}/decision`, { method: "POST", body: JSON.stringify(input) });
  const approve = useApiMutation(post, decision("Proposal approved"));
  const reject = useApiMutation(post, decision("Proposal rejected"));
  const withdraw = useApiMutation(
    (input: StatutoryWithdrawInput) =>
      apiFetch<StatutoryRule>(`${path}/withdraw`, { method: "POST", body: JSON.stringify(input) }),
    {
      invalidate: [statutoryKey(hospital)],
      successToast: "Proposal withdrawn",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const pending = approve.isPending || reject.isPending || withdraw.isPending;
  const failure = approve.error ?? reject.error ?? withdraw.error;
  const errors = form.formState.errors;
  const submit = (decision: StatutoryDecisionForm["decision"]) =>
    form.handleSubmit((values) =>
      (decision === "approve" ? approve : reject).mutate({
        proposal_id: proposal.id,
        expected_version: rule.version,
        decision,
        note: values.note.trim(),
      }),
    );
  return (
    <div role="region" aria-label="Proposal decision" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {mine ? "Your proposal" : "Review proposal"} · {STATUTORY_KINDS[rule.kind]}
        </h4>
        <Button variant="ghost" disabled={pending} onClick={onClose}>
          Close
        </Button>
      </div>
      <dl className="mb-4 grid gap-4 text-sm sm:grid-cols-3">
        <Fact label="Proposed" value={describeStatutoryRevision(proposal)} />
        <Fact label="Effective" value={proposal.effective_on} />
        <Fact label="Relied on" value={proposal.citation} />
        <Fact
          label="In force now"
          value={rule.current ? describeStatutoryRevision(rule.current) : "Nothing approved yet"}
        />
        {proposal.reference && <Fact label="Reference" value={proposal.reference} />}
        <Fact label="Proposed by" value={mine ? "You" : proposal.proposed_by_subject} />
      </dl>
      <form
        noValidate
        autoComplete="off"
        aria-label={mine ? "Withdraw proposal" : "Decide proposal"}
        onSubmit={(ev) => {
          ev.preventDefault();
          if (mine)
            withdraw.mutate({ proposal_id: proposal.id, note: form.getValues("note").trim() });
          else void submit("approve")(ev);
        }}
        className="space-y-4"
      >
        <Field
          id="statutory-decision-note"
          label={mine ? "Reason for withdrawing (optional)" : "Note (required to reject)"}
          error={errors.note?.message}
        >
          <textarea
            id="statutory-decision-note"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.note}
            disabled={pending}
            {...form.register("note")}
          />
        </Field>
        {failure && (
          <SaveError message={`${failure.message} Close and reload to see the current rule.`} />
        )}
        <div className="flex justify-end gap-2">
          {mine ? (
            <Button type="submit" variant="outline" disabled={pending}>
              {withdraw.isPending ? "Withdrawing…" : "Withdraw proposal"}
            </Button>
          ) : (
            <>
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={(ev) => {
                  form.setValue("decision", "reject");
                  void submit("reject")(ev);
                }}
              >
                {reject.isPending ? "Rejecting…" : "Reject proposal"}
              </Button>
              <Button
                type="submit"
                disabled={pending}
                onClick={() => form.setValue("decision", "approve")}
              >
                {approve.isPending ? "Approving…" : "Approve proposal"}
              </Button>
            </>
          )}
        </div>
      </form>
    </div>
  );
}

function StatutoryPreview({
  hospital,
  base,
  onClose,
}: {
  hospital: string;
  base: string;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const structures = useApiPagedQuery<SalaryStructure>(
    structuresKey(hospital),
    salaryStructuresPath(base),
  );
  const form = useZodForm(statutoryPreviewFormSchema, {
    structure_id: "",
    on: today(),
    payable_days: "",
    disability: false,
    gender: "",
  });
  const preview = useApiMutation(
    (input: StatutoryPreviewInput) =>
      apiFetch<StatutoryPreviewResult>(statutoryPreviewPath(base), {
        method: "POST",
        body: JSON.stringify(input),
      }),
    { suppressErrorToast: true },
  );
  const errors = form.formState.errors;
  const value = form.watch();
  const result = preview.data;
  return (
    <div role="region" aria-label="Statutory preview" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          Preview contributions
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close preview
        </Button>
      </div>
      {structures.isError ? (
        <Failure message="Could not load salary structures." retry={structures.retry} />
      ) : (
        <form
          noValidate
          autoComplete="off"
          aria-label="Statutory preview inputs"
          onSubmit={form.handleSubmit((input) => preview.mutate(toStatutoryPreviewInput(input)))}
          className="grid items-end gap-4 sm:grid-cols-[2fr_1fr_1fr_auto]"
        >
          <Field
            id="statutory-preview-structure"
            label="Salary structure"
            error={errors.structure_id?.message}
          >
            <SelectControl
              id="statutory-preview-structure"
              value={value.structure_id}
              aria-invalid={!!errors.structure_id}
              placeholder={structures.isPending ? "Loading…" : "Choose a structure"}
              onValueChange={(next) =>
                form.setValue("structure_id", next, {
                  shouldDirty: true,
                  shouldValidate: form.formState.isSubmitted,
                })
              }
              options={structures.items.map((s) => ({
                value: s.id,
                label: `${s.name} · ${s.code}`,
              }))}
            />
          </Field>
          <Field id="statutory-preview-on" label="Month containing" error={errors.on?.message}>
            <input
              id="statutory-preview-on"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.on}
              {...form.register("on")}
            />
          </Field>
          <Field
            id="statutory-preview-days"
            label="Payable days"
            error={errors.payable_days?.message}
          >
            <input
              id="statutory-preview-days"
              inputMode="decimal"
              className={inputClass}
              aria-invalid={!!errors.payable_days}
              {...form.register("payable_days")}
            />
          </Field>
          <Button type="submit" disabled={preview.isPending}>
            {preview.isPending ? "Computing…" : "Compute contributions"}
          </Button>
          <Field id="statutory-preview-gender" label="Gender (professional tax)">
            <SelectControl
              id="statutory-preview-gender"
              value={value.gender}
              onValueChange={(next) =>
                form.setValue("gender", next as typeof value.gender, { shouldDirty: true })
              }
              options={[
                { value: "", label: "Not stated" },
                ...Object.entries(STATUTORY_GENDERS).map(([g, label]) => ({ value: g, label })),
              ]}
            />
          </Field>
          <div className="self-center sm:col-span-3">
            <Check
              label="Employee is a person with disability (uses the higher ESI ceiling)"
              checked={value.disability}
              onChange={(on) => form.setValue("disability", on, { shouldDirty: true })}
            />
          </div>
        </form>
      )}
      {structures.hasMore && (
        <Button
          className="mt-3"
          variant="outline"
          disabled={structures.isFetchingMore}
          onClick={structures.loadMore}
        >
          Load more salary structures
        </Button>
      )}
      <p className="mt-2 text-xs text-muted-foreground">
        Uses the salary and statutory revisions in force on that date. Nothing is stored. Whether
        ESI continues through a contribution period is decided when payroll runs. Professional tax
        needs a gender only where the state&apos;s slabs differ by gender.
      </p>
      {preview.isError && <SaveError message={preview.error.message} />}
      {result && (
        <div className="mt-5 space-y-4">
          {result.results.map((r) => (
            <div key={r.kind} className="rounded-xl border p-4 text-sm">
              <p className="font-medium">
                {STATUTORY_KINDS[r.kind]} · {STATUTORY_RESULT_STATUSES[r.status]}
              </p>
              {r.status === "applied" && (
                <table className="mt-3 w-full">
                  <caption className="sr-only">
                    {STATUTORY_KINDS[r.kind]} on {formatPaise(r.base_paise)}
                  </caption>
                  <thead>
                    <tr className="border-b text-left text-xs text-muted-foreground">
                      <th className="py-2 font-medium">Component</th>
                      <th className="py-2 font-medium">Paid by</th>
                      <th className="py-2 text-right font-medium">Amount</th>
                    </tr>
                  </thead>
                  <tbody>
                    {r.components.map((c) => (
                      <tr key={c.component} className="border-b last:border-0">
                        <td className="py-2">{STATUTORY_COMPONENTS[c.component]}</td>
                        <td className="py-2">{c.party === "employee" ? "Employee" : "Employer"}</td>
                        <td className="py-2 text-right tabular-nums">
                          {formatPaise(c.amount_paise)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              {r.status === "gender_required" && (
                <p className="mt-2 text-xs text-muted-foreground">
                  The amount at these wages differs by gender. Choose a gender and compute again.
                </p>
              )}
              <p className="mt-2 text-xs text-muted-foreground">
                Wages {formatPaise(r.wages_paise)} · contribution base {formatPaise(r.base_paise)}
              </p>
              {r.period && <PeriodLine period={r.period} />}
            </div>
          ))}
          <dl className="grid gap-4 text-sm sm:grid-cols-3">
            <Fact label="Employee contributions" value={formatPaise(result.employee_paise)} />
            <Fact label="Employer contributions" value={formatPaise(result.employer_paise)} />
            <Fact
              label="Net after contributions"
              value={formatPaise(result.net_after_statutory_paise)}
            />
          </dl>
        </div>
      )}
    </div>
  );
}

// A periodic levy is matched on the period's wages; the basis says whether they were paid or projected.
export function PeriodLine({ period }: { period: StatutoryPeriod }) {
  return (
    <p className="mt-1 text-xs text-muted-foreground">
      {PT_CADENCES[period.cadence]} period {period.starts_on} to {period.ends_on} · wages{" "}
      {period.basis === "projected" ? "projected" : "paid"} at {formatPaise(period.wages_paise)}{" "}
      over {period.months} months
    </p>
  );
}
