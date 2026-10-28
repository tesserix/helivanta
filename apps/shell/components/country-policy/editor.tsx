"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { Plus, Trash2 } from "lucide-react";
import {
  countryConfiguration,
  countryPolicyEditorSchema,
  countryPolicySubmissionFromEditor,
  countryProfile,
  newCountryPolicyId,
  RequirementContract,
  submitCountryPolicy,
  type CountryPolicyIdentity,
  type CountryPolicyReceipt,
  type CountryPolicyScope,
  type CountryPolicySubmission,
  type CountryPolicyEditorValues,
  type CountryPolicyVersion,
} from "@helivanta/api";
import { Field, FormSelectField, Panel, useFieldArray, useZodForm } from "@helivanta/ui";
import { useCountryPolicyCommand } from "./command";

export const policyLabel = (value: string) =>
  value === "otc"
    ? "OTC"
    : value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
type PolicyForm = ReturnType<typeof useZodForm<typeof countryPolicyEditorSchema>>;
const textareaClass =
  "min-h-24 w-full rounded-md border bg-background px-3 py-2 text-sm font-normal";
const policyFields = countryPolicyEditorSchema.shape;

function initialPolicy(
  scope?: CountryPolicyScope,
  version?: CountryPolicyVersion,
): CountryPolicyEditorValues {
  return {
    id: newCountryPolicyId(),
    scope_id: scope?.id ?? newCountryPolicyId(),
    expected_revision: scope?.revision ?? 0,
    country: scope?.country ?? "IN",
    jurisdiction: scope?.jurisdiction ?? "",
    category: scope?.category ?? "otc",
    operation: scope?.operation ?? "order",
    effective_from: version ? new Date(version.effective_from).toISOString().slice(0, -1) : "",
    effective_until: version ? new Date(version.effective_until).toISOString().slice(0, -1) : "",
    sources: version?.sources.map((source) => ({
      ...source,
      reviewed_at: new Date(source.reviewed_at).toISOString().slice(0, -1),
    })) ?? [{ key: `source-${newCountryPolicyId()}`, title: "", url: "", reviewed_at: "" }],
    requirements:
      version?.requirements ??
      RequirementContract.shape.domain.options.map((domain) => ({
        domain,
        applicability: "required",
        reason: "",
        source_keys: [],
      })),
  };
}

