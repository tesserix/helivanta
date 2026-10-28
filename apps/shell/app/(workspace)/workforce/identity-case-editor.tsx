"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, FormSelectField, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  IdentityCaseDraftInputContract,
  identityCasesKey,
  newIdentityCaseId,
  saveIdentityCaseDraft,
  useApiMutation,
  type IdentityCase,
  type IdentityCaseDraftInput,
  type IdentityCaseScope,
  type IdentitySource,
} from "@helivanta/api";

const draftForm = IdentityCaseDraftInputContract.pick({
  kind: true,
  policy_reference: true,
  check_reference: true,
  observed_on: true,
})
  .extend({
    valid_until: z
      .string()
      .min(1, "Choose a validity date and time.")
      .refine(
        (value) => Number.isFinite(new Date(value).getTime()),
        "Choose a valid date and time.",
      ),
  })
  .superRefine((value, ctx) => {
    if (Date.parse(value.valid_until) <= Date.parse(`${value.observed_on}T00:00:00Z`)) {
      ctx.addIssue({
        code: "custom",
        path: ["valid_until"],
        message: "Choose validity after the observation date.",
      });
    }
    if (value.observed_on > new Date().toISOString().slice(0, 10)) {
      ctx.addIssue({
        code: "custom",
        path: ["observed_on"],
        message: "Choose an observation date no later than today in UTC.",
      });
    }
  });

function localTime(date: Date) {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

function failure(error: Error) {
  if (error instanceof ApiError && error.status === 409) {
    return "The staff details, draft or workspace changed. Close this form and refresh before continuing.";
  }
  if (error instanceof ApiError && [400, 401, 403, 404].includes(error.status))
    return error.message;
  return "The request may already have been recorded. Retry the same draft or check history before starting another request.";
}

export function IdentityCaseEditor({
  scope,
  source,
  item,
  onClose,
  onSaved,
}: {
  scope: IdentityCaseScope;
  source: IdentitySource;
  item?: IdentityCase;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [id] = useState(() => item?.id ?? newIdentityCaseId());
  const [basis] = useState(() => ({
    identity_hash: source.identity_hash,
    employment_epoch: source.employment_epoch,
  }));
  const [submitted, setSubmitted] = useState<IdentityCaseDraftInput | null>(null);
  const form = useZodForm(draftForm, {
    kind: item?.kind ?? "identity",
    policy_reference: item?.policy_reference ?? "",
    check_reference: item?.check_reference ?? "",
    observed_on: item?.observed_on ?? new Date().toISOString().slice(0, 10),
    valid_until: item ? localTime(new Date(item.valid_until)) : "",
  });
  const save = useApiMutation(
    (command: IdentityCaseDraftInput) => saveIdentityCaseDraft(scope, id, command),
    {
      invalidate: [identityCasesKey(scope)],
      successToast: "Review draft saved",
      suppressErrorToast: true,
      onSuccess: onSaved,
    },
  );
  const changed =
    basis.identity_hash !== source.identity_hash ||
    basis.employment_epoch !== source.employment_epoch;
  const locked = save.isPending || submitted !== null;
  return (
    <form
      aria-label={item ? "Edit review draft" : "Prepare review draft"}
      noValidate
      className="space-y-4 border-t pt-4"
      onSubmit={form.handleSubmit((values) => {
        if (!submitted && changed) return;
        const command = submitted ?? {
          ...values,
          ...basis,
          command_id: newIdentityCaseId(),
          expected_version: item?.version ?? 0,
          valid_until: new Date(values.valid_until).toISOString(),
        };
        setSubmitted(command);
        save.mutate(command);
      })}
    >
      <h4 className="font-semibold">{item ? "Edit review draft" : "Prepare review draft"}</h4>
      <p className="text-sm text-muted-foreground">
        Record the hospital policy and check reference. A draft does not verify the staff member or
        issue an identity card.
      </p>
      <fieldset disabled={locked} className="grid gap-4 sm:grid-cols-2">
        <FormSelectField
          control={form.control}
          name="kind"
          id="identity-case-kind"
          label="Check type"
          disabled={Boolean(item)}
          options={[
            { value: "identity", label: "Identity" },
            { value: "background", label: "Background" },
            { value: "photograph", label: "Photograph" },
          ]}
        />
        <Field
          id="identity-case-policy"
          label="Policy reference"
          error={
            form.formState.errors.policy_reference?.type === "too_small"
              ? "Enter the hospital policy reference."
              : form.formState.errors.policy_reference?.message
          }
        >
          <Input id="identity-case-policy" maxLength={300} {...form.register("policy_reference")} />
        </Field>
        <Field
          id="identity-case-reference"
          label="Check reference"
          error={
            form.formState.errors.check_reference?.type === "too_small"
              ? "Enter a check reference."
              : form.formState.errors.check_reference?.message
          }
        >
          <Input
            id="identity-case-reference"
            maxLength={500}
            {...form.register("check_reference")}
          />
        </Field>
        <Field
          id="identity-case-observed"
          label="Observation date (UTC)"
          error={form.formState.errors.observed_on?.message}
        >
          <Input id="identity-case-observed" type="date" {...form.register("observed_on")} />
        </Field>
        <Field
          id="identity-case-validity"
          label="Valid until"
          error={form.formState.errors.valid_until?.message}
        >
          <Input
            id="identity-case-validity"
            type="datetime-local"
            {...form.register("valid_until")}
          />
        </Field>
      </fieldset>
      <p className="text-xs text-muted-foreground">
        Validity uses your device timezone. The draft is bound to the identity details shown above;
        changes create a retained revision.
      </p>
      {changed && !submitted && (
        <p role="alert">
          Identity or employment details changed. Close and reopen this draft before saving.
        </p>
      )}
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {failure(save.error)}
        </p>
      )}
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Close draft form
        </Button>
        <Button type="submit" disabled={save.isPending || (changed && !submitted)}>
          {save.isPending ? "Saving…" : submitted ? "Retry same draft" : "Save draft"}
        </Button>
      </div>
    </form>
  );
}
