"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  benefitPlanFormSchema,
  benefitPlansKey,
  saveBenefitPlan,
  useApiMutation,
  BENEFIT_PLAN_KINDS,
  type BenefitPlan,
  type BenefitPlanForm,
  type HRMaster,
  type UseApiPagedQueryResult,
} from "@helivanta/api";
import { ConfirmDialog, Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Check } from "../payroll-setup/payroll-workspace";
import { PLAN_STATUSES, saveFailure } from "./benefits-ui";

const kindOptions = Object.entries(BENEFIT_PLAN_KINDS).map(([value, label]) => ({ value, label }));
const statusOptions = Object.entries(PLAN_STATUSES).map(([value, label]) => ({ value, label }));
const optionalDate = { setValueAs: (value: string) => value || undefined };

const toForm = (plan?: BenefitPlan): BenefitPlanForm =>
  plan
    ? {
        expected_version: plan.version,
        code: plan.code,
        name: plan.name,
        kind: plan.kind,
        description: plan.description,
        covers_dependents: plan.covers_dependents,
        covered_relationships: plan.covered_relationships,
        effective_from: plan.effective_from,
        effective_until: plan.effective_until,
        status: plan.status,
      }
    : {
        expected_version: 0,
        code: "",
        name: "",
        kind: "health",
        description: "",
        covers_dependents: false,
        covered_relationships: [],
        effective_from: "",
        status: "active",
      };

export function PlanEditor({
  hospital,
  record,
  relationships,
  onClose,
}: {
  hospital: string;
  record?: BenefitPlan;
  relationships: UseApiPagedQueryResult<HRMaster>;
  onClose: () => void;
}) {
  // One id while the form is open, so a retried create replays instead of adding a second plan.
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const form = useZodForm(benefitPlanFormSchema, toForm(record));
  const [retiring, setRetiring] = useState<BenefitPlanForm | null>(null);
  const settle = (successToast: string) => ({
    invalidate: [benefitPlansKey(hospital)],
    successToast,
    suppressErrorToast: true,
    onSuccess: onClose,
  });
  const save = useApiMutation(
    (input: BenefitPlanForm) => saveBenefitPlan(hospital, id, input),
    settle("Plan saved"),
  );
  const retire = useApiMutation(
    (input: BenefitPlanForm) => saveBenefitPlan(hospital, id, input),
    settle("Plan retired"),
  );
  useEffect(() => form.setFocus(record ? "name" : "code"), [form, record]);
  const errors = form.formState.errors;
  const value = form.watch();
  const busy = save.isPending || retire.isPending;
  const failure = save.error ?? retire.error;
  const set = <K extends keyof BenefitPlanForm>(name: K, next: BenefitPlanForm[K]) =>
    form.setValue(name, next as never, {
      shouldDirty: true,
      shouldValidate: form.formState.isSubmitted,
    });
  // Only active relationships can be added; one the plan already covers stays listed so it can be removed.
  const choices = relationships.items.filter(
    (r) => r.active || record?.covered_relationships.includes(r.id),
  );
  const text = (name: "code" | "name", label: string, disabled?: boolean) => (
    <Field id={`plan-${name}`} label={label} error={errors[name]?.message}>
      <input
        id={`plan-${name}`}
        className={inputClass}
        disabled={disabled}
        aria-invalid={!!errors[name]}
        {...form.register(name)}
      />
    </Field>
  );
  const date = (name: "effective_from" | "effective_until", label: string) => (
    <Field id={`plan-${name}`} label={label} error={errors[name]?.message}>
      <input
        id={`plan-${name}`}
        type="date"
        className={inputClass}
        aria-invalid={!!errors[name]}
        {...form.register(name, name === "effective_until" ? optionalDate : {})}
      />
    </Field>
  );
  return (
    <>
      <form
        noValidate
        autoComplete="off"
        aria-label={record ? `Edit ${record.name}` : "Add plan"}
        onSubmit={form.handleSubmit((input) =>
          input.status === "retired" ? setRetiring(input) : save.mutate(input),
        )}
        className="space-y-6 border-b bg-muted/10 p-5"
      >
        <fieldset disabled={busy} className="space-y-6">
          <div className="grid gap-5 sm:grid-cols-3">
            {text("code", "Code", !!record)}
            {text("name", "Name")}
            <Field id="plan-kind" label="Kind" error={errors.kind?.message}>
              <SelectControl
                id="plan-kind"
                value={value.kind}
                onValueChange={(next) => set("kind", next as BenefitPlanForm["kind"])}
                options={kindOptions}
              />
            </Field>
            {date("effective_from", "Effective from")}
            {date("effective_until", "Effective until")}
            {record && (
              <Field id="plan-status" label="Status" error={errors.status?.message}>
                <SelectControl
                  id="plan-status"
                  value={value.status}
                  onValueChange={(next) => set("status", next as BenefitPlanForm["status"])}
                  options={statusOptions}
                />
              </Field>
            )}
          </div>
          <Field id="plan-description" label="Description" error={errors.description?.message}>
            <textarea
              id="plan-description"
              rows={2}
              className={inputClass}
              aria-invalid={!!errors.description}
              {...form.register("description")}
            />
          </Field>
          <div className="space-y-3">
            <Check
              label="Covers dependents"
              checked={value.covers_dependents}
              onChange={(on) => {
                set("covers_dependents", on);
                if (!on) set("covered_relationships", []);
              }}
            />
            {value.covers_dependents && (
              <fieldset
                aria-describedby={
                  errors.covered_relationships ? "plan-relationships-error" : undefined
                }
                className="rounded-xl border p-4"
              >
                <legend className="px-1 text-sm font-medium">Covered relationships</legend>
                {choices.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    {relationships.isPending
                      ? "Loading relationships…"
                      : "Add an active relationship in HR setup first."}
                  </p>
                ) : (
                  <div className="grid gap-2 sm:grid-cols-3">
                    {choices.map((r) => (
                      <Check
                        key={r.id}
                        label={r.name}
                        checked={value.covered_relationships.includes(r.id)}
                        onChange={(on) =>
                          set(
                            "covered_relationships",
                            on
                              ? [...value.covered_relationships, r.id]
                              : value.covered_relationships.filter((x) => x !== r.id),
                          )
                        }
                      />
                    ))}
                  </div>
                )}
                {relationships.hasMore && (
                  <Button
                    type="button"
                    variant="outline"
                    className="mt-3"
                    disabled={relationships.isFetchingMore}
                    onClick={relationships.loadMore}
                  >
                    Load more relationships
                  </Button>
                )}
                {errors.covered_relationships && (
                  <p
                    id="plan-relationships-error"
                    role="alert"
                    className="mt-2 text-sm text-destructive"
                  >
                    {errors.covered_relationships.message}
                  </p>
                )}
              </fieldset>
            )}
          </div>
        </fieldset>
        {failure && <SaveError message={saveFailure(failure, "plan")} />}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={busy}>
            {save.isPending ? "Saving…" : "Save plan"}
          </Button>
        </div>
      </form>
      {record && (
        <ConfirmDialog
          open={retiring !== null}
          onOpenChange={(open) => !open && setRetiring(null)}
          title={`Retire ${record.name}?`}
          description="A retired plan takes no new coverage and can never change again. Coverage that is open-ended or runs past the end date must end first."
          confirmLabel="Retire plan"
          busy={retire.isPending}
          onConfirm={() => {
            if (retiring) retire.mutate(retiring);
            setRetiring(null);
          }}
        />
      )}
    </>
  );
}
