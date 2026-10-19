"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  benefitEnrollmentsKey,
  benefitProposalFormSchema,
  proposeBenefitCoverage,
  useApiMutation,
  type BenefitEnrollment,
  type BenefitPlan,
  type BenefitProposalForm,
  type BenefitScope,
  type Dependent,
  type Staff,
  type UseApiPagedQueryResult,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { personLabel, saveFailure } from "./benefits-ui";

const EMPLOYEE = "employee";
const optionalDate = { setValueAs: (value: string) => value || undefined };

// A dependent is coverable only while active and only under a plan that covers their relationship.
const covers = (plan: BenefitPlan | undefined, dependent: Dependent) =>
  dependent.status === "active" &&
  !!plan?.covers_dependents &&
  plan.covered_relationships.includes(dependent.relationship_master_id);

export function CoverageProposal({
  scope,
  member,
  plans,
  dependents,
  record,
  title,
  onClose,
}: {
  scope: BenefitScope;
  member: Staff;
  plans: UseApiPagedQueryResult<BenefitPlan>;
  dependents: UseApiPagedQueryResult<Dependent>;
  record?: BenefitEnrollment;
  title?: string;
  onClose: () => void;
}) {
  // One enrollment while the form is open, and one proposal id per distinct input: a retry replays, an edit proposes anew.
  const [enrollment] = useState(() => record?.id ?? crypto.randomUUID());
  const command = useRef<{ id: string; input: string } | null>(null);
  const form = useZodForm(benefitProposalFormSchema, {
    expected_version: record?.coverage?.version ?? 0,
    plan_id: record?.plan_id ?? "",
    dependent_id: record?.dependent_id,
    coverage_from: record?.coverage?.coverage_from ?? "",
    coverage_until: record?.coverage?.coverage_until,
    eligibility_basis: "",
  });
  const save = useApiMutation(
    (input: BenefitProposalForm) => {
      const key = JSON.stringify(input);
      if (command.current?.input !== key) command.current = { id: crypto.randomUUID(), input: key };
      return proposeBenefitCoverage(scope, enrollment, command.current.id, input);
    },
    {
      invalidate: [benefitEnrollmentsKey(scope)],
      successToast: "Coverage proposed",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => {
    if (record) form.setFocus("coverage_from");
  }, [form, record]);
  const errors = form.formState.errors;
  const value = form.watch();
  const plan = plans.items.find((p) => p.id === value.plan_id);
  const set = <K extends "plan_id" | "dependent_id">(name: K, next: BenefitProposalForm[K]) =>
    form.setValue(name, next as never, {
      shouldDirty: true,
      shouldValidate: form.formState.isSubmitted,
    });
  const choosePlan = (id: string) => {
    set("plan_id", id);
    const dependent = dependents.items.find((d) => d.id === value.dependent_id);
    if (
      dependent &&
      !covers(
        plans.items.find((p) => p.id === id),
        dependent,
      )
    )
      set("dependent_id", undefined);
  };
  const people = [
    { value: EMPLOYEE, label: personLabel(member) },
    ...dependents.items
      .filter((d) => covers(plan, d))
      .map((d) => ({ value: d.id, label: personLabel(member, d) })),
  ];
  const date = (name: "coverage_from" | "coverage_until", label: string) => (
    <Field id={`coverage-${name}`} label={label} error={errors[name]?.message}>
      <input
        id={`coverage-${name}`}
        type="date"
        className={inputClass}
        aria-invalid={!!errors[name]}
        {...form.register(name, name === "coverage_until" ? optionalDate : {})}
      />
    </Field>
  );
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={record ? `Change coverage for ${title}` : "Add coverage"}
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="space-y-6 border-b bg-muted/10 p-5"
    >
      {record && (
        <p className="text-sm">
          {title}. The plan and person stay as approved; propose the new dates.
        </p>
      )}
      <fieldset disabled={save.isPending} className="grid gap-5 sm:grid-cols-2">
        {!record && (
          <>
            <Field id="coverage-plan" label="Plan" error={errors.plan_id?.message}>
              <SelectControl
                id="coverage-plan"
                value={value.plan_id}
                placeholder="Choose a plan"
                onValueChange={choosePlan}
                options={plans.items
                  .filter((p) => p.status === "active")
                  .map((p) => ({ value: p.id, label: p.name }))}
              />
            </Field>
            <Field id="coverage-person" label="Covers" error={errors.dependent_id?.message}>
              <SelectControl
                id="coverage-person"
                value={value.dependent_id ?? EMPLOYEE}
                onValueChange={(next) => set("dependent_id", next === EMPLOYEE ? undefined : next)}
                options={people}
              />
            </Field>
            {(plans.hasMore || dependents.hasMore) && (
              <div className="flex gap-2 sm:col-span-2">
                {plans.hasMore && (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={plans.isFetchingMore}
                    onClick={plans.loadMore}
                  >
                    Load more plans
                  </Button>
                )}
                {dependents.hasMore && (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={dependents.isFetchingMore}
                    onClick={dependents.loadMore}
                  >
                    Load more dependents
                  </Button>
                )}
              </div>
            )}
          </>
        )}
        {date("coverage_from", "Coverage from")}
        {date("coverage_until", "Coverage until")}
        <div className="sm:col-span-2">
          <Field
            id="coverage-basis"
            label="What the approver should check"
            error={errors.eligibility_basis?.message}
          >
            <textarea
              id="coverage-basis"
              rows={2}
              maxLength={500}
              className={inputClass}
              aria-invalid={!!errors.eligibility_basis}
              {...form.register("eligibility_basis")}
            />
          </Field>
        </div>
      </fieldset>
      {save.error && <SaveError message={saveFailure(save.error, "enrollment")} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Proposing…" : "Propose coverage"}
        </Button>
      </div>
    </form>
  );
}