export function CountryPolicyEditor({
  identity,
  scope,
  version,
  onSaved,
  onCancel,
}: {
  identity: CountryPolicyIdentity;
  scope?: CountryPolicyScope;
  version?: CountryPolicyVersion;
  onSaved: (receipt: CountryPolicyReceipt) => void;
  onCancel: () => void;
}) {
  const [initial] = useState(() => initialPolicy(scope, version));
  const form = useZodForm(countryPolicyEditorSchema, initial);
  const sources = useFieldArray({ control: form.control, name: "sources" });
  const sourceValues = form.watch("sources");
  const profile = countryProfile(form.watch("country"));
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start" });
    heading.current?.focus({ preventScroll: true });
  }, []);
  const save = useCountryPolicyCommand(
    identity,
    (input: CountryPolicySubmission, signal) => submitCountryPolicy(identity, input, signal),
    "Policy submitted",
    onSaved,
  );
  const errors = form.formState.errors;
  return (
    <Panel>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => {
          save.mutate(countryPolicySubmissionFromEditor(values));
        })}
        className="space-y-6"
      >
        <header>
          <h3 ref={heading} tabIndex={-1} className="scroll-mt-24 text-xl font-semibold">
            {scope ? "New policy version" : "New country policy"}
          </h3>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">
            Record the exact operating scope, reviewed citations and every requirement. Submission
            needs independent review before it can become active.
          </p>
        </header>
        <fieldset disabled={save.isPending} className="space-y-6">
          <div className="grid gap-5 sm:grid-cols-2 xl:grid-cols-4">
            <FormSelectField
              control={form.control}
              name="country"
              id="policy-country"
              label="Country"
              disabled={!!scope}
              options={countryConfiguration.countries.map((country) => ({
                value: country.code,
                label: country.name,
              }))}
            />
            <Field
              id="policy-jurisdiction"
              label="Jurisdiction"
              error={errors.jurisdiction?.message}
            >
              <Input
                id="policy-jurisdiction"
                readOnly={!!scope}
                {...form.register("jurisdiction")}
                placeholder="State, province or local jurisdiction"
              />
            </Field>
            <FormSelectField
              control={form.control}
              name="category"
              id="policy-category"
              label="Product category"
              disabled={!!scope}
              options={policyFields.category.options.map((value) => ({
                value,
                label: policyLabel(value),
              }))}
            />
            <FormSelectField
              control={form.control}
              name="operation"
              id="policy-operation"
              label="Trading operation"
              disabled={!!scope}
              options={policyFields.operation.options.map((value) => ({
                value,
                label: policyLabel(value),
              }))}
            />
          </div>
          <p className="text-sm text-muted-foreground">
            Country metadata: {profile?.currency} · {profile?.timezone}. This policy does not
            activate another country or jurisdiction.
          </p>
          <div className="grid gap-5 sm:grid-cols-2">
            <PolicyTimeField
              form={form}
              name="effective_from"
              id="policy-from"
              label="Effective from (UTC)"
            />
            <PolicyTimeField
              form={form}
              name="effective_until"
              id="policy-until"
              label="Effective until (UTC)"
            />
          </div>
          <section aria-label="Reviewed policy sources" className="space-y-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h4 className="font-semibold">Reviewed sources</h4>
              <Button
                type="button"
                variant="outline"
                disabled={sources.fields.length >= 30}
                onClick={() =>
                  sources.append({
                    key: `source-${newCountryPolicyId()}`,
                    title: "",
                    url: "",
                    reviewed_at: "",
                  })
                }
              >
                <Plus className="me-2 size-4" />
                Add source
              </Button>
            </div>
            {sources.fields.map((source, index) => (
              <Panel key={source.id}>
                <fieldset className="space-y-4">
                  <legend className="px-1 text-sm font-medium">Source {index + 1}</legend>
                  <Field
                    id={`source-${index}-title`}
                    label={`Source ${index + 1} title`}
                    error={errors.sources?.[index]?.title?.message}
                  >
                    <Input
                      id={`source-${index}-title`}
                      {...form.register(`sources.${index}.title`)}
                    />
                  </Field>
                  <Field
                    id={`source-${index}-url`}
                    label={`Source ${index + 1} HTTPS citation`}
                    error={errors.sources?.[index]?.url?.message}
                  >
                    <Input
                      id={`source-${index}-url`}
                      type="url"
                      {...form.register(`sources.${index}.url`)}
                      placeholder="https://…"
                    />
                  </Field>
                  <PolicyTimeField
                    form={form}
                    name={`sources.${index}.reviewed_at`}
                    id={`source-${index}-reviewed`}
                    label={`Source ${index + 1} reviewed at (UTC)`}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    disabled={sources.fields.length === 1}
                    onClick={() => {
                      const key = sourceValues[index].key;
                      sources.remove(index);
                      form.getValues("requirements").forEach((requirement, row) =>
                        form.setValue(
                          `requirements.${row}.source_keys`,
                          requirement.source_keys.filter((value) => value !== key),
                        ),
                      );
                    }}
                  >
                    <Trash2 className="me-2 size-4" />
                    Remove source {index + 1}
                  </Button>
                </fieldset>
              </Panel>
            ))}
            {errors.sources?.message && (
              <p role="alert" className="text-sm text-destructive">
                {errors.sources.message}
              </p>
            )}
          </section>
          <section className="space-y-5">
            <h4 className="font-semibold">Requirements and applicability</h4>
            <p className="text-sm leading-6 text-muted-foreground">
              For each domain, explain the requirements or why they do not apply, and select the
              reviewed citations that support the decision.
            </p>
            {form.getValues("requirements").map((requirement, index) => (
              <Panel key={requirement.domain}>
                <fieldset
                  aria-label={`${policyLabel(requirement.domain)} requirement`}
                  className="space-y-4"
                >
                  <legend className="px-1 text-sm font-semibold">
                    {policyLabel(requirement.domain)}
                  </legend>
                  <FormSelectField
                    control={form.control}
                    name={`requirements.${index}.applicability`}
                    id={`requirement-${index}-applies`}
                    label={`${policyLabel(requirement.domain)} applicability`}
                    options={[
                      { value: "required", label: "Required" },
                      {
                        value: "not_applicable",
                        label: "Not applicable — reviewed reason required",
                      },
                    ]}
                  />
                  <Field
                    id={`requirement-${index}-reason`}
                    label={`${policyLabel(requirement.domain)} rationale`}
                    error={errors.requirements?.[index]?.reason?.message}
                  >
                    <textarea
                      id={`requirement-${index}-reason`}
                      className={textareaClass}
                      {...form.register(`requirements.${index}.reason`)}
                    />
                  </Field>
                  <div className="space-y-2">
                    <p className="text-sm font-medium">Supporting citations</p>
                    {sourceValues.map((source, sourceIndex) => (
                      <label
                        key={source.key}
                        className="flex items-start gap-2 text-sm font-normal"
                      >
                        <input
                          type="checkbox"
                          value={source.key}
                          {...form.register(`requirements.${index}.source_keys`)}
                          className="mt-1 accent-(--hms-accent)"
                        />
                        <span>{source.title || `Source ${sourceIndex + 1}`}</span>
                      </label>
                    ))}
                  </div>
                  {errors.requirements?.[index]?.source_keys?.message && (
                    <p role="alert" className="text-sm text-destructive">
                      {errors.requirements[index]?.source_keys?.message}
                    </p>
                  )}
                </fieldset>
              </Panel>
            ))}
            {errors.requirements?.message && (
              <p role="alert" className="text-sm text-destructive">
                {errors.requirements.message}
              </p>
            )}
          </section>
          {save.error && (
            <p role="alert" className="rounded-md bg-destructive/5 p-4 text-sm text-destructive">
              {save.error.message}
            </p>
          )}
          <div className="flex flex-wrap gap-3">
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? "Submitting…" : "Submit policy for review"}
            </Button>
            <Button type="button" variant="outline" onClick={onCancel}>
              Cancel
            </Button>
          </div>
        </fieldset>
      </form>
    </Panel>
  );
}

function PolicyTimeField({
  form,
  name,
  id,
  label,
}: {
  form: PolicyForm;
  name: "effective_from" | "effective_until" | `sources.${number}.reviewed_at`;
  id: string;
  label: string;
}) {
  return (
    <Field id={id} label={label} error={form.getFieldState(name, form.formState).error?.message}>
      <Input id={id} type="datetime-local" step="0.001" {...form.register(name)} />
    </Field>
  );
}
