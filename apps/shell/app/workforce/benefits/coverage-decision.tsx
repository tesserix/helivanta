"use client";
import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  benefitDecisionFormSchema,
  benefitEnrollmentsKey,
  benefitWithdrawInputContract,
  decideBenefitCoverage,
  useApiMutation,
  withdrawBenefitCoverage,
  type BenefitDecisionForm,
  type BenefitEnrollment,
  type BenefitProposal,
  type BenefitScope,
  type BenefitWithdrawInput,
} from "@helivanta/api";
import { Field, useZodForm } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Fact } from "../payroll-setup/payroll-workspace";
import { period, saveFailure } from "./benefits-ui";

type Props = {
  scope: BenefitScope;
  enrollment: BenefitEnrollment;
  proposal: BenefitProposal;
  title: string;
  onClose: () => void;
};

const settle = (scope: BenefitScope, successToast: string, onClose: () => void) => ({
  invalidate: [benefitEnrollmentsKey(scope)],
  successToast,
  suppressErrorToast: true,
  onSuccess: onClose,
});

function Facts({
  enrollment,
  proposal,
  title,
  mine,
}: Omit<Props, "scope" | "onClose"> & { mine: boolean }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const current = enrollment.coverage;
  return (
    <>
      <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
        {title}
      </h4>
      <dl className="grid gap-4 text-sm sm:grid-cols-2">
        <Fact
          label="Proposed coverage"
          value={period(proposal.coverage_from, proposal.coverage_until)}
        />
        <Fact
          label="In force now"
          value={
            current ? period(current.coverage_from, current.coverage_until) : "Nothing approved yet"
          }
        />
        <Fact label="Eligibility basis" value={proposal.eligibility_basis} />
        <Fact label="Proposed by" value={mine ? "You" : proposal.proposed_by_subject} />
      </dl>
    </>
  );
}

export function ReviewForm({ scope, enrollment, proposal, title, onClose }: Props) {
  const form = useZodForm(benefitDecisionFormSchema, { decision: "approve", note: "" });
  const decide = (input: BenefitDecisionForm) =>
    decideBenefitCoverage(scope, enrollment.id, proposal.id, {
      expected_version: enrollment.coverage?.version ?? 0,
      ...input,
    });
  const approve = useApiMutation(decide, settle(scope, "Coverage approved", onClose));
  const reject = useApiMutation(decide, settle(scope, "Coverage rejected", onClose));
  const busy = approve.isPending || reject.isPending;
  const failure = approve.error ?? reject.error;
  const submit = (decision: BenefitDecisionForm["decision"]) => {
    form.setValue("decision", decision);
    void form.handleSubmit((values) =>
      (values.decision === "approve" ? approve : reject).mutate(values),
    )();
  };
  return (
    <form
      noValidate
      aria-label="Review coverage proposal"
      className="space-y-4 border-b bg-muted/10 p-5"
      onSubmit={(ev) => {
        ev.preventDefault();
        submit("approve");
      }}
    >
      <Facts enrollment={enrollment} proposal={proposal} title={title} mine={false} />
      <Field
        id="coverage-note"
        label="Note (required to reject)"
        error={form.formState.errors.note?.message}
      >
        <textarea
          id="coverage-note"
          rows={2}
          maxLength={500}
          className={inputClass}
          aria-invalid={!!form.formState.errors.note}
          disabled={busy}
          {...form.register("note")}
        />
      </Field>
      {failure && <SaveError message={saveFailure(failure, "enrollment")} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={busy} onClick={onClose}>
          Close
        </Button>
        <Button type="button" variant="outline" disabled={busy} onClick={() => submit("reject")}>
          {reject.isPending ? "Rejecting…" : "Reject coverage"}
        </Button>
        <Button type="submit" disabled={busy}>
          {approve.isPending ? "Approving…" : "Approve coverage"}
        </Button>
      </div>
    </form>
  );
}

export function WithdrawForm({ scope, enrollment, proposal, title, onClose }: Props) {
  const form = useZodForm(benefitWithdrawInputContract, { note: "" });
  const withdraw = useApiMutation(
    (input: BenefitWithdrawInput) =>
      withdrawBenefitCoverage(scope, enrollment.id, proposal.id, input),
    settle(scope, "Proposal withdrawn", onClose),
  );
  return (
    <form
      noValidate
      aria-label="Withdraw coverage proposal"
      className="space-y-4 border-b bg-muted/10 p-5"
      onSubmit={form.handleSubmit((values) => withdraw.mutate(values))}
    >
      <Facts enrollment={enrollment} proposal={proposal} title={title} mine />
      <Field
        id="coverage-withdraw-note"
        label="Reason for withdrawing (optional)"
        error={form.formState.errors.note?.message}
      >
        <textarea
          id="coverage-withdraw-note"
          rows={2}
          maxLength={500}
          className={inputClass}
          disabled={withdraw.isPending}
          {...form.register("note")}
        />
      </Field>
      {withdraw.error && <SaveError message={saveFailure(withdraw.error, "enrollment")} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={withdraw.isPending} onClick={onClose}>
          Close
        </Button>
        <Button type="submit" variant="outline" disabled={withdraw.isPending}>
          {withdraw.isPending ? "Withdrawing…" : "Withdraw proposal"}
        </Button>
      </div>
    </form>
  );
}
